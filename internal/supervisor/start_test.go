package supervisor

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"wow-hub/internal/logx"
	"wow-hub/internal/osops"
)

func TestMarkerWatcherFindsMarkerSplitAcrossWrites(t *testing.T) {
	// A pipe splits output wherever it likes. Matching whole Write calls
	// would miss the readiness marker about half the time, and the start
	// sequence would then sit until its timeout against a server that is
	// already up.
	w := newMarkerWatcher(worldReadyMarker)

	w.Write([]byte("WORLD: World Init"))
	if w.Reached() {
		t.Fatal("Reached = true on an incomplete line")
	}

	w.Write([]byte("ialized In 0 Minutes 46 Seconds\n"))
	if !w.Reached() {
		t.Fatalf("Reached = false after the line completed; buffer holds %q", w.Buffer())
	}
}

func TestMarkerWatcherIgnoresReadyBanner(t *testing.T) {
	// The line that looks like readiness and is not: it prints in the
	// banner, before the world has loaded.
	w := newMarkerWatcher(worldReadyMarker)
	w.Write([]byte("AzerothCore rev. 37fe7503ca89+ 2026-09-07 (worldserver-daemon) ready...\n"))

	if w.Reached() {
		t.Fatal("Reached matched the banner line")
	}
}

func TestMarkerWatcherIgnoresPartialMarkerAtEnd(t *testing.T) {
	// Without the newline the line is not complete, and a marker half
	// written must not count.
	w := newMarkerWatcher(worldReadyMarker)
	w.Write([]byte("WORLD: World Init"))
	if w.Reached() {
		t.Fatal("Reached = true for an unterminated partial marker")
	}
}

func TestMarkerWatcherCountsLinesNotWrites(t *testing.T) {
	w := newMarkerWatcher(worldReadyMarker)
	w.Write([]byte("a\nb\nc\n"))
	if got := w.Lines(); got != 3 {
		t.Fatalf("Lines = %d, want 3", got)
	}
}

func TestMarkerWatcherForwardsToTheLogger(t *testing.T) {
	// Everything a child prints has to reach logs\wow-hub.log, or a user
	// reporting a problem has nothing to attach.
	dir := t.TempDir()
	lg, err := logx.New(dir)
	if err != nil {
		t.Fatalf("logx.New: %v", err)
	}
	t.Cleanup(func() { lg.Close() })

	w := newMarkerWatcherWith(worldReadyMarker, newLogTap(lg, "world"))
	w.Write([]byte("something happened\n"))
	lg.Close()

	data, err := readFile(dir + string(filepathSeparator) + logx.LogFileName)
	if err != nil {
		t.Fatalf("reading the log: %v", err)
	}
	if !strings.Contains(data, "something happened") {
		t.Fatalf("log = %q, want the forwarded line", data)
	}
	if !strings.Contains(data, "world") {
		t.Fatalf("log = %q, want the service prefix", data)
	}
}

func TestLogTapNeverReturnsAnError(t *testing.T) {
	// A non-nil error here propagates into the child process's pipe and
	// can kill the very process the app is supervising. The logger being
	// closed - or a full disk - must never do that.
	lg, err := logx.New(t.TempDir())
	if err != nil {
		t.Fatalf("logx.New: %v", err)
	}
	lg.Close()

	tap := newLogTap(lg, "mysql")
	n, err := tap.Write([]byte("line after close\n"))
	if err != nil {
		t.Fatalf("Write returned %v, want nil - an error here kills the child process", err)
	}
	if n != len("line after close\n") {
		t.Fatalf("Write returned n = %d, want %d - a short write would truncate the stream", n, len("line after close\n"))
	}
}

func TestLogTapSplitsOnNewlinesAcrossWrites(t *testing.T) {
	// If the tap joined lines or dropped the tail after a split, Task 11's
	// grep for "Shutdown complete" would find nothing and report a false
	// failure on a clean shutdown.
	dir := t.TempDir()
	lg, _ := logx.New(dir)
	defer lg.Close()

	tap := newLogTap(lg, "mysql")
	tap.Write([]byte("Waiting for"))
	tap.Write([]byte(" connections\n"))
	lg.Close()

	data, _ := readFile(dir + string(filepathSeparator) + logx.LogFileName)
	if !strings.Contains(data, "Waiting for connections") {
		t.Fatalf("log = %q, want the joined line", data)
	}
}

// --- start sequence ---

func TestStartAllOrdersServices(t *testing.T) {
	f := newStartFixture(t)

	if err := f.sup.StartAll(context.Background()); err != nil {
		t.Fatalf("StartAll: %v", err)
	}

	starts := f.serviceStarts()
	want := []string{"mysqld", "authserver", "worldserver"}
	if len(starts) != len(want) {
		t.Fatalf("started %v, want exactly %v", starts, want)
	}
	for i := range want {
		if starts[i] != want[i] {
			t.Fatalf("start order = %v, want %v", starts, want)
		}
	}
}

func TestStartAllPingsBeforeAuth(t *testing.T) {
	// worldserver spends about a minute failing against a dead database and
	// then dies in a screenful of errors. Catching it here turns a minute of
	// confusion into one sentence.
	f := newStartFixture(t)
	if err := f.sup.StartAll(context.Background()); err != nil {
		t.Fatalf("StartAll: %v", err)
	}

	pingAt := -1
	authAt := -1
	for i, s := range f.startedNames() {
		if s == "ping" && pingAt < 0 {
			pingAt = i
		}
		if s == "authserver" && authAt < 0 {
			authAt = i
		}
	}
	if pingAt < 0 {
		t.Fatalf("database was never pinged; sequence was %v", f.startedNames())
	}
	if pingAt > authAt {
		t.Fatalf("ping happened at %d, after authserver at %d; sequence was %v", pingAt, authAt, f.startedNames())
	}
}

// serviceStarts returns only the launches, without the ping probe, so an
// assertion about start ORDER is not confused by it.
func (f *startFixture) serviceStarts() []string {
	var out []string
	for _, s := range f.startedNames() {
		if s == "ping" {
			continue
		}
		out = append(out, s)
	}
	return out
}

func TestStartAllLaunchesEachServiceWithItsOwnArguments(t *testing.T) {
	// The arguments are part of the contract with the repack, not decoration.
	// The config paths are relative because each process is started with the
	// repack root as its working directory - an absolute path would work here
	// and hide a wrong working directory.
	f := newStartFixture(t)
	if err := f.sup.StartAll(context.Background()); err != nil {
		t.Fatalf("StartAll: %v", err)
	}

	args := map[string]string{}
	for _, call := range f.ops.StartCalls {
		args[exeName(call[0])] = strings.Join(call[1:], " ")
	}

	if got, want := args["authserver"], "-c configs/authserver.conf"; got != want {
		t.Errorf("authserver args = %q, want %q", got, want)
	}
	if got, want := args["worldserver"], "-c configs/worldserver.conf"; got != want {
		t.Errorf("worldserver args = %q, want %q", got, want)
	}
	// The flags themselves are checked against MySQL.bat by the next test.
	// This only proves argsFor routes MySQL to them and not to nil - which is
	// what it did before, silently running the database on compiled-in
	// defaults.
	if got, want := args["mysqld"], strings.Join(mysqlArgs, " "); got != want {
		t.Errorf("mysqld args = %q, want %q", got, want)
	}
}

func TestMySQLArgsStayInSyncWithTheRepackBat(t *testing.T) {
	// mysqlArgs carries the flags MySQL.bat starts the database with, and its
	// comment says to keep them in sync. Nothing enforced that, and the drift
	// had already happened once: the app launched mysqld with no arguments at
	// all, so the database ran with a 128M buffer pool and REPEATABLE-READ
	// instead of the repack's 512M and READ-COMMITTED.
	//
	// MySQL.bat sits above this module and is absent when the app is checked
	// out on its own, so its absence skips rather than fails. Three levels
	// up from this package: internal/supervisor -> internal -> wow-hub ->
	// the repack root.
	bat, err := os.ReadFile(filepath.Join("..", "..", "..", "MySQL.bat"))
	if err != nil {
		t.Skipf("repack MySQL.bat not available: %v", err)
	}
	// cmd.exe quotes the isolation level; the quotes are shell escaping, not
	// part of the value.
	text := strings.ReplaceAll(string(bat), `"`, "")

	inBat := map[string]bool{}
	for _, tok := range strings.Fields(text) {
		if strings.HasPrefix(tok, "--") {
			inBat[tok] = true
		}
	}
	inArgs := map[string]bool{}
	for _, arg := range mysqlArgs {
		inArgs[arg] = true
	}

	for arg := range inBat {
		if !inArgs[arg] {
			t.Errorf("MySQL.bat passes %q but mysqlArgs does not", arg)
		}
	}
	for arg := range inArgs {
		if !inBat[arg] {
			t.Errorf("mysqlArgs passes %q but MySQL.bat does not", arg)
		}
	}
}

func TestStartAbortsWhenMySQLPortNeverOpens(t *testing.T) {
	f := newStartFixture(t)
	// Nothing ever listens on 3306.
	f.ops.Listening[portMySQL] = false

	done := make(chan error, 1)
	go func() { done <- f.sup.StartAll(context.Background()) }()
	f.pumpStart(done, 3*time.Second)

	for _, s := range f.startedNames() {
		if s == "authserver" || s == "worldserver" {
			t.Fatalf("started %v although MySQL never came up", f.startedNames())
		}
	}
}

func TestStartSurfacesPortConflict(t *testing.T) {
	// Port 3306 accepting connections while the database refuses them means
	// something else owns the port - a second mysqld, or an unrelated
	// service. "ping failed" is accurate and useless; the user is looking at
	// a working TCP port and has no reason to suspect the other end.
	f := newStartFixture(t)
	f.ops.Listening[portMySQL] = true
	f.pingErr = errTest("Access denied for user 'acore'@'localhost'")

	done := make(chan error, 1)
	go func() { done <- f.sup.StartAll(context.Background()) }()
	err := f.pumpStart(done, 3*time.Second)

	if err == nil {
		t.Fatal("StartAll returned nil although the database refused the connection")
	}
	if !strings.Contains(err.Error(), "3306") {
		t.Errorf("error = %q, want it to name port 3306", err)
	}
}

func TestStartRollsBackWhenWorldNeverBecomesReady(t *testing.T) {
	// Rollback stops only what this run started. Rolling back a service that
	// was already up before the button was pressed is a surprise nobody
	// asked for.
	f := newStartFixture(t)
	// auth opens its port, world never opens 8085.
	f.ops.Listening[portWorld] = false
	f.ops.OnStart = func(exe string, args []string, dir string, out io.Writer) (osops.Proc, error) {
		f.started = append(f.started, exeName(exe))
		if strings.Contains(exe, "worldserver") {
			return osops.NewFakeProc(900), nil
		}
		return osops.NewFakeProc(800), nil
	}

	done := make(chan error, 1)
	go func() { done <- f.sup.StartAll(context.Background()) }()
	f.pumpStart(done, 5*time.Second)

	if len(f.ops.RunCalls) == 0 {
		t.Fatal("rollback never ran: MySQL was not shut down after a failed world start")
	}
	joined := strings.Join(f.ops.RunCalls[0], " ")
	if !strings.Contains(joined, "mysqladmin") {
		t.Fatalf("rollback used %v, want mysqladmin", f.ops.RunCalls[0])
	}
	if len(f.ops.CtrlCCalls) == 0 {
		t.Fatal("rollback never tried to stop authserver")
	}
}

func TestStartServiceSkipsAlreadyRunning(t *testing.T) {
	// Two worldserver processes writing the same characters corrupt the
	// characters database, and the damage is not reliably visible later.
	f := newStartFixture(t)
	f.ops.Procs["authserver.exe"] = []int{4242}
	f.ops.PortPIDs[portAuth] = 4242
	f.sup.Refresh()

	if err := f.sup.StartService(context.Background(), SvcAuth); err != nil {
		t.Fatalf("StartService on an already-running service: %v", err)
	}
	for _, s := range f.started {
		if strings.Contains(s, "authserver") {
			t.Fatalf("started %v although authserver was already running", f.started)
		}
	}
}

func TestStartAllAdoptsARunningWorldInsteadOfStartingASecond(t *testing.T) {
	// The world is the one that must never be duplicated: two of them writing
	// the same characters corrupt the database, and the damage is not reliably
	// visible afterwards.
	//
	// So this asserts both halves. No second worldserver is launched - and no
	// refusal either, because the user chose adoption: a world already up is
	// something to keep, not a reason to do nothing at all. MySQL and auth are
	// still brought up around it.
	f := newStartFixture(t)
	f.ops.Procs["worldserver.exe"] = []int{5151}
	f.ops.PortPIDs[portWorld] = 5151
	f.ops.Listening[portWorld] = true
	f.sup.Refresh()

	done := make(chan error, 1)
	go func() { done <- f.sup.StartAll(context.Background()) }()
	if err := f.pumpStart(done, 5*time.Second); err != nil {
		t.Fatalf("StartAll with the world already up: %v", err)
	}

	for _, s := range f.startedNames() {
		if strings.Contains(s, "worldserver") {
			t.Fatalf("started a second worldserver: %v", f.startedNames())
		}
	}
	// The other two still come up: adoption is per service, not a skip.
	for _, want := range []string{"mysqld", "authserver"} {
		found := false
		for _, s := range f.startedNames() {
			if strings.Contains(s, want) {
				found = true
			}
		}
		if !found {
			t.Errorf("%s was not started although only the world was up: %v", want, f.startedNames())
		}
	}
}

func TestStartAllDoesNotStopAnAdoptedServiceOnRollback(t *testing.T) {
	// MySQL is the common case: the user starts the database by hand, or the
	// panel was closed and reopened. A later failure must not shut down a
	// database this run never started - the panel would be taking down a
	// working server on the user's behalf.
	f := newStartFixture(t)
	f.ops.Procs["mysqld.exe"] = []int{3310}
	f.ops.PortPIDs[portMySQL] = 3310
	f.sup.Refresh()

	// The world then fails, so rollback runs. The fixture's own OnStart opens
	// 8085 on launch, which would let the start succeed; this keeps it shut.
	f.ops.OnStart = func(exe string, args []string, dir string, out io.Writer) (osops.Proc, error) {
		f.mu.Lock()
		f.started = append(f.started, exeName(exe))
		f.mu.Unlock()
		return osops.NewFakeProc(900), nil
	}

	done := make(chan error, 1)
	go func() { done <- f.sup.StartAll(context.Background()) }()
	err := f.pumpStart(done, 5*time.Second)
	if err == nil {
		t.Fatal("StartAll returned nil although the world never opened its port")
	}

	for _, call := range f.ops.RunCalls {
		if strings.Contains(strings.Join(call, " "), "mysqladmin") {
			t.Fatalf("rollback shut down the adopted MySQL: %v", f.ops.RunCalls)
		}
	}
	for _, pid := range f.ops.KillCalls {
		if pid == 3310 {
			t.Fatalf("rollback killed the adopted MySQL (PID %d); "+
				"it must be left alone", pid)
		}
	}
}

func TestASecondOperationIsRefusedWhileOneIsRunning(t *testing.T) {
	// A queued start would be worse than a rejected one. The user clicks
	// twice, the second run waits out the first, and then starts a second
	// worldserver against the same characters.
	f := newStartFixture(t)
	// The world never opens its port, so the first StartAll is still working.
	// The fixture's own OnStart would open 8085 on launch and let it finish
	// before the second call arrives.
	f.ops.OnStart = func(exe string, args []string, dir string, out io.Writer) (osops.Proc, error) {
		f.mu.Lock()
		f.started = append(f.started, exeName(exe))
		f.mu.Unlock()
		return osops.NewFakeProc(900), nil
	}

	done := make(chan error, 1)
	go func() { done <- f.sup.StartAll(context.Background()) }()

	// Probe with StopService on a service that is not running: it takes the
	// lock and then returns immediately, so it can be retried without changing
	// anything. Probing with StartService instead was the obvious choice and it
	// is wrong - the probe grabs the lock itself, launches authserver on every
	// attempt, and starves StartAll of the turn it is supposed to collide with.
	//
	// The clock is not advanced either. StartAll takes the lock before it does
	// anything else, so a few milliseconds is enough; firing the clock would
	// run out the world's three-minute budget and let the first operation
	// finish, leaving nothing to collide with.
	time.Sleep(20 * time.Millisecond)
	deadline := time.Now().Add(2 * time.Second)
	busy := false
	for time.Now().Before(deadline) {
		if errors.Is(f.sup.StopService(context.Background(), SvcAuth), ErrBusy) {
			busy = true
			break
		}
		time.Sleep(time.Millisecond)
	}
	if !busy {
		t.Fatal("the first StartAll never took the operation lock")
	}

	if err := f.sup.StartService(context.Background(), SvcAuth); !errors.Is(err, ErrBusy) {
		t.Errorf("a second operation returned %v, want ErrBusy", err)
	}
	if err := f.sup.StopAll(context.Background()); !errors.Is(err, ErrBusy) {
		t.Errorf("StopAll during a start returned %v, want ErrBusy", err)
	}
	if err := f.sup.StopService(context.Background(), SvcAuth); !errors.Is(err, ErrBusy) {
		t.Errorf("StopService during a start returned %v, want ErrBusy", err)
	}

	// Let the first operation finish so its goroutine does not outlive the
	// test and trip the race detector.
	f.ops.Listening[portWorld] = true
	f.pumpStart(done, 5*time.Second)
}

func TestStartServiceAsksTheOSRatherThanTrustingTheCachedTable(t *testing.T) {
	// Refresh runs once a second, so a process can be up while the table still
	// says stopped - a server the user started by hand, or one that appeared
	// between two ticks. Deciding on the cache is deciding on a coincidence,
	// and the coincidence is a second mysqld on a port it cannot bind.
	f := newStartFixture(t)
	f.ops.Procs["authserver.exe"] = []int{4242}
	f.ops.PortPIDs[portAuth] = 4242
	// Deliberately no Refresh: this is the stale-table case.

	if err := f.sup.StartService(context.Background(), SvcAuth); err != nil {
		t.Fatalf("StartService: %v", err)
	}
	for _, s := range f.startedNames() {
		if strings.Contains(s, "authserver") {
			t.Fatalf("started %v although authserver was already running and the "+
				"table had not caught up", f.startedNames())
		}
	}
}

func TestStartWorldTimeoutMessageNamesThePort(t *testing.T) {
	// The message has to say what was being waited for, or the user is left
	// guessing whether the server is loading, stuck, or already dead.
	f := newStartFixture(t)
	f.ops.OnStart = func(exe string, args []string, dir string, out io.Writer) (osops.Proc, error) {
		f.started = append(f.started, exeName(exe))
		return osops.NewFakeProc(900), nil
	}

	done := make(chan error, 1)
	go func() { done <- f.sup.StartAll(context.Background()) }()
	err := f.pumpStart(done, 5*time.Second)

	if err == nil {
		t.Fatal("StartAll returned nil although the world port never opened")
	}
	if !strings.Contains(err.Error(), "8085") {
		t.Errorf("error = %q, want it to name port 8085", err)
	}
}

func TestWorldGetsTheLongTimeout(t *testing.T) {
	// World init measured at 46 s warm and over two minutes cold. Giving it
	// authserver's 30-second budget means a healthy world is declared dead
	// while it is still loading, and the rollback then pulls the database out
	// from under it - which is what the integration run actually did.
	f := newStartFixture(t)
	f.ops.Listening[portWorld] = false
	f.ops.OnStart = func(exe string, args []string, dir string, out io.Writer) (osops.Proc, error) {
		f.mu.Lock()
		f.started = append(f.started, exeName(exe))
		f.mu.Unlock()
		if out != nil {
			out.Write([]byte("WORLD: World Initialized In 2 Minutes 12 Seconds\r\n"))
		}
		return osops.NewFakeProc(900), nil
	}

	done := make(chan error, 1)
	go func() { done <- f.sup.StartAll(context.Background()) }()

	// Wait until the world is actually launched before pushing time forward.
	// Firing a clock nobody is waiting on does nothing but advance Now, so
	// this would otherwise race the goroutine and assert about a world that
	// had not started yet.
	waitUntilCond(t, 2*time.Second, func() bool {
		return contains(f.startedNames(), "worldserver")
	})

	// Push well past authserver's 30-second budget.
	for i := 0; i < 40; i++ {
		f.clk.Fire(time.Second)
	}

	select {
	case err := <-done:
		t.Fatalf("StartAll gave up on the world after ~40s (%v); its budget must be %s", err, f.sup.conf.Start.WorldMarker)
	default:
	}

	if got := find(f.sup.Status(), SvcWorld).State; got != StateStarting {
		t.Fatalf("world state = %q, want %q - it is still loading, not dead", got, StateStarting)
	}
	// and it must not have been rolled back while it is still legitimately
	// loading. Marking a healthy world dead at 30s is the bug this catches.
	for _, s := range []string{"mysqld", "ping", "authserver"} {
		if !contains(f.startedNames(), s) {
			t.Fatalf("sequence = %v, want it to reach the world", f.startedNames())
		}
	}
	f.pumpStart(done, 3*time.Second)
}

func TestStartRollsBackAServiceThatFailedToBecomeReady(t *testing.T) {
	// A process that was spawned but never became ready is still a process.
	// Rolling back only what finished starting leaves it running - the
	// half-started stack this design exists to prevent. The first
	// integration run left a worldserver alive and labelled "starting".
	f := newStartFixture(t)
	// The world must still be running when the rollback reaches it: a fake
	// whose stdin handler kills it would look like a successful rollback
	// while proving nothing about the orphan bug this test exists for.
	f.ops.Listening[portWorld] = false
	f.ops.OnStart = func(exe string, args []string, dir string, out io.Writer) (osops.Proc, error) {
		f.mu.Lock()
		f.started = append(f.started, exeName(exe))
		f.mu.Unlock()
		if out != nil {
			out.Write([]byte("WORLD: World Initialized In 0 Minutes 46 Seconds\r\n"))
		}
		if strings.Contains(strings.ToLower(exe), "worldserver") {
			return osops.NewFakeProc(3000), nil
		}
		return osops.NewFakeProc(1000), nil
	}

	done := make(chan error, 1)
	go func() { done <- f.sup.StartAll(context.Background()) }()
	f.pumpStart(done, 5*time.Second)

	// Both MySQL and the world must have been dealt with: auth succeeded,
	// world was spawned then rejected.
	if len(f.ops.RunCalls) == 0 {
		t.Error("MySQL was not shut down during the rollback")
	}
	if len(f.ops.CtrlCCalls) == 0 {
		t.Fatalf("the world process was never stopped; CtrlCCalls=%v KillCalls=%v. "+
			"A process that was launched but never became ready is still a process",
			f.ops.CtrlCCalls, f.ops.KillCalls)
	}
	// The invariant is that the world is GONE, not which rung stopped it.
	// The first integration run left one alive and labelled "starting"; that
	// is what this test is about.
	if f.ops.Alive(3000) {
		t.Fatalf("the world (PID 3000) is still running after the rollback; "+
			"CtrlCCalls=%v KillCalls=%v RunCalls=%v",
			f.ops.CtrlCCalls, f.ops.KillCalls, f.ops.RunCalls)
	}
}

func TestStartMarksWorldRunningOnlyAfterThePortOpens(t *testing.T) {
	// "WORLD: World Initialized In" is not readiness: about 140 lines of
	// module init follow it. The world socket is the only signal that held
	// up.
	f := newStartFixture(t)
	if err := f.sup.StartAll(context.Background()); err != nil {
		t.Fatalf("StartAll: %v", err)
	}
	if got := find(f.sup.Status(), SvcWorld).State; got != StateRunning {
		t.Fatalf("world state = %q, want %q", got, StateRunning)
	}
}

func TestStartRollsBackOnContextCancellation(t *testing.T) {
	// The user closes the app mid-start. Leaving a half-started stack behind
	// is how the next session finds a MySQL nobody is watching.
	f := newStartFixture(t)
	f.ops.Listening[portWorld] = false
	f.ops.OnStart = func(exe string, args []string, dir string, out io.Writer) (osops.Proc, error) {
		f.started = append(f.started, exeName(exe))
		return osops.NewFakeProc(900), nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- f.sup.StartAll(ctx) }()

	// Cancel once world is up but before it is ready.
	waitUntilCond(t, 2*time.Second, func() bool {
		for _, s := range f.started {
			if strings.Contains(s, "worldserver") {
				return true
			}
		}
		return false
	})
	cancel()
	f.pumpStart(done, 5*time.Second)

	if len(f.ops.RunCalls) == 0 {
		t.Fatal("cancelling mid-start left MySQL running")
	}
}
