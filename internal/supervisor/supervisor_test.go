package supervisor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"wow-hub/internal/i18n"
	"wow-hub/internal/notify"
	"wow-hub/internal/osops"
	"wow-hub/internal/repack"
)

func TestMain(m *testing.M) {
	// The assertions pin exact Russian wording; the process-wide language
	// defaults to English now, so the tests pin theirs explicitly.
	i18n.Set(i18n.Ru)
	os.Exit(m.Run())
}

// fakeConfirmer answers the force-kill dialog from a script.
type fakeConfirmer struct {
	answers []bool
	asked   []string
	calls   int
}

func (c *fakeConfirmer) AskConfirm(service string, pid int) <-chan bool {
	c.asked = append(c.asked, service)
	ch := make(chan bool, 1)
	i := c.calls
	c.calls++
	if i < len(c.answers) {
		ch <- c.answers[i]
	} else {
		// No scripted answer: never reply, which is how the dialog timeout
		// is exercised.
	}
	return ch
}

// harness bundles a supervisor with the fakes behind it.
type harness struct {
	sup   *Supervisor
	ops   *osops.FakeOps
	clk   *osops.FakeClock
	sink  *notify.Recorder
	procs map[ServiceID]*osops.FakeProc
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	ops := osops.NewFake()
	clk := osops.NewFakeClock()
	sink := notify.NewRecorder()
	lay := repack.Layout{Root: `C:\repack`}

	sup := New(ops, clk, lay, sink, nil, &fakeConfirmer{answers: []bool{true}}, Conf{})

	return &harness{sup: sup, ops: ops, clk: clk, sink: sink, procs: map[ServiceID]*osops.FakeProc{}}
}

// testLayout is a layout with every path filled, so code that guards on an
// empty path - stopMySQL refusing to run without mysqladmin.exe - is
// exercised rather than short-circuited.
func testLayout() repack.Layout {
	const root = `C:\repack`
	return repack.Layout{
		Root:           root,
		WorldserverExe: filepath.Join(root, "worldserver.exe"),
		AuthserverExe:  filepath.Join(root, "authserver.exe"),
		MySQLdExe:      filepath.Join(root, "mysql", "bin", "mysqld.exe"),
		MySQLadminExe:  filepath.Join(root, "mysql", "bin", "mysqladmin.exe"),
		WorldConf:      filepath.Join(root, "configs", "worldserver.conf"),
		AuthConf:       filepath.Join(root, "configs", "authserver.conf"),
		MySQLPassConf:  filepath.Join(root, "temp", "mysql_ac.conf"),
		MySQLRootConf:  filepath.Join(root, "temp", "mysql_root.conf"),
		WorldConfRel:   "configs/worldserver.conf",
		AuthConfRel:    "configs/authserver.conf",
		DataDbcDir:     filepath.Join(root, "data", "dbc"),
		LogsDir:        filepath.Join(root, "logs"),
	}
}

func find(snap Snapshot, id ServiceID) ServiceStatus {
	for _, sv := range snap.Services {
		if sv.ID == id {
			return sv
		}
	}
	return ServiceStatus{}
}

func TestStatusAllStoppedWhenNothingRuns(t *testing.T) {
	h := newHarness(t)

	snap := h.sup.Status()
	if len(snap.Services) != 3 {
		t.Fatalf("snapshot has %d services, want 3", len(snap.Services))
	}
	for _, sv := range snap.Services {
		if sv.State != StateStopped {
			t.Errorf("%s state = %q, want %q", sv.ID, sv.State, StateStopped)
		}
		if sv.PID != 0 {
			t.Errorf("%s PID = %d, want 0", sv.ID, sv.PID)
		}
	}
}

func TestStatusOrderingIsFixed(t *testing.T) {
	// The UI renders rows in this order regardless of map iteration, and a
	// snapshot whose services jump around between refreshes would make the
	// table flicker.
	h := newHarness(t)

	want := []ServiceID{SvcMySQL, SvcAuth, SvcWorld}
	for i := 0; i < 5; i++ {
		snap := h.sup.Status()
		if len(snap.Services) != len(want) {
			t.Fatalf("snapshot has %d services", len(snap.Services))
		}
		for j, id := range want {
			if snap.Services[j].ID != id {
				t.Fatalf("position %d = %q, want %q", j, snap.Services[j].ID, id)
			}
		}
	}
}

func TestRefreshAdoptsSingleExternalProcess(t *testing.T) {
	h := newHarness(t)
	h.ops.Procs["worldserver.exe"] = []int{4242}

	h.sup.Refresh()

	got := find(h.sup.Status(), SvcWorld)
	if got.State != StateExternal {
		t.Errorf("state = %q, want %q", got.State, StateExternal)
	}
	if got.PID != 4242 {
		t.Errorf("PID = %d, want 4242", got.PID)
	}
	if got.Owned {
		t.Error("Owned = true for a process the app did not start")
	}
}

func TestRefreshPrefersTheProcessOwningThePort(t *testing.T) {
	// MySQL 8.4.7 on Windows runs as a parent/child pair of mysqld.exe and
	// only the child owns 3306. Taking the lower PID adopts the parent,
	// which listens on nothing - and the Server Control table would show a
	// PID that leads nowhere.
	h := newHarness(t)
	h.ops.Procs["mysqld.exe"] = []int{12448, 74248}
	h.ops.PortPIDs[3306] = 74248

	h.sup.Refresh()

	got := find(h.sup.Status(), SvcMySQL)
	if got.PID != 74248 {
		t.Fatalf("PID = %d, want the port owner 74248, not the lower 12448", got.PID)
	}
	if !strings.Contains(got.Detail, "12448") {
		t.Errorf("Detail = %q, want it to mention the process that was not chosen", got.Detail)
	}
}

func TestRefreshFallsBackToLowestPIDWhenNoPortOwner(t *testing.T) {
	// Between mysqld's parent starting and its child binding 3306 there is a
	// window with two processes and no port owner. Adoption still has to
	// pick something.
	h := newHarness(t)
	h.ops.Procs["mysqld.exe"] = []int{12448, 74248}

	h.sup.Refresh()

	got := find(h.sup.Status(), SvcMySQL)
	if got.PID != 12448 {
		t.Fatalf("PID = %d, want the lowest 12448 when nothing owns 3306", got.PID)
	}
}

func TestStatusPicksLowestWhenSeveralWorlds(t *testing.T) {
	h := newHarness(t)
	h.ops.Procs["worldserver.exe"] = []int{5151, 4242}

	h.sup.Refresh()

	got := find(h.sup.Status(), SvcWorld)
	if got.PID != 4242 {
		t.Errorf("PID = %d, want the lowest 4242", got.PID)
	}
	if !strings.Contains(got.Detail, "5151") || !strings.Contains(got.Detail, "4242") {
		t.Errorf("Detail = %q, want it to name both PIDs", got.Detail)
	}
}

func TestRefreshEmitsOnlyWhenSomethingChanges(t *testing.T) {
	h := newHarness(t)
	h.ops.Procs["worldserver.exe"] = []int{4242}

	h.sup.Refresh() // first observation: something to report
	h.sup.Refresh() // identical
	h.sup.Refresh() // identical

	if got := h.sink.CountOf(notify.EventStatus); got != 1 {
		t.Fatalf("emitted %d status events for one real change, want 1", got)
	}

	h.sink.Reset()
	h.ops.Procs["worldserver.exe"] = []int{4242, 5151}
	h.sup.Refresh()
	h.sup.Refresh()

	if got := h.sink.CountOf(notify.EventStatus); got != 1 {
		t.Fatalf("emitted %d status events after a PID set change, want 1", got)
	}
}

func TestRefreshEmitsOnlyWhenMemoryLeavesItsBucket(t *testing.T) {
	// This is the test that keeps the UI from being redrawn every second.
	// Memory changes constantly; emitting on every change would turn the
	// delta stream into a flood and repaint the whole table forever.
	//
	// It is not excluded outright, though. A number that is recomputed every
	// tick and never reaches the screen is worse than no number at all, so a
	// change counts once it crosses a bucket boundary (memoryBucketMB).
	h := newHarness(t)
	h.ops.Procs["worldserver.exe"] = []int{4242}
	h.ops.Memory[4242] = 1024

	h.sup.Refresh()
	h.sink.Reset()

	// A few megabytes of ordinary allocator noise: same bucket, no repaint.
	h.ops.Memory[4242] = 1024 + memoryBucketMB - 1
	h.sup.Refresh()
	if got := h.sink.CountOf(notify.EventStatus); got != 0 {
		t.Fatalf("emitted %d status events for a change inside one bucket, want 0", got)
	}

	// Crossing a bucket is a change the figure on screen should reflect.
	h.ops.Memory[4242] = 1024 + memoryBucketMB
	h.sup.Refresh()
	if got := h.sink.CountOf(notify.EventStatus); got != 1 {
		t.Fatalf("emitted %d status events after crossing a bucket, want 1", got)
	}
}

func TestRefreshDoesNotOverwriteAStartThatLandedWhileObserving(t *testing.T) {
	// Refresh reads which processes it tracks, then spends hundreds of
	// milliseconds gathering observations - tasklist and netstat per
	// untracked service - and only then applies them. A start can land in
	// that window, and the observation being held was built from the state
	// BEFORE it. Applying it anyway rewrites the fresh StateStarting as
	// StateExternal, and every later tick preserves that.
	//
	// That is the bug the user reported: a worldserver this app had just
	// started sat on "запущен извне" for the whole two and a half minutes of
	// its startup, and only flipped to "работает" when the readiness check
	// happened to set StateRunning.
	h := newHarness(t)

	// The world appears, and is adopted as ours, while Refresh is inside
	// its observation phase.
	proc := osops.NewFakeProc(4242)
	h.ops.OnFindByName = func(name string) ([]int, error) {
		if name == "worldserver.exe" {
			h.sup.track(SvcWorld, proc)
			return []int{4242}, nil
		}
		return nil, nil
	}

	h.sup.Refresh()

	if got := find(h.sup.Status(), SvcWorld).State; got != StateStarting {
		t.Fatalf("state = %q, want %q: a stale observation overwrote a start that had already happened",
			got, StateStarting)
	}
}

func TestStatusStillReportsChangedMemory(t *testing.T) {
	// Not emitting is not the same as not knowing. The value must be current
	// in the snapshot even when it does not count as a change.
	h := newHarness(t)
	h.ops.Procs["worldserver.exe"] = []int{4242}
	h.ops.Memory[4242] = 1024

	h.sup.Refresh()
	h.ops.Memory[4242] = 2048
	h.sup.Refresh()

	if got := find(h.sup.Status(), SvcWorld).MemoryMB; got != 2048 {
		t.Fatalf("MemoryMB = %d, want the current 2048", got)
	}
}

func TestRefreshSumsTheProcessTreeOfATrackedService(t *testing.T) {
	// MySQL 8.4.7 on Windows is a mysqld.exe parent/child pair, and this app
	// starts the parent - a process holding about 30 MB while the 512 MB
	// buffer pool lives in its child. Reading the tracked PID alone is what
	// made the table claim 28 MB for a database Task Manager showed at 460.
	h := newHarness(t)
	h.sup.track(SvcMySQL, osops.NewFakeProc(79700))
	h.ops.Memory[79700] = 29
	h.ops.Memory[48476] = 460
	h.ops.Children[79700] = []int{48476}

	h.sup.Refresh()

	if got := find(h.sup.Status(), SvcMySQL).MemoryMB; got != 489 {
		t.Fatalf("MemoryMB = %d, want 489: the parent and the child that holds the buffer pool", got)
	}
}

func TestRefreshSumsEveryMatchedProcessOfAnAdoptedService(t *testing.T) {
	// An adopted MySQL is the same pair, found by image name instead of
	// started by us. The figure has to match the one a start of ours
	// produces, or the table would tell a different story about the same
	// database depending on who launched it.
	h := newHarness(t)
	h.ops.Procs["mysqld.exe"] = []int{79700, 48476}
	h.ops.PortPIDs[portMySQL] = 48476
	h.ops.Memory[79700] = 29
	h.ops.Memory[48476] = 460

	h.sup.Refresh()

	got := find(h.sup.Status(), SvcMySQL)
	if got.MemoryMB != 489 {
		t.Fatalf("MemoryMB = %d, want 489", got.MemoryMB)
	}
	if got.PID != 48476 {
		t.Fatalf("PID = %d, want the port owner 48476", got.PID)
	}
}

func TestRefreshAdoptsWhenSecondProcessAppears(t *testing.T) {
	h := newHarness(t)
	h.ops.Procs["worldserver.exe"] = []int{4242}
	h.sup.Refresh()
	first := find(h.sup.Status(), SvcWorld)
	h.sink.Reset()

	h.ops.Procs["worldserver.exe"] = []int{4242, 5151}
	h.sup.Refresh()

	got := find(h.sup.Status(), SvcWorld)
	if got.Detail == first.Detail {
		t.Fatalf("Detail = %q, unchanged after a second process appeared", got.Detail)
	}
	if got.Count != 2 {
		t.Errorf("process count = %d, want 2", got.Count)
	}
	if h.sink.CountOf(notify.EventStatus) != 1 {
		t.Errorf("emitted %d events, want 1 - an ambiguous match must be visible", h.sink.CountOf(notify.EventStatus))
	}
}

func TestRefreshForgetsDeadExternalProcess(t *testing.T) {
	// A process the app adopted can die on its own. Staying stuck on
	// "external" would make Stop act on a PID that no longer exists.
	h := newHarness(t)
	h.ops.Procs["worldserver.exe"] = []int{4242}
	h.sup.Refresh()

	h.ops.Procs["worldserver.exe"] = nil
	h.ops.Dead[4242] = true
	h.sup.Refresh()

	if got := find(h.sup.Status(), SvcWorld).State; got != StateStopped {
		t.Fatalf("state = %q, want %q after the adopted process died", got, StateStopped)
	}
}

func TestUnexpectedExitFlipsToFailed(t *testing.T) {
	// A worldserver that dies on its own is not a normal stop. Reporting
	// "stopped" sends the user looking for a shutdown they never asked for
	// instead of at Crashes\.
	h := newHarness(t)
	p := osops.NewFakeProc(4242)
	h.procs[SvcWorld] = p
	h.sup.track(SvcWorld, p)

	h.sink.Reset()
	p.WaitCh <- nil

	waitForState(t, h.sup, SvcWorld, StateFailed)
	if h.sink.CountOf(notify.EventStatus) == 0 {
		t.Error("no status event after an unexpected exit")
	}
}

func TestUnexpectedExitCarriesWaitError(t *testing.T) {
	h := newHarness(t)
	p := osops.NewFakeProc(4242)
	h.sup.track(SvcWorld, p)

	h.sink.Reset()
	p.WaitCh <- errForTest("access violation at 0x7ff72464fa38")

	waitForState(t, h.sup, SvcWorld, StateFailed)
	got := find(h.sup.Status(), SvcWorld)
	if !strings.Contains(got.Detail, "access violation") {
		t.Fatalf("Detail = %q, want the exit reason", got.Detail)
	}
}

func TestRequestedStopIsNotReportedAsACrash(t *testing.T) {
	// A stop the app asked for must land as "stopped", never as "failed".
	// Otherwise every ordinary shutdown looks like a crash.
	h := newHarness(t)
	p := osops.NewFakeProc(4242)
	h.sup.track(SvcWorld, p)
	h.sup.markStopping(SvcWorld)

	p.WaitCh <- nil

	waitForState(t, h.sup, SvcWorld, StateStopped)
	if got := find(h.sup.Status(), SvcWorld); strings.Contains(got.Detail, "exit") {
		t.Errorf("Detail = %q, want no crash text for a requested stop", got.Detail)
	}
}

func TestStatusDoesNotAdoptWhileOwned(t *testing.T) {
	// Once the app has started a process it must keep tracking that one. A
	// refresh that re-adopted from FindByName could latch onto a leftover
	// process from a previous run.
	h := newHarness(t)
	p := osops.NewFakeProc(7000)
	h.sup.track(SvcWorld, p)
	h.ops.Procs["worldserver.exe"] = []int{9999}

	h.sup.Refresh()

	got := find(h.sup.Status(), SvcWorld)
	if got.PID != 7000 {
		t.Fatalf("PID = %d, want the tracked 7000, not the adopted 9999", got.PID)
	}
	if !got.Owned {
		t.Error("Owned = false for a process the app started")
	}
}

func TestNewDefaultsZeroTimeouts(t *testing.T) {
	// A zero timeout would make every wait succeed instantly, so the start
	// sequence would declare MySQL up before it had even started.
	ops := osops.NewFake()
	sup := New(ops, osops.NewFakeClock(), repack.Layout{}, notify.NewRecorder(), nil, &fakeConfirmer{}, Conf{})

	conf := sup.conf
	if conf.StopTimeout <= 0 || conf.ConfirmTimeout <= 0 {
		t.Fatalf("StopTimeout = %v, ConfirmTimeout = %v; want both defaulted above zero", conf.StopTimeout, conf.ConfirmTimeout)
	}
	if conf.Start.MySQLPort <= 0 || conf.Start.AuthPort <= 0 || conf.Start.WorldMarker <= 0 || conf.Start.DBPing <= 0 {
		t.Fatalf("start timeouts not defaulted: %+v", conf.Start)
	}
}

func TestNewPreservesExplicitTimeouts(t *testing.T) {
	want := Conf{
		StopTimeout:    90 * time.Second,
		ConfirmTimeout: 15 * time.Second,
		Start: StartTimeouts{
			MySQLPort:   5 * time.Second,
			DBPing:      6 * time.Second,
			AuthPort:    7 * time.Second,
			WorldMarker: 8 * time.Second,
		},
	}
	sup := New(osops.NewFake(), osops.NewFakeClock(), repack.Layout{}, notify.NewRecorder(), nil, &fakeConfirmer{}, want)

	if sup.conf != want {
		t.Fatalf("conf = %+v, want the caller's %+v", sup.conf, want)
	}
}

// waitForState polls until the service reaches want, or fails the test. The
// exit watcher runs on its own goroutine, so a test cannot simply read the
// result once.
func waitForState(t *testing.T, sup *Supervisor, id ServiceID, want State) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if find(sup.Status(), id).State == want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	got := find(sup.Status(), id)
	t.Fatalf("%s state = %q, want %q", id, got.State, want)
}

type errForTest string

func (e errForTest) Error() string { return string(e) }
