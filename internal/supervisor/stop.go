package supervisor

import (
	"context"
	"errors"
	"fmt"
	"time"

	"wow-hub/internal/i18n"
	"wow-hub/internal/notify"
	"wow-hub/internal/osops"
	"wow-hub/internal/repack"
)

// Stop order. worldserver writes to the database as it shuts down, so if
// MySQL leaves first it leaves the server writing into a dead connection,
// where it may hang or lose an unsaved character. This order is not a
// preference.
var stopOrder = []ServiceID{SvcWorld, SvcAuth, SvcMySQL}

// stopAgain is how long the process gets after a kill before the operation
// gives up on it being gone.
const stopAgain = 10 * time.Second

// StopAll stops everything in order, reverse of the start sequence.
//
// A failure does not abort the chain, but the dependency guard does: if the
// world refuses to die, the stops of auth and MySQL are refused in turn
// rather than carried out. That is the point of the guard - a chain that
// "continues anyway" would shut the database down underneath a live
// worldserver, which is the one outcome this whole package exists to
// prevent. Everything that is safe to stop still gets stopped, and the
// failures are joined and returned.
func (s *Supervisor) StopAll(ctx context.Context) error {
	if err := s.begin(); err != nil {
		return err
	}
	defer s.end()

	var problems []error
	for _, id := range stopOrder {
		if err := s.stopOne(ctx, id); err != nil {
			problems = append(problems, err)
		}
	}
	if len(problems) > 0 {
		return errors.Join(problems...)
	}
	return nil
}

// StopService stops one service.
func (s *Supervisor) StopService(ctx context.Context, id ServiceID) error {
	if err := s.begin(); err != nil {
		return err
	}
	defer s.end()

	return s.stopOne(ctx, id)
}

// ShutdownCommand is the console command that stops these servers cleanly.
//
// The argument is the shutdown countdown in seconds - the world logs
// "Server shutdown in N second(s)." and gives online players that long to
// disconnect - not an exit code. Measured on this worldserver: written to
// stdin once the world is ready, it produces the countdown, every database
// pool closes in order, and module shutdown hooks run. CTRL_BREAK does none
// of that; measured against the same ready world it left the process
// untouched for a full minute.
//
// spec 12.1 claimed this path could not work, on the grounds that the console
// is read through _kbhit and a pipe is not a console. That conclusion came
// from symbols in a PDB, which proves nothing, and these servers react to
// stdin in another way entirely - an EOF on stdin makes them shut themselves
// down. Whatever reads stdin does read it.
const ShutdownCommand = "server shutdown 1"

// shutdownWait is how long the console command gets before the ladder moves
// on. The countdown is one second; the rest is pool teardown.
const shutdownWait = 60 * time.Second

// stopOne stops one service, refusing when a service that depends on it is
// still running.
//
// The guard lives here, at the single choke point every stop passes through,
// rather than in the UI. The row buttons, StopAll and Restart all arrive in
// this function; a check in the frontend would protect none of the other
// two, and Restart - which stops before it starts - is the easiest one to
// fire by accident.
func (s *Supervisor) stopOne(ctx context.Context, id ServiceID) error {
	if dep := s.dependentRunning(id); dep != "" {
		return errors.New(i18n.Tf("stop.denied", id, dep, dep))
	}
	return s.stopOneUnchecked(ctx, id)
}

// dependentRunning names a still-running service that must be stopped before
// id may be, or "" when nothing blocks it.
func (s *Supervisor) dependentRunning(id ServiceID) ServiceID {
	for _, dep := range dependents[id] {
		if s.stillRunning(dep) {
			return dep
		}
	}
	return ""
}

// stopOneUnchecked runs the escalation ladder for a single service without
// consulting the dependency guard.
//
// It exists for the cleanup paths - StartAll's rollback and StartService's
// failure path - which are removing something this run just started rather
// than deliberately taking a dependency out from under a running world. A
// guard there would refuse to clean up a broken MySQL merely because an
// unrelated authserver happened to be running.
//
// Four rungs, in order:
//
//  1. the console command on stdin - the only path measured to give a clean
//     shutdown, and therefore the only one that saves online characters;
//  2. CTRL_BREAK - harmless, and it may work on a build where the command
//     reader is unreachable;
//  3. ask, then kill - never without asking, and never before the two
//     graceful attempts have actually been made;
//  4. nothing further: report.
//
// Rung 1 needs a process we started, because only then do we hold its stdin.
// An adopted process has neither, so it goes to rung 2 with no warning in
// advance - the dialog is where the user is told.
//
// The exit code is never the success signal: a clean shutdown of this
// worldserver returns 0xc0000374.
func (s *Supervisor) stopOneUnchecked(ctx context.Context, id ServiceID) error {
	s.mu.Lock()
	sv := s.services[id]
	if sv == nil {
		s.mu.Unlock()
		return errors.New(i18n.Tf("stop.unknown", id))
	}
	if sv.status.State == StateStopped && sv.proc == nil {
		s.mu.Unlock()
		return nil // already down; a second click must do nothing
	}
	pid := sv.status.PID
	external := sv.status.State == StateExternal
	proc := sv.proc
	sv.stopping = true
	sv.status.State = StateStopping
	sv.rev++
	s.mu.Unlock()

	if id == SvcMySQL {
		return s.stopMySQL(ctx, pid)
	}

	// Where the world's log stood before the first attempt. Taken up front
	// because the point is to judge only what this stop appends - an earlier
	// run's "Server shutdown in 3 second(s)" must not make this one look
	// clean. See worldlog.go.
	var worldNote shutdownNote
	if id == SvcWorld {
		worldNote = s.noteWorldLog()
	}

	// Rung 1: stdin. The primary route, and the only one measured to give a
	// clean shutdown of the worldserver. Requires a process we started.
	//
	// authserver has no such command - it stops on Ctrl+C - so it skips
	// straight to rung 2. Writing the command at it and waiting a minute
	// would waste a minute on every stop.
	//
	// An adopted world is out of reach, and that is measured rather than
	// assumed. Three ways of reaching it were tried against the real binary:
	// the stdin pipe (the panel holding it is gone), a command typed into its
	// console (its stdin is the pipe, not the console, so the keystrokes sit
	// in a buffer nobody reads), and a console control event. The last one is
	// the awkward one: CTRL_C_EVENT stopped the world on one run, with all
	// four pools closing in order, and did nothing at all on the next;
	// CTRL_BREAK_EVENT did nothing either. Two runs out of three say nothing
	// can be promised to the user, so it is not offered as a rung.
	//
	// A named pipe as stdin does not help either, and for a reason worth
	// remembering: it works while its client stays connected, and the moment
	// that client disconnects the child reads the end of stream and shuts
	// itself down - the same trap, renamed. A second CreateFile on the same
	// name answers "All pipe instances are busy".
	//
	// So an adopted world goes to the kill question, which is why that
	// question has to name what it costs - see askConfirm. The one channel
	// that would fix this properly is the world's own RA port: it listens,
	// so it survives the panel exactly as MySQL does. It is disabled in this
	// repack (Ra.Enable = 0) and needs credentials of its own.
	if proc != nil && !external && id == SvcWorld {
		if err := s.sendShutdownCommand(proc, id, pid); err != nil {
			s.logf("команда остановки для %s не доставлена: %v", id, err)
		} else if waitGone(ctx, s.clk, s.ops, pid, shutdownWait) {
			s.logf("%s остановлен командой %q", id, ShutdownCommand)
			s.markWorldStopped(worldNote)
			return nil
		} else {
			s.logf("%s не остановился командой %q за %s", id, ShutdownCommand, shutdownWait)
		}

		// Rung 1b: the same command typed into the console instead. Different
		// transport - real keystrokes rather than a pipe - for a build that
		// reads its console rather than stdin.
		if err := s.ops.SendLine(pid, ShutdownCommand); err != nil {
			s.logf("ввод команды в консоль %s не удался: %v", id, err)
		} else if waitGone(ctx, s.clk, s.ops, pid, shutdownWait) {
			s.logf("%s остановлен командой, введённой в консоль", id)
			s.markWorldStopped(worldNote)
			return nil
		}
	}

	// Rung 2: Ctrl+C into the process's own console.
	//
	// Not GenerateConsoleCtrlEvent with a process group: measured, that left
	// a fully-initialized worldserver untouched for 60 seconds. A process in
	// its own group stops receiving control events delivered that way, which
	// is why the flag is no longer set at startup. Attaching to the server's
	// console and raising CTRL_C_EVENT there reaches it - the same way a
	// human pressing Ctrl+C in the window would.
	if !external && pid > 0 {
		if err := s.ops.SendCtrlC(pid); err != nil {
			s.logf("Ctrl+C для %s (PID %d) не доставлен: %v", id, pid, err)
		}
		if waitGone(ctx, s.clk, s.ops, pid, s.conf.StopTimeout) {
			if id == SvcWorld {
				s.markWorldStopped(worldNote)
			} else {
				s.markStopped(id)
			}
			return nil
		}
	}

	// Rung 3.
	agreed, answered := s.askConfirm(ctx, id, pid)
	if !agreed {
		// The service and PID belong in the refusal. The user clicked a
		// button on one row; an error that does not name that row leaves
		// them working out which of three processes is still running.
		msg := fmt.Errorf("%s (PID %d): %w", id, pid, stopRefused(answered))
		s.markStopFailed(id, pid, msg)
		return msg
	}

	if err := s.ops.Kill(pid); err != nil {
		msg := fmt.Errorf("%s: %w", i18n.Tf("stop.kill.fail", id, pid), err)
		s.markStopFailed(id, pid, msg)
		return msg
	}

	if waitGone(ctx, s.clk, s.ops, pid, stopAgain) {
		if id == SvcWorld {
			s.markWorldStopped(worldNote)
		} else {
			s.markStopped(id)
		}
		return nil
	}

	msg := errors.New(i18n.Tf("stop.kill.alive", id, pid))
	s.markStopFailed(id, pid, msg)
	return msg
}

// sendShutdownCommand writes the console command into the child's stdin.
//
// The newline is not optional: these servers read a line at a time, and
// without it the command sits in the buffer unread and the world simply
// stays up.
func (s *Supervisor) sendShutdownCommand(proc osops.Proc, id ServiceID, pid int) error {
	if _, err := fmt.Fprintf(proc.Stdin(), "%s\n", ShutdownCommand); err != nil {
		return fmt.Errorf("запись в stdin: %w", err)
	}
	s.logf("%s (PID %d): отправлена команда %q", id, pid, ShutdownCommand)
	return nil
}

// markWorldStopped records a completed world stop and reports how clean it
// was.
//
// The classification is the whole reason this app exists. Pools closing is
// not the same as the main loop's save countdown running, and only the
// countdown writes out the players who were still online. Grading the stop
// here means one that did not save says so, in the UI, instead of looking
// exactly like every other stop.
func (s *Supervisor) markWorldStopped(note shutdownNote) {
	s.markStopped(SvcWorld)
	s.reportWorldShutdown(note)
}

func (s *Supervisor) reportWorldShutdown(note shutdownNote) {
	kind := s.classifyWorldShutdown(note)
	if kind == ShutdownClean {
		s.logInfof("мир остановлен чисто: цикл сохранения выполнен, пулы БД закрыты")
		return
	}

	msg := PartialShutdownWarning(kind)
	s.logf("остановка мира оценена как %q: %s", kind, msg)
	if s.sink != nil {
		s.sink.Emit(notify.Event{
			Type: notify.EventOperation,
			Data: map[string]any{"level": notify.LevelWarn, "text": msg},
		})
	}
}

// stopMySQL shuts MySQL down with mysqladmin and never signals it.
//
// mysqladmin is a clean shutdown; a signal to mysqld is not. Killing InnoDB
// is how databases get hurt, and the whole reason this app exists is to stop
// the server without damaging a character.
//
// The credentials come from repack: root/test as the repack documents them,
// overridden by temp\mysql_root.conf when that file exists, so a user who
// changed the maintenance password can still stop MySQL cleanly. The working
// account (acore) cannot be used - it has no SHUTDOWN privilege.
func (s *Supervisor) stopMySQL(ctx context.Context, pid int) error {
	exe := s.lay.MySQLadminExe
	if exe == "" {
		return errors.New(i18n.T("stop.mysql.noadmin"))
	}

	user, password := repack.MySQLRootCredentials(s.lay.MySQLRootConf)

	if err := s.ops.Run(exe, "-u", user, "-p"+password, "shutdown"); err != nil {
		msg := fmt.Errorf("%s: %w", i18n.T("stop.mysql.adminfail"), err)
		s.markStopFailed(SvcMySQL, 0, msg)
		return msg
	}

	// Wait for the PROCESS, not for the port.
	//
	// Measured: mysqladmin returns as soon as the shutdown is under way, and
	// the port stops listening while mysqld is still alive. Waiting on the
	// port made the table say "stopped" while the database process was still
	// running - a lie the user acts on, and one that would break the next
	// start. The process is the thing that has to be gone.
	if !waitGone(ctx, s.clk, s.ops, pid, s.conf.StopTimeout) {
		msg := errors.New(i18n.Tf("stop.mysql.alive", pid, portMySQL))
		s.markStopFailed(SvcMySQL, pid, msg)
		return msg
	}

	s.markStopped(SvcMySQL)
	return nil
}

// stopRefused explains why no kill happened. Silence and refusal are
// reported differently, because a user who dismissed the dialog and a user
// who never saw it need different next steps.
func stopRefused(answered bool) error {
	if answered {
		return errors.New(i18n.T("stop.confirm.declined"))
	}
	return errors.New(i18n.T("stop.confirm.unanswered"))
}

func (s *Supervisor) markStopped(id ServiceID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sv := s.services[id]; sv != nil {
		s.clearLocked(sv)
	}
}

// markStopFailed records that a stop did not complete. The state goes back
// to what it was rather than to "stopped": the process is very likely still
// running, and a table claiming otherwise is a lie the user acts on.
func (s *Supervisor) markStopFailed(id ServiceID, pid int, cause error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	sv := s.services[id]
	if sv == nil {
		return
	}
	sv.stopping = false
	sv.status.State = StateFailed
	sv.status.Detail = cause.Error()
	if pid > 0 {
		sv.status.PID = pid
	}
	sv.rev++
	s.logf("остановка %s не завершена: %v", id, cause)
}

func (s *Supervisor) logf(format string, args ...any) {
	// logMu, not s.mu: markStopFailed calls this while already holding s.mu,
	// and a sync.Mutex is not reentrant.
	s.logMu.Lock()
	l := s.log
	s.logMu.Unlock()
	if l != nil {
		l.Error(fmt.Sprintf(format, args...))
	}
}

// logInfof logs a line that is not a problem. logf is the ERROR channel and
// stays that way: a clean shutdown reported as an error would teach the user
// to ignore the error level entirely.
func (s *Supervisor) logInfof(format string, args ...any) {
	s.logMu.Lock()
	l := s.log
	s.logMu.Unlock()
	if l != nil {
		l.Info(fmt.Sprintf(format, args...))
	}
}
