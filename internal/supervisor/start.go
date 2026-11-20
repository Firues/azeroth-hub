package supervisor

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
	"wow-hub/internal/i18n"
	"wow-hub/internal/logx"
)

// StartAll brings the whole stack up in order, and rolls back what it
// started if any step fails.
//
// The order is not a preference. worldserver needs the database before it
// can do anything, and it needs authserver to be reachable before a player
// can log in. Half a stack is worse than none: MySQL running with nothing
// watching it is a process nobody knows about until the next boot complains.
func (s *Supervisor) StartAll(ctx context.Context) error {
	if err := s.begin(); err != nil {
		return err
	}
	defer s.end()

	started := make([]ServiceID, 0, 3)
	rollback := func(cause error) error {
		for i := len(started) - 1; i >= 0; i-- {
			// Only what this run started. Stopping something that was already
			// up before the button was pressed is a surprise nobody asked
			// for - and with adoption below, that is now a real case: MySQL
			// is commonly already running when this is pressed.
			//
			// Unchecked: this is cleanup of something this run just started,
			// not a deliberate stop of a dependency, so the guard must not
			// refuse it and leave a half-started process behind.
			if err := s.stopOneUnchecked(ctx, started[i]); err != nil {
				cause = fmt.Errorf("%w; %s: %w", cause, i18n.T("start.rollback.word"), err)
			}
		}
		return cause
	}

	// ensure brings up one service, or accepts the one already running.
	//
	// Adoption is the whole point of the second branch. A second mysqld
	// cannot bind 3306, dies immediately, and leaves every check below it
	// satisfied by the *first* MySQL - the readiness wait and the database
	// ping would both pass against a process this run did not start. Then a
	// later failure would roll back and shut down a database that was
	// already serving the user, which is the surprise the comment above
	// says must not happen.
	ensure := func(id ServiceID) error {
		up, pids, err := s.alreadyUp(id)
		if err != nil {
			// The process list is unreadable. Launching blind on a guess is
			// how a duplicate happens, so stop here and say so.
			return fmt.Errorf("%s: %w", i18n.Tf("start.probe.fail", id), err)
		}
		if up {
			s.logf("%s: уже запущен (PID %s) — второй не запускаем, ждём готовности", id, joinInts(pids))
		} else {
			if err := s.launch(ctx, id); err != nil {
				return err
			}
			// Registered only because this run launched it, so rollback
			// cannot touch anything it merely adopted.
			started = append(started, id)
		}
		// Wait either way. An adopted process may still be initialising -
		// the world takes minutes - and adopting it is not a reason to
		// declare it ready before it serves.
		return s.awaitReady(ctx, id)
	}

	// Step 1: MySQL.
	if err := ensure(SvcMySQL); err != nil {
		return rollback(err)
	}

	// Step 2: a real database ping. worldserver takes about a minute to fail
	// against a dead database and then dies in a screenful of errors; catching
	// it here turns that minute into one sentence.
	//
	// Runs even when MySQL was adopted, and it matters most exactly then: a
	// port that accepts connections while the database refuses them means
	// something else owns it.
	if err := s.pingDatabase(ctx); err != nil {
		return rollback(err)
	}

	// Step 3: authserver.
	if err := ensure(SvcAuth); err != nil {
		return rollback(err)
	}

	// Step 4: worldserver, ready when its socket accepts connections.
	//
	// Through rollback, like every other step. A worldserver that times out
	// leaves MySQL and authserver running, and a half-started stack is the
	// outcome this whole function exists to prevent.
	if err := ensure(SvcWorld); err != nil {
		return rollback(err)
	}

	return nil
}

// alreadyUp asks the operating system whether a service is running right now.
//
// The cached status is not good enough for a start decision. Refresh runs once
// a second, and a start taken on a stale cache is a start taken on a
// coincidence - which is how a second mysqld gets launched.
func (s *Supervisor) alreadyUp(id ServiceID) (bool, []int, error) {
	pids, err := s.ops.FindByName(imageExe[id])
	if err != nil {
		return false, nil, err
	}
	return len(pids) > 0, pids, nil
}

// StartService brings up one service, doing nothing if it is already running.
func (s *Supervisor) StartService(ctx context.Context, id ServiceID) error {
	if err := s.begin(); err != nil {
		return err
	}
	defer s.end()

	// Live, not cached: the row button is the reason this matters. A start
	// decided on a status that is up to a second old decides whether a
	// duplicate process appears.
	if up, pids, err := s.alreadyUp(id); err != nil {
		return fmt.Errorf("%s: %w", i18n.Tf("start.probe.fail", id), err)
	} else if up {
		s.logf("%s: уже запущен (PID %s) — ничего не делаю", id, joinInts(pids))
		return nil
	}

	// A world cannot come up without its database. Saying so here costs one
	// sentence; letting it try costs the minute worldserver spends failing
	// against a dead database before dying in a screenful of errors.
	//
	// Only the launch is refused, and only on a positive answer: if the
	// process list cannot be read, the start goes ahead rather than being
	// blocked by a guess.
	if id == SvcWorld {
		if up, _, err := s.alreadyUp(SvcMySQL); err == nil && !up {
			return errors.New(i18n.T("start.world.needsdb"))
		}
	}

	if err := s.launch(ctx, id); err != nil {
		return err
	}
	if err := s.awaitReady(ctx, id); err != nil {
		// It exists now, so it gets stopped - same rule as the rollback in
		// StartAll, and unchecked for the same reason: this is cleanup of a
		// process this call just started.
		s.stopOneUnchecked(ctx, id)
		return err
	}
	if id == SvcMySQL {
		return s.pingDatabase(ctx)
	}
	return nil
}

func (s *Supervisor) isRunning(id ServiceID) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	sv := s.services[id]
	if sv == nil {
		return false
	}
	return sv.status.State == StateRunning ||
		sv.status.State == StateExternal ||
		sv.status.State == StateStarting
}

// readyTimeout is how long a service may take to become ready.
//
// The world gets the long budget and auth gets the short one, and the
// distinction is not cosmetic: world init measured 46 s warm and over two
// minutes cold, while auth answers in seconds. Sharing auth's 30 seconds
// declares a healthy world dead while it is still loading - and the
// rollback then pulls MySQL out from under a server that had not finished
// connecting. That is not hypothetical; the first integration run did
// exactly this.
func (s *Supervisor) readyTimeout(id ServiceID) time.Duration {
	if id == SvcWorld {
		return s.conf.Start.WorldMarker
	}
	return s.conf.Start.AuthPort
}

// launch starts the process and nothing else.
//
// Split from awaitReady so the caller can register the service for rollback
// the moment a process exists. A process that was launched but never became
// ready is still a process, and forgetting it leaves the half-started stack
// this design exists to prevent - which is also what the first integration
// run did.
func (s *Supervisor) launch(ctx context.Context, id ServiceID) error {
	var (
		out io.Writer
		w   *markerWatcher
	)

	if id == SvcMySQL {
		out = newLogTap(s.logger(), "mysql")
	} else {
		marker := ""
		if id == SvcWorld {
			marker = worldReadyMarker
		}
		w = newMarkerWatcherWith(marker, newLogTap(s.logger(), string(id)))
		out = w
		s.rememberWatcher(id, w)
	}

	proc, err := s.ops.Start(s.exeFor(id), s.argsFor(id), s.lay.Root, out)
	if err != nil {
		return fmt.Errorf("%s: %w", id, err)
	}
	s.track(id, proc)
	s.ops.HideConsole(proc.PID())
	return nil
}

// awaitReady waits for a launched service to serve.
func (s *Supervisor) awaitReady(ctx context.Context, id ServiceID) error {
	port := servicePort(id)
	budget := s.readyTimeout(id)

	if !waitUntil(ctx, s.clk, budget, func() bool { return s.ops.Listen(port) }) {
		return errors.New(i18n.Tf("start.port.timeout", id, port, budget))
	}

	// The port is the readiness signal. The initialization line is a
	// progress indicator only, and a worldserver that has finished
	// connecting but whose line was missed is running, not broken.
	if id == SvcWorld {
		if w := s.watcherFor(SvcWorld); w != nil && !w.Reached() {
			s.logf("мир: порт %d открылся, но строка %q не встретилась (%d строк вывода)",
				port, worldReadyMarker, w.Lines())
		}
	}

	s.markRunning(id)
	return nil
}

// pingDatabase proves the database answers, not merely that something is
// listening.
//
// A port that accepts connections while the database refuses them means
// something else owns it - a second mysqld, or an unrelated service. That
// sentence is the difference between a user who fixes it in a minute and one
// who files a bug.
func (s *Supervisor) pingDatabase(ctx context.Context) error {
	if s.pinger == nil {
		return errors.New(i18n.T("start.db.unconfigured"))
	}

	pingCtx, cancel := context.WithTimeout(ctx, s.conf.Start.DBPing)
	defer cancel()

	if _, err := s.pinger.Ping(pingCtx); err != nil {
		if s.ops.Listen(portMySQL) {
			return errors.New(i18n.Tf("start.db.foreign", portMySQL, err))
		}
		return fmt.Errorf("%s: %w", i18n.T("start.db.prefix"), err)
	}
	return nil
}

// exeFor is the executable a service is launched from.
func (s *Supervisor) exeFor(id ServiceID) string {
	switch id {
	case SvcAuth:
		return s.lay.AuthserverExe
	case SvcWorld:
		return s.lay.WorldserverExe
	default:
		return s.lay.MySQLdExe
	}
}

func (s *Supervisor) argsFor(id ServiceID) []string {
	switch id {
	case SvcAuth:
		return []string{"-c", s.lay.AuthConfRel}
	case SvcWorld:
		return []string{"-c", s.lay.WorldConfRel}
	default:
		return mysqlArgs
	}
}

// mysqlArgs are the flags MySQL is started with.
//
// They are the repack's own, taken from MySQL.bat and AGENTS.md section 3,
// and each one earns its place. --console puts MySQL's output on the pipe
// this app reads; without it mysqld logs only to mysql\data\<host>.err and
// logs\wow-hub.log stays empty for MySQL. The buffer pool, packet size and
// isolation level are the values the repack ships with and the ones its
// documentation tells the user to run.
//
// --skip-log-bin and --innodb_buffer_pool_size=1G were added 2026-10-04, the
// settings that Claude's list and the mod-playerbots docs agree on: with
// log_bin=ON and sync_binlog=1 every commit fsynced twice (redo plus binlog)
// and wrote 369 MB of binlogs per 5-day window, and nothing here reads them -
// no replication, gtid_mode=OFF, and backups go through --single-transaction.
// The 4G from the docs is left alone on purpose: the whole database is 1.25 GB,
// so a pool bigger than it is reserved memory nobody fills.
//
// --innodb_buffer_pool_instances is gone because MySQL 8.4 ignores it. It was
// passed as 4 in both lists, and SHOW VARIABLES reported 1: the variable is
// deprecated and the instance count is derived. A flag that reads like a
// tuning knob and does nothing is worse than no flag.
//
// --binlog_expire_logs_seconds is inert while --skip-log-bin is on. It is kept
// so re-enabling binary logging keeps the 5-day retention this setup wants.
//
// Starting mysqld with no arguments at all - which is what this did before -
// silently ran the database on compiled-in defaults instead: a 128M buffer
// pool rather than 512M, a 64M packet limit rather than 128M, and
// REPEATABLE-READ rather than the READ-COMMITTED the repack is tuned for.
//
// Keep in sync with MySQL.bat.
var mysqlArgs = []string{
	"--standalone",
	"--console",
	"--skip-log-bin",
	"--max_allowed_packet=128M",
	"--innodb_redo_log_capacity=200MB",
	"--binlog_expire_logs_seconds=432000",
	"--innodb_buffer_pool_size=1G",
	"--innodb_io_capacity=500",
	"--innodb_io_capacity_max=2500",
	"--innodb_use_fdatasync=ON",
	"--innodb_log_buffer_size=32M",
	"--transaction_isolation=READ-COMMITTED",
}

// rememberWatcher keeps a service's output watcher so awaitReady can report
// on it later. Guarded by its own lock: the watcher is written on the start
// path and read on the readiness path.
func (s *Supervisor) rememberWatcher(id ServiceID, w *markerWatcher) {
	s.watchMu.Lock()
	defer s.watchMu.Unlock()
	s.watchers[id] = w
}

func (s *Supervisor) watcherFor(id ServiceID) *markerWatcher {
	s.watchMu.Lock()
	defer s.watchMu.Unlock()
	return s.watchers[id]
}

func (s *Supervisor) logger() *logx.Logger {
	s.logMu.Lock()
	defer s.logMu.Unlock()
	return s.log
}

func joinInts(pids []int) string {
	parts := make([]string, len(pids))
	for i, p := range pids {
		parts[i] = fmt.Sprint(p)
	}
	return strings.Join(parts, ", ")
}

var _ io.Writer = (*markerWatcher)(nil)
var _ io.Writer = (*logTap)(nil)
