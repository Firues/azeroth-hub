package supervisor

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"wow-hub/internal/notify"
	"wow-hub/internal/osops"
)

// stopFixture wires a supervisor whose clock and confirmer are under test
// control, with all three services up and owned.
type stopFixture struct {
	sup   *Supervisor
	ops   *osops.FakeOps
	clk   *osops.FakeClock
	sink  *notify.Recorder
	conf  *fakeConfirmer
	procs map[ServiceID]*osops.FakeProc
}

func newStopFixture(t *testing.T) *stopFixture {
	t.Helper()
	ops := osops.NewFake()
	clk := osops.NewFakeClock()
	sink := notify.NewRecorder()
	conf := &fakeConfirmer{answers: []bool{true}}
	sup := New(ops, clk, testLayout(), sink, nil, conf, Conf{})

	f := &stopFixture{sup: sup, ops: ops, clk: clk, sink: sink, conf: conf,
		procs: map[ServiceID]*osops.FakeProc{}}
	for _, id := range []ServiceID{SvcMySQL, SvcAuth, SvcWorld} {
		p := osops.NewFakeProc(pidFor(id))
		// A server obeys its console command. Measured on the real
		// binaries: "server shutdown 1" on stdin makes them shut down
		// cleanly. Modelling that here is what lets the graceful rung be
		// the normal path in tests instead of every stop falling through to
		// a kill.
		p.OnWrite = func() { ops.MarkDead(p.PID()) }
		f.procs[id] = p

		// The process list has to agree that it is running. The dependency
		// guard asks the OS, not the cached table, so without this the guard
		// would wave through stops the real machine refuses.
		ops.Procs[imageExe[id]] = []int{p.PID()}

		sup.track(id, p)
		sup.markRunning(id)
	}

	// mysqladmin shutting mysqld down is what actually stops MySQL, so the
	// fake has to model it - otherwise every StopAll test would end with
	// MySQL still alive and the guard would refuse nothing.
	ops.OnRun = func(string, ...string) error {
		ops.MarkDead(pidFor(SvcMySQL))
		return nil
	}

	return f
}

// commandFails makes the fixture model a server that ignores its console
// command, so the ladder's later rungs are reachable.
//
// The default is the opposite, because that is what the binaries do: measured
// on the real worldserver, "server shutdown 1" on stdin runs the save
// countdown and closes every pool. A fixture that ignored it would make every
// ordinary stop look like a disaster and would test nothing that happens in
// practice.
func (f *stopFixture) commandFails() {
	for _, p := range f.procs {
		p.OnWrite = nil
	}
}

// commandPathsFail makes the two COMMAND routes fail while leaving Ctrl+C
// working. That is the shape a test about the Ctrl+C rung needs: it has to
// reach rung 2, which means rungs 1 and 1b must not have already finished
// the job.
func (f *stopFixture) commandPathsFail() {
	f.commandFails()
	f.ops.OnSendLine = func(int, string) error { return nil }
}

// gracefulPathsFail makes every automatic path fail, which is the measured
// behaviour of a server that ignores stdin and ignores Ctrl+C. Needed by any
// test about escalation: FakeOps marks a process dead on a successful
// SendCtrlC, so without this the ladder stops at rung 2 and the kill - the
// thing under test - is never reached.
func (f *stopFixture) gracefulPathsFail() {
	f.commandFails()
	f.ops.OnSendLine = func(int, string) error { return nil }
	f.ops.OnSendCtrlC = func(int) error { return nil }
}

func pidFor(id ServiceID) int {
	switch id {
	case SvcMySQL:
		return 1000
	case SvcAuth:
		return 2000
	default:
		return 3000
	}
}

// --- the dependency guard ---

func TestStopServiceRefusesToStopMySQLWhileTheWorldRuns(t *testing.T) {
	// The row buttons are the easiest way to do this by accident: MySQL and
	// auth both carry a Stop button, and neither has any idea what else is
	// running. Stopping MySQL under a live worldserver is how an unsaved
	// character is lost.
	f := newStopFixture(t)

	err := f.sup.StopService(context.Background(), SvcMySQL)
	if err == nil {
		t.Fatal("StopService(mysql) succeeded while the world was running")
	}
	if !strings.Contains(err.Error(), "world") {
		t.Errorf("error = %q, want it to name what blocks the stop", err)
	}
	if len(f.ops.RunCalls) != 0 {
		t.Fatalf("RunCalls = %v, want mysqladmin never invoked", f.ops.RunCalls)
	}
	if got := find(f.sup.Status(), SvcMySQL).State; got != StateRunning {
		t.Errorf("mysql state = %q, want it untouched at %q", got, StateRunning)
	}
}

func TestStopServiceRefusesToStopAuthWhileTheWorldRuns(t *testing.T) {
	f := newStopFixture(t)

	err := f.sup.StopService(context.Background(), SvcAuth)
	if err == nil {
		t.Fatal("StopService(auth) succeeded while the world was running")
	}
	if !strings.Contains(err.Error(), "world") {
		t.Errorf("error = %q, want it to name the world", err)
	}
	if len(f.ops.CtrlCCalls) != 0 {
		t.Fatalf("CtrlCCalls = %v, want auth left alone", f.ops.CtrlCCalls)
	}
}

func TestStopServiceAllowsMySQLOnceItsDependentsAreGone(t *testing.T) {
	// The guard must not be a wall. In the right order - world, then auth,
	// then MySQL - everything still stops.
	f := newStopFixture(t)

	for _, id := range []ServiceID{SvcWorld, SvcAuth} {
		if err := f.sup.StopService(context.Background(), id); err != nil {
			t.Fatalf("StopService(%s): %v", id, err)
		}
	}
	if err := f.sup.StopService(context.Background(), SvcMySQL); err != nil {
		t.Fatalf("StopService(mysql) after the world and auth are gone: %v", err)
	}
	if len(f.ops.RunCalls) != 1 {
		t.Fatalf("RunCalls = %v, want mysqladmin invoked once", f.ops.RunCalls)
	}
}

func TestRestartOfMySQLIsRefusedWhileTheWorldRuns(t *testing.T) {
	// Restart stops before it starts, so it is the same hazard behind a
	// friendlier name.
	f := newStopFixture(t)

	if err := f.sup.StopService(context.Background(), SvcMySQL); err == nil {
		t.Fatal("StopService(mysql) was allowed; the Restart path rests on this")
	}
}

// --- the stop ladder ---

func TestStopCtrlCStopsWhenCommandsFail(t *testing.T) {
	f := newStopFixture(t)
	f.commandPathsFail()
	pid := pidFor(SvcWorld)

	// The ladder waits on the clock before reaching the signal rung, so the
	// test has to advance it.
	done := make(chan error, 1)
	go func() { done <- f.sup.StopService(context.Background(), SvcWorld) }()
	if err := f.pump(done, 5*time.Second); err != nil {
		t.Fatalf("StopService: %v", err)
	}

	if len(f.ops.CtrlCCalls) != 1 || f.ops.CtrlCCalls[0] != pid {
		t.Fatalf("CtrlCCalls = %v, want [%d]", f.ops.CtrlCCalls, pid)
	}
	if len(f.ops.KillCalls) != 0 {
		t.Fatalf("KillCalls = %v, want none when the signal worked", f.ops.KillCalls)
	}
	if got := find(f.sup.Status(), SvcWorld).State; got != StateStopped {
		t.Fatalf("state = %q, want %q", got, StateStopped)
	}
}

func TestStopEscalatesToKillWhenNothingAutomaticWorks(t *testing.T) {
	// The whole point of the ladder. A worldserver that ignores its console
	// and its Ctrl+C must not be left running forever, and must not be
	// killed without asking either.
	f := newStopFixture(t)
	f.gracefulPathsFail()
	pid := pidFor(SvcWorld)

	done := make(chan error, 1)
	go func() { done <- f.sup.StopService(context.Background(), SvcWorld) }()

	if err := f.pump(done, 5*time.Second); err != nil {
		t.Fatalf("StopService: %v, want nil once the kill was agreed to", err)
	}

	if len(f.ops.CtrlCCalls) != 1 || f.ops.CtrlCCalls[0] != pid {
		t.Errorf("CtrlCCalls = %v, want exactly one attempt for %d - giving up before "+
			"trying the signal would be giving up before trying anything",
			f.ops.CtrlCCalls, pid)
	}
	if len(f.ops.KillCalls) != 1 || f.ops.KillCalls[0] != pid {
		t.Fatalf("KillCalls = %v, want [%d]", f.ops.KillCalls, pid)
	}
	if len(f.conf.asked) != 1 {
		t.Errorf("confirmer asked %d times, want exactly 1 - killing must never be silent", len(f.conf.asked))
	}
}

func TestStopAbortsWhenUserDeclines(t *testing.T) {
	// Never report success for a stop that did not happen. A returned nil
	// here would leave the UI showing "stopped" while the server runs.
	f := newStopFixture(t)
	f.commandPathsFail()
	f.gracefulPathsFail()
	f.conf.answers = []bool{false}

	done := make(chan error, 1)
	go func() { done <- f.sup.StopService(context.Background(), SvcWorld) }()

	err := f.pump(done, 5*time.Second)
	if err == nil {
		t.Fatal("StopService returned nil after the user refused the kill")
	}
	if !strings.Contains(err.Error(), "отмен") {
		t.Fatalf("error = %q, want it to say the user cancelled", err)
	}
	if len(f.ops.KillCalls) != 0 {
		t.Fatalf("KillCalls = %v, want none after a refusal", f.ops.KillCalls)
	}
	if got := find(f.sup.Status(), SvcWorld).State; got != StateFailed {
		t.Errorf("state = %q, want %q - the process is still running and the table must not say stopped", got, StateFailed)
	}
}

func TestStopTreatsDialogTimeoutAsDecline(t *testing.T) {
	// The backend must never sit blocked on the UI. Silence for longer than
	// the timeout counts as a refusal and the operation unwinds.
	f := newStopFixture(t)
	f.gracefulPathsFail()
	f.conf.answers = nil // never answers

	done := make(chan error, 1)
	go func() { done <- f.sup.StopService(context.Background(), SvcWorld) }()

	err := f.pump(done, 5*time.Second)
	if err == nil {
		t.Fatal("StopService returned nil after the dialog timed out")
	}
	if len(f.ops.KillCalls) != 0 {
		t.Fatalf("KillCalls = %v, want none after a dialog timeout", f.ops.KillCalls)
	}
	// The dialog has to be taken off the screen. Leaving it up shows a
	// question the backend has stopped listening to.
	if got := f.sink.CountOf(notify.EventConfirmCancel); got != 1 {
		t.Fatalf("confirm-cancel emitted %d times, want 1 so the page can close the dialog", got)
	}
}

func TestStopExternalProcessGoesStraightToAsking(t *testing.T) {
	// An adopted process cannot be reached, and that is measured, not assumed.
	// Three routes were tried against the real worldserver:
	//
	//   - its stdin pipe: the panel that wrote to it is gone;
	//   - a command typed into its console: its stdin is that pipe, not the
	//     console, so the keystrokes sit in a buffer nobody reads;
	//   - a console control event: CTRL_C_EVENT stopped the world on one run
	//     and did nothing on the next, CTRL_BREAK_EVENT did nothing. Two runs
	//     out of three is not something to promise a user who is about to lose
	//     a character's progress.
	//
	// So there is nothing graceful left to try, and going straight to the kill
	// question is the honest thing. What that costs is the dialog's job to say:
	// the save pass will not run.
	f := newStopFixture(t)
	f.sup.forget(SvcWorld)
	f.sup.Refresh()

	done := make(chan error, 1)
	go func() { done <- f.sup.StopService(context.Background(), SvcWorld) }()
	f.pump(done, 5*time.Second)

	if len(f.ops.Lines) != 0 {
		t.Errorf("Lines = %v, want none - console typing does not reach an adopted world", f.ops.Lines)
	}
	if len(f.ops.CtrlCCalls) != 0 {
		t.Errorf("CtrlCCalls = %v, want none for an adopted process", f.ops.CtrlCCalls)
	}
	if len(f.conf.asked) != 1 {
		t.Errorf("confirmer asked %d times, want exactly 1", len(f.conf.asked))
	}
}

func TestStopSendsTheConsoleCommandFirst(t *testing.T) {
	// This is the only rung measured to give a clean shutdown. It has to
	// happen before any signal, because a signal costs an online character
	// up to PlayerSaveInterval.
	f := newStopFixture(t)

	done := make(chan error, 1)
	go func() { done <- f.sup.StopService(context.Background(), SvcWorld) }()

	// pump, not a bare select: after writing the command the ladder waits on
	// the clock, so a test that never advances it waits forever.
	if err := f.pump(done, 5*time.Second); err != nil {
		t.Fatalf("StopService: %v, want nil - the command path should have worked", err)
	}

	if got := f.procs[SvcWorld].StdinWritten(); !strings.Contains(got, ShutdownCommand) {
		t.Fatalf("stdin = %q, want it to carry %q", got, ShutdownCommand)
	}
	if !strings.HasSuffix(f.procs[SvcWorld].StdinWritten(), "\n") {
		t.Error("the command is not newline-terminated; a line-reading server would never see it")
	}
	if len(f.ops.KillCalls) != 0 {
		t.Errorf("KillCalls = %v, want none - the command succeeded", f.ops.KillCalls)
	}
	if len(f.conf.asked) != 0 {
		t.Errorf("the user was asked to force-kill %d times, want 0 - nothing needed killing", len(f.conf.asked))
	}
}

func TestStopTwiceIsANoOp(t *testing.T) {
	// People click twice. A second kill of a process another instance is
	// already killing helps nobody.
	f := newStopFixture(t)

	if err := f.sup.StopService(context.Background(), SvcWorld); err != nil {
		t.Fatalf("first StopService: %v", err)
	}
	beforeBreak, beforeKill := len(f.ops.CtrlCCalls), len(f.ops.KillCalls)

	if err := f.sup.StopService(context.Background(), SvcWorld); err != nil {
		t.Fatalf("second StopService: %v, want nil for an already-stopped service", err)
	}

	if len(f.ops.CtrlCCalls) != beforeBreak || len(f.ops.KillCalls) != beforeKill {
		t.Fatalf("second stop issued signals: CtrlCCalls %d->%d, KillCalls %d->%d",
			beforeBreak, len(f.ops.CtrlCCalls), beforeKill, len(f.ops.KillCalls))
	}
}

func TestStopReportsKillFailureVerbatim(t *testing.T) {
	// The user can act on "taskkill failed: access denied" and on nothing
	// else. Hiding the text behind "could not stop" wastes the only clue
	// they have.
	f := newStopFixture(t)
	f.gracefulPathsFail()
	f.ops.OnKill = func(pid int) error { return errors.New("access is denied") }

	done := make(chan error, 1)
	go func() { done <- f.sup.StopService(context.Background(), SvcWorld) }()

	err := f.pump(done, 5*time.Second)
	if err == nil {
		t.Fatal("StopService returned nil although the kill failed")
	}
	if !strings.Contains(err.Error(), "access is denied") {
		t.Errorf("error = %q, want the kill failure quoted verbatim", err)
	}
	if !strings.Contains(err.Error(), "3000") {
		t.Errorf("error = %q, want it to name the PID", err)
	}
}

func TestStopSetsStoppingStateDuringTheOperation(t *testing.T) {
	// The UI has to show that something is happening; a table frozen on
	// "running" for 30 seconds looks like the app has hung.
	f := newStopFixture(t)
	f.commandFails()

	done := make(chan error, 1)
	go func() { done <- f.sup.StopService(context.Background(), SvcWorld) }()
	go f.pump(done, 5*time.Second)

	// Wait for the state, not for a particular rung. Waiting for a rung
	// first deadlocks: the ladder cannot reach it until the clock advances,
	// and the clock is advanced by the pump.
	waitForCall(t, f, func() bool {
		return find(f.sup.Status(), SvcWorld).State == StateStopping
	})
	f.pump(done, 5*time.Second)
}

// --- StopAll ---

func TestStopAllUsesReverseOrder(t *testing.T) {
	// world, then auth, then MySQL - never the other way round. worldserver
	// writes to the database as it stops, and a MySQL that leaves first
	// leaves it writing into a dead connection.
	//
	// Every automatic route is made to fail so that all three stops leave a
	// trace in the same place, which is what makes the order observable.
	f := newStopFixture(t)
	f.gracefulPathsFail()
	f.conf.answers = []bool{true, true, true}

	done := make(chan error, 1)
	go func() { done <- f.sup.StopAll(context.Background()) }()
	if err := f.pump(done, 10*time.Second); err != nil {
		t.Fatalf("StopAll: %v", err)
	}

	want := []int{pidFor(SvcWorld), pidFor(SvcAuth)}
	if len(f.ops.CtrlCCalls) < 2 || f.ops.CtrlCCalls[0] != want[0] || f.ops.CtrlCCalls[1] != want[1] {
		t.Fatalf("CtrlCCalls = %v, want %v in that order", f.ops.CtrlCCalls, want)
	}
	if len(f.ops.RunCalls) != 1 {
		t.Fatalf("RunCalls = %v, want MySQL stopped exactly once through Run", f.ops.RunCalls)
	}
}

func TestStopAllUsesMysqladminForMySQL(t *testing.T) {
	// MySQL is never signalled. mysqladmin is a clean shutdown and a signal
	// to mysqld is not; taskkill on InnoDB is how databases get hurt.
	f := newStopFixture(t)

	done := make(chan error, 1)
	go func() { done <- f.sup.StopAll(context.Background()) }()
	if err := f.pump(done, 10*time.Second); err != nil {
		t.Fatalf("StopAll: %v", err)
	}

	if len(f.ops.RunCalls) != 1 {
		t.Fatalf("RunCalls = %v, want exactly one call", f.ops.RunCalls)
	}
	call := f.ops.RunCalls[0]
	joined := strings.Join(call, " ")
	if !strings.Contains(joined, "mysqladmin") {
		t.Fatalf("Run = %v, want mysqladmin", call)
	}
	if !strings.Contains(joined, "-u root") || !strings.Contains(joined, "-ptest") {
		t.Fatalf("Run = %v, want the documented root/-ptest credentials", call)
	}
	if !strings.HasSuffix(joined, "shutdown") {
		t.Fatalf("Run = %v, want it to end in shutdown", call)
	}
	for _, pid := range f.ops.CtrlCCalls {
		if pid == pidFor(SvcMySQL) {
			t.Fatalf("CtrlCCalls = %v, MySQL must never be signalled", f.ops.CtrlCCalls)
		}
	}
}

func TestStopAllNeverStopsMySQLUnderALiveWorld(t *testing.T) {
	// The rule this whole app exists for. If the world refuses to die - the
	// user said no, or the kill failed - MySQL must NOT be stopped: a live
	// worldserver writing into a dead database is how an unsaved character
	// is lost. auth is refused too, because the world depends on it.
	f := newStopFixture(t)
	f.gracefulPathsFail()
	f.conf.answers = []bool{false} // the user declines to force-kill the world

	done := make(chan error, 1)
	go func() { done <- f.sup.StopAll(context.Background()) }()

	err := f.pump(done, 10*time.Second)
	if err == nil {
		t.Fatal("StopAll returned nil although the world refused to stop")
	}
	if !strings.Contains(err.Error(), "world") {
		t.Errorf("error = %q, want it to name the world", err)
	}
	if len(f.ops.RunCalls) != 0 {
		t.Fatalf("RunCalls = %v, want MySQL untouched while the world is still running", f.ops.RunCalls)
	}
	if got := find(f.sup.Status(), SvcMySQL).State; got != StateRunning {
		t.Errorf("mysql state = %q, want it left alone", got)
	}
	if got := find(f.sup.Status(), SvcAuth).State; got != StateRunning {
		t.Errorf("auth state = %q, want it left alone while the world runs", got)
	}
}

func TestStopAllOnNothingRunningIsANoOp(t *testing.T) {
	f := newHarness(t)
	if err := f.sup.StopAll(context.Background()); err != nil {
		t.Fatalf("StopAll with everything stopped: %v", err)
	}
	if len(f.ops.KillCalls) != 0 || len(f.ops.RunCalls) != 0 {
		t.Fatalf("StopAll touched the system: %v %v", f.ops.KillCalls, f.ops.RunCalls)
	}
}

// --- what the stop actually did ---

func TestWorldStopWarnsWhenItCannotBeConfirmed(t *testing.T) {
	// With no log to read, the verdict is "unknown", and the app must say so
	// rather than quietly implying a clean stop. The clean/partial grading
	// itself is covered in worldlog_test.go; this is about it reaching the
	// user at all.
	f := newStopFixture(t)
	f.sup.lay.LogsDir = filepath.Join(t.TempDir(), "no-logs-here")

	if err := f.sup.StopService(context.Background(), SvcWorld); err != nil {
		t.Fatalf("StopService: %v", err)
	}

	var found bool
	for _, e := range f.sink.Events() {
		if e.Type != notify.EventOperation {
			continue
		}
		found = true
		d, ok := e.Data.(map[string]any)
		if !ok {
			t.Fatalf("operation event carries %T, want a level and a text", e.Data)
		}
		if d["level"] != notify.LevelWarn {
			t.Errorf("level = %v, want %q", d["level"], notify.LevelWarn)
		}
		if text, _ := d["text"].(string); text == "" {
			t.Error("the warning carries no text")
		}
	}
	if !found {
		t.Fatal("a world stop that could not be confirmed emitted no warning")
	}
}

func TestWorldStopStaysQuietWhenTheSaveCountdownRan(t *testing.T) {
	// The other half: a stop the log confirms as clean must not cry wolf. A
	// warning on every stop would be ignored on the one that matters.
	dir := t.TempDir()
	f := newStopFixture(t)
	f.sup.lay.LogsDir = dir

	path := writeServerLog(t, dir, "Server_2026-09-30_10_00_00.log", "starting up\n")

	// The child obeys the command and, on the way out, writes what a clean
	// shutdown writes. OnWrite fires when the command reaches it, which is
	// before waitGone observes the exit and therefore before the verdict is
	// read.
	f.procs[SvcWorld].OnWrite = func() {
		appendTo(t, path, "Server shutdown in 1 second(s).\r\n"+
			"Halting process...\r\n"+
			"All connections on DatabasePool 'acore_characters' closed.\r\n")
		f.ops.MarkDead(pidFor(SvcWorld))
	}

	if err := f.sup.StopService(context.Background(), SvcWorld); err != nil {
		t.Fatalf("StopService: %v", err)
	}

	for _, e := range f.sink.Events() {
		if e.Type == notify.EventOperation {
			t.Fatalf("a clean stop emitted a warning: %v", e.Data)
		}
	}
}

// --- helpers ---

// pump advances the fake clock until the operation finishes or the budget
// runs out, and returns the operation's error.
//
// It exists because the supervisor blocks on the clock between rungs of the
// ladder. A test that waits for a later rung without advancing time waits
// forever; a test that advances time blindly races the goroutine. This
// advances only when something is actually blocked, which is what the real
// clock does anyway.
func (f *stopFixture) pump(done <-chan error, budget time.Duration) error {
	deadline := time.Now().Add(budget)
	for time.Now().Before(deadline) {
		select {
		case err := <-done:
			return err
		default:
		}
		if f.clk.Pending() > 0 {
			f.clk.Fire(time.Second)
			continue
		}
		time.Sleep(time.Millisecond)
	}
	select {
	case err := <-done:
		return err
	case <-time.After(100 * time.Millisecond):
		return errStillRunning
	}
}

var errStillRunning = errors.New("операция не завершилась")

func waitForCall(t *testing.T, f *stopFixture, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("condition not met in time; KillCalls=%v CtrlCCalls=%v asked=%v",
		f.ops.KillCalls, f.ops.CtrlCCalls, f.conf.asked)
}
