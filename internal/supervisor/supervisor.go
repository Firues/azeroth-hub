// Package supervisor owns the state of the three server processes and the
// order in which they are started and stopped.
//
// The order is not a preference. worldserver writes to the database as it
// shuts down, so if MySQL goes first the server meets a dead connection and
// may hang or lose an unsaved character. Start is MySQL, then a real
// database ping, then auth, then world; stop is the exact reverse.
//
// Everything here is tested against osops.FakeOps and osops.FakeClock. The
// stop ladder waits 30 seconds and the confirmation dialog 60 more; against
// the real clock that is a two-minute test suite nobody runs.
package supervisor

import (
	"strconv"
	"strings"
	"sync"
	"time"

	"wow-hub/internal/dbping"
	"wow-hub/internal/i18n"
	"wow-hub/internal/logx"
	"wow-hub/internal/notify"
	"wow-hub/internal/osops"
	"wow-hub/internal/repack"
)

// service is the mutable per-process record behind a ServiceStatus.
type service struct {
	id     ServiceID
	status ServiceStatus

	// proc is set only for a process this app started. Adopted processes
	// have none, so their exit cannot be waited on.
	proc osops.Proc
	// stopping is true between "the app asked it to stop" and its exit. A
	// process that exits during that window was stopped on purpose, and
	// reporting it as failed would make every ordinary shutdown look like a
	// crash.
	stopping bool

	// rev counts changes to status. Refresh reads it before it gathers its
	// observations and refuses to apply one that a later change has
	// outrun; see the comment in Refresh. Every helper that changes status
	// must bump it.
	rev int
}

// Supervisor is the single source of truth for what is running.
type Supervisor struct {
	ops       osops.Ops
	clk       osops.Clock
	lay       repack.Layout
	sink      notify.Sink
	pinger    dbping.Pinger
	confirmer Confirmer
	conf      Conf

	mu       sync.Mutex
	services map[ServiceID]*service
	order    []ServiceID

	// logMu guards the logger pointer only. It is deliberately not s.mu:
	// markStopFailed holds s.mu while logging, and a plain sync.Mutex is not
	// reentrant, so one log line from a locked section would deadlock the
	// whole app. The logger is read far more often than it is replaced, so
	// it gets its own uncontended lock.
	logMu sync.Mutex
	log   *logx.Logger

	// watchMu guards watchers, keyed by service. Separate from s.mu because
	// the start path writes one while holding no other lock and the readiness
	// path reads it from the same flow.
	watchMu  sync.Mutex
	watchers map[ServiceID]*markerWatcher

	// lastKey is the previous snapshot's change-detection key, so Refresh
	// can stay silent when nothing moved.
	lastKey string

	// opMu admits one operation at a time and is taken with TryLock, so a
	// second caller is told rather than queued.
	//
	// A queued start would be worse than a rejected one: the user clicks
	// twice, the second run waits out the first, and then starts a second
	// worldserver against the same characters. Rejecting costs one sentence
	// and cannot pile up.
	//
	// Held across StartAll, StartService, StopAll and StopService, and by
	// nothing else. The stop helpers it calls (stopOne, stopMySQL) must
	// never take it, or rollback inside StartAll would deadlock against the
	// start that triggered it. Refresh deliberately does not take it
	// either: it runs every second and would stall behind a three-minute
	// start, which is exactly when the table most needs to stay live.
	opMu sync.Mutex
}

// ErrBusy is returned when an operation is already under way.
//
// The message is read at use time, not here, so a language switch between
// construction and the click shows up in the sentence.
var ErrBusy = busyError{}

type busyError struct{}

func (busyError) Error() string { return i18n.T("err.busy") }

// begin admits an operation, or reports that one is already running.
func (s *Supervisor) begin() error {
	if !s.opMu.TryLock() {
		return ErrBusy
	}
	return nil
}

func (s *Supervisor) end() { s.opMu.Unlock() }

// New builds a Supervisor. Zero timeouts in conf are filled with defaults.
func New(ops osops.Ops, clk osops.Clock, lay repack.Layout, sink notify.Sink,
	pinger dbping.Pinger, confirmer Confirmer, conf Conf) *Supervisor {

	s := &Supervisor{
		ops:       ops,
		clk:       clk,
		lay:       lay,
		sink:      sink,
		pinger:    pinger,
		confirmer: confirmer,
		conf:      conf.withDefaults(),
		services:  map[ServiceID]*service{},
		watchers:  map[ServiceID]*markerWatcher{},
		order:     serviceOrder,
	}
	for _, id := range s.order {
		s.services[id] = &service{
			id:     id,
			status: ServiceStatus{ID: id, State: StateStopped},
		}
	}
	return s
}

// SetLogger attaches a logger. Optional: without one the supervisor stays
// silent rather than failing.
func (s *Supervisor) SetLogger(l *logx.Logger) {
	s.logMu.Lock()
	defer s.logMu.Unlock()
	s.log = l
}

// Status returns the current table.
func (s *Supervisor) Status() Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.snapshotLocked()
}

func (s *Supervisor) snapshotLocked() Snapshot {
	snap := Snapshot{Services: make([]ServiceStatus, 0, len(s.order))}
	for _, id := range s.order {
		snap.Services = append(snap.Services, s.services[id].status)
	}
	return snap
}

// observation is one service's freshly gathered facts, taken without any
// lock held.
type observation struct {
	// tracked is the PID of a process we started ourselves, or 0.
	tracked int

	alive bool
	memMB int

	// For a process we do not track: what the OS reports about its image.
	pids    []int
	findErr error
	pid     int
	detail  string
}

// Refresh re-reads the world and emits a status event only if something
// actually changed.
//
// It is called once a second, and every operating-system call it makes
// happens OUTSIDE the state lock. That matters: adoption shells out to
// tasklist, and holding s.mu across a subprocess would block Status, the
// stop ladder and the exit watcher for the whole of it. The lock is taken
// twice - once to learn which processes we track, once to apply the results.
//
// The price of taking the lock twice is a window between the two, and it is
// wide: the observation phase spawns tasklist and netstat, which costs
// hundreds of milliseconds on this machine. A start or a stop can land in
// it, and the observation was gathered from the state BEFORE that. Applying
// it anyway would overwrite the newer state with older facts.
//
// The case that made this a bug report: the user starts the world, and the
// tick that happened to be in flight when it launched carries "nothing is
// tracked for world" from before the launch. Applying that rewrote the fresh
// StateStarting as StateExternal, and every later tick then preserved it -
// so a worldserver this app had just started sat on "запущен извне" for the
// whole two and a half minutes of its startup, until the readiness check
// happened to overwrite the state with StateRunning. That is exactly what
// the user saw: "запущен извне" while starting, "работает" once up.
//
// The revision counters are what closes the window: a service whose rev
// moved is skipped, because its state is newer than the observation.
func (s *Supervisor) Refresh() {
	s.mu.Lock()
	tracked := make(map[ServiceID]int, len(s.order))
	revs := make(map[ServiceID]int, len(s.order))
	for _, id := range s.order {
		sv := s.services[id]
		if sv.proc != nil {
			tracked[id] = sv.proc.PID()
		}
		revs[id] = sv.rev
	}
	s.mu.Unlock()

	obs := make(map[ServiceID]observation, len(s.order))
	for _, id := range s.order {
		o := observation{tracked: tracked[id]}
		if o.tracked != 0 {
			// Ours: ask about it directly rather than searching for its
			// image, which could match a leftover from an earlier run.
			o.alive = s.ops.Alive(o.tracked)
			if o.alive {
				o.memMB = s.ops.TreeMemoryMB(o.tracked)
			}
		} else {
			o.pids, o.findErr = s.ops.FindByName(imageExe[id])
			if o.findErr == nil && len(o.pids) > 0 {
				o.pid, o.detail = s.pickLocked(id, o.pids)
				if o.pid != 0 {
					o.memMB = s.sumMemory(o.pids)
				}
			}
		}
		obs[id] = o
	}

	s.mu.Lock()
	for _, id := range s.order {
		sv := s.services[id]
		if sv.rev != revs[id] {
			// Something changed while the observation was being gathered.
			// That change is newer than these facts, so they are dropped
			// rather than applied over it; the next tick sees the new
			// state and observes it properly.
			continue
		}
		s.applyLocked(sv, obs[id])
	}
	snap := s.snapshotLocked()
	key := snap.key()
	changed := key != s.lastKey
	if changed {
		s.lastKey = key
	}
	s.mu.Unlock()

	if changed && s.sink != nil {
		s.sink.Emit(notify.Event{Type: notify.EventStatus, Data: snap})
	}
}

// applyLocked folds one observation into a service's state. It performs no
// I/O at all, which is what makes it safe to call with s.mu held.
func (s *Supervisor) applyLocked(sv *service, o observation) {
	if o.tracked != 0 {
		if !o.alive {
			s.clearLocked(sv)
			return
		}
		sv.status.MemoryMB = o.memMB
		return
	}

	// Not ours, or nothing tracked: the observation is what the OS said
	// about an already-running instance.
	if o.findErr != nil || len(o.pids) == 0 {
		if sv.status.State == StateExternal {
			// An adopted process died on its own. Staying on "external"
			// would make Stop act on a PID that no longer exists.
			s.clearLocked(sv)
		}
		return
	}
	if o.pid == 0 {
		return
	}

	// Since means "when this PID was first observed", so it survives a
	// refresh of the same process and restarts only when a different one
	// takes its place.
	since := sv.status.Since
	if sv.status.PID != o.pid || since.IsZero() {
		since = time.Now()
	}

	sv.status = ServiceStatus{
		ID:       sv.id,
		State:    StateExternal,
		PID:      o.pid,
		MemoryMB: o.memMB,
		Owned:    false,
		Detail:   o.detail,
		Count:    len(o.pids),
		Since:    since,
	}
}

// stillRunning answers whether a service is running right now, for the
// dependency guard in the stop path.
//
// It asks the operating system rather than trusting the cached table, and it
// deliberately treats "I could not tell" as "yes, still running". The two
// mistakes are not symmetrical: a false positive refuses a stop the user can
// retry a second later, while a false negative stops the database underneath
// a live worldserver. The cheap answer has to be the safe one.
func (s *Supervisor) stillRunning(id ServiceID) bool {
	if s.isRunning(id) {
		return true
	}
	pids, err := s.ops.FindByName(imageExe[id])
	if err != nil {
		return true
	}
	return len(pids) > 0
}

// sumMemory adds up the private working sets of the processes that make up a
// service the app did not start.
//
// Plain MemoryMB per PID, never TreeMemoryMB: the matched PIDs of a
// parent/child pair are both in the list already, so walking each one's tree
// as well would count the child twice. This is what makes an adopted MySQL
// report the same figure as one the app started itself - the pair either
// way, rather than the serving half of it.
//
// Each PID arrives already rounded to whole megabytes, so this can differ
// from TreeMemoryMB by a megabyte per process. At the sizes that matter -
// 443 + 18 for the MySQL pair - that is invisible, and the alternative,
// widening Ops with a byte-level sum just for this, buys nothing a person
// could see.
func (s *Supervisor) sumMemory(pids []int) int {
	total := 0
	for _, p := range pids {
		total += s.ops.MemoryMB(p)
	}
	return total
}

// pickLocked chooses which of several matching processes is the live one.
//
// The port decides, not the PID order. MySQL 8.4.7 on Windows runs as a
// parent/child pair of mysqld.exe and only the child owns 3306, so the PID
// that is not the port owner is the parent, which listens on nothing. Two
// worldserver.exe cannot both serve 8085 either.
//
// The PID order is not a shortcut for any of this. Measured on this repack
// the child had the *lower* PID of the pair, so "take the lowest" would have
// adopted the serving process by luck rather than by rule - and would pick
// the parent on the next machine. When nothing owns the port - a real window
// while mysqld's child is still binding - the lowest PID wins, because
// something has to be picked.
func (s *Supervisor) pickLocked(id ServiceID, pids []int) (int, string) {
	sorted := append([]int(nil), pids...)
	for i := 1; i < len(sorted); i++ {
		for j := i; j > 0 && sorted[j] < sorted[j-1]; j-- {
			sorted[j], sorted[j-1] = sorted[j-1], sorted[j]
		}
	}

	if owner, ok := s.ops.PortOwner(servicePort(id)); ok {
		for _, p := range sorted {
			if p == owner {
				if len(sorted) > 1 {
					return p, ambiguityDetail(sorted, owner)
				}
				return p, ""
			}
		}
	}

	if len(sorted) == 1 {
		return sorted[0], ""
	}
	return sorted[0], ambiguityDetail(sorted, sorted[0])
}

// ambiguityDetail explains that several processes matched and which one was
// taken. An ambiguous match must be visible, not silently resolved.
func ambiguityDetail(pids []int, chosen int) string {
	all := make([]string, len(pids))
	for i, p := range pids {
		all[i] = strconv.Itoa(p)
	}
	verb := i18n.T("process.ownerless")
	if chosen != pids[0] {
		verb = i18n.T("process.owner")
	}
	return i18n.Tf("process.pids", len(pids), strings.Join(all, ", "), verb, chosen)
}

func (s *Supervisor) clearLocked(sv *service) {
	sv.proc = nil
	sv.stopping = false
	sv.status = ServiceStatus{ID: sv.id, State: StateStopped}
	sv.rev++
}

// track records a process this app started and watches it for exit.
//
// It is called by the start sequence in Task 6. It exists here, in the task
// that owns process lifetime, so that no other package has to remember to
// watch.
func (s *Supervisor) track(id ServiceID, p osops.Proc) {
	s.mu.Lock()
	defer s.mu.Unlock()

	sv := s.services[id]
	sv.proc = p
	sv.stopping = false
	sv.status = ServiceStatus{
		ID:     id,
		State:  StateStarting,
		PID:    p.PID(),
		Owned:  true,
		Since:  time.Now(),
		Detail: "",
		Count:  1,
	}
	sv.rev++

	s.spawnExitWatcher(sv, p)
}

func (s *Supervisor) spawnExitWatcher(sv *service, p osops.Proc) {
	go func() {
		err := p.Wait()

		s.mu.Lock()

		// Only the process we are watching, and only while it is the one we
		// hold: a stale goroutine must not clobber a later start.
		if sv.proc != p {
			s.mu.Unlock()
			return
		}

		if sv.stopping {
			s.clearLocked(sv)
		} else {
			detail := i18n.T("process.exited")
			if err != nil {
				detail = i18n.Tf("process.exited.err", err)
			}
			sv.proc = nil
			sv.stopping = false
			sv.status = ServiceStatus{
				ID:     sv.id,
				State:  StateFailed,
				Owned:  false,
				Detail: detail,
				Count:  0,
				Since:  time.Now(),
			}
			sv.rev++
		}

		snap := s.snapshotLocked()
		key := snap.key()
		changed := key != s.lastKey
		if changed {
			s.lastKey = key
		}
		s.mu.Unlock()

		// Emit here rather than leaving it to the next tick. A process that
		// died on its own is the single moment the user most needs told; a
		// one-second delay is a second of looking at a table that claims
		// everything is fine.
		if changed && s.sink != nil {
			s.sink.Emit(notify.Event{Type: notify.EventStatus, Data: snap})
		}
	}()
}

// markStopping records that a stop was requested, so the exit watcher reads
// the exit as intended rather than as a crash.
func (s *Supervisor) markStopping(id ServiceID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sv := s.services[id]; sv != nil {
		sv.stopping = true
		sv.status.State = StateStopping
		sv.rev++
	}
}

// markRunning records that a process finished coming up and is serving.
// Task 6 calls it once each readiness check passes.
func (s *Supervisor) markRunning(id ServiceID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sv := s.services[id]
	if sv == nil || sv.proc == nil {
		return
	}
	sv.status.State = StateRunning
	sv.status.Detail = ""
	sv.rev++
}

// forget drops a tracked process without touching the OS, so adoption can
// run again from FindByName. Tests use it; nothing in the stop path does.
func (s *Supervisor) forget(id ServiceID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sv := s.services[id]; sv != nil {
		sv.proc = nil
		sv.stopping = false
		sv.status = ServiceStatus{ID: id, State: StateStopped}
		sv.rev++
	}
}
