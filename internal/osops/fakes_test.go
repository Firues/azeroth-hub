package osops

import (
	"io"
	"testing"
	"time"
)

func TestFakeOpsRecordsCallsInOrder(t *testing.T) {
	f := NewFake()

	f.Kill(111)
	f.Kill(222)

	if len(f.KillCalls) != 2 || f.KillCalls[0] != 111 || f.KillCalls[1] != 222 {
		t.Fatalf("KillCalls = %v, want [111 222]", f.KillCalls)
	}
}

func TestFakeOpsFindByNameHidesDeadProcesses(t *testing.T) {
	// The operating system does not list a process that has exited, and the
	// dependency guard asks exactly this question: "is a worldserver running
	// right now?". A fake that kept reporting a killed process would make
	// that guard untestable - and would let a test pass while the real
	// system would have allowed a stop.
	f := NewFake()
	f.Procs["worldserver.exe"] = []int{3000, 3001}

	f.Dead[3000] = true

	pids, err := f.FindByName("worldserver.exe")
	if err != nil {
		t.Fatalf("FindByName: %v", err)
	}
	if len(pids) != 1 || pids[0] != 3001 {
		t.Fatalf("FindByName = %v, want [3001] with the dead PID filtered out", pids)
	}
}

func TestFakeOpsTreeMemoryMBAddsDescendants(t *testing.T) {
	// The shape of MySQL on this repack, with the real figures: a parent
	// that holds almost nothing and a child that holds the buffer pool.
	f := NewFake()
	f.Memory[79700] = 29
	f.Memory[48476] = 460
	f.Children[79700] = []int{48476}

	if got := f.TreeMemoryMB(79700); got != 489 {
		t.Fatalf("TreeMemoryMB(parent) = %d, want 489", got)
	}
	if got := f.TreeMemoryMB(48476); got != 460 {
		t.Fatalf("TreeMemoryMB(child) = %d, want 460: a leaf has no tree to add", got)
	}
}

func TestFakeOpsTreeMemoryMBSkipsTheDead(t *testing.T) {
	// An exited process has no working set to read, and the real one is
	// skipped for the same reason: a stale parent link would otherwise
	// inflate the figure forever.
	f := NewFake()
	f.Memory[100] = 30
	f.Memory[200] = 400
	f.Children[100] = []int{200}
	f.Dead[200] = true

	if got := f.TreeMemoryMB(100); got != 30 {
		t.Fatalf("TreeMemoryMB = %d, want 30 with the dead child left out", got)
	}
}

func TestFakeOpsTreeMemoryMBSurvivesAStaleParentLink(t *testing.T) {
	// Windows reuses PIDs, so a parent link left behind by an exited process
	// can point back into the tree. Without a visited set that walk never
	// returns, and the status tick that calls it would hang the whole app -
	// including the stop path.
	f := NewFake()
	f.Memory[1] = 10
	f.Memory[2] = 20
	f.Children[1] = []int{2}
	f.Children[2] = []int{1}

	if got := f.TreeMemoryMB(1); got != 30 {
		t.Fatalf("TreeMemoryMB = %d, want 30", got)
	}
}

func TestFakeOpsFindByNameCopiesState(t *testing.T) {
	f := NewFake()
	f.Procs["worldserver.exe"] = []int{4242}

	pids, err := f.FindByName("worldserver.exe")
	if err != nil {
		t.Fatalf("FindByName: %v", err)
	}
	if len(pids) != 1 || pids[0] != 4242 {
		t.Fatalf("FindByName = %v, want [4242]", pids)
	}

	// A caller that mutates the returned slice must not corrupt the fake's
	// state - tests that do that would make later assertions lie.
	pids[0] = 1
	again, _ := f.FindByName("worldserver.exe")
	if again[0] != 4242 {
		t.Fatalf("FindByName after mutating the result = %v, want [4242]", again)
	}
}

func TestFakeOpsFindByNameMissingIsEmptyNotNil(t *testing.T) {
	f := NewFake()
	pids, err := f.FindByName("authserver.exe")
	if err != nil {
		t.Fatalf("FindByName: %v, want nil error", err)
	}
	if pids == nil {
		t.Fatal("got a nil slice, want an empty non-nil one")
	}
}

func TestFakeOpsKillMarksDeadByDefault(t *testing.T) {
	f := NewFake()
	f.Kill(900)

	if f.Alive(900) {
		t.Fatal("Alive(900) = true after Kill, want false")
	}
	if !f.Alive(901) {
		t.Fatal("Alive(901) = false for a PID never touched, want true")
	}
}

func TestFakeOpsOverrideWinsOverDefault(t *testing.T) {
	f := NewFake()
	called := false
	f.OnKill = func(pid int) error {
		called = true
		return io.ErrClosedPipe
	}

	err := f.Kill(5)
	if !called {
		t.Fatal("OnKill was not consulted")
	}
	if err != io.ErrClosedPipe {
		t.Fatalf("Kill = %v, want the override's error", err)
	}
	// The call must still be recorded even when the override returns an
	// error: a test asserting on KillCalls needs to see the attempt.
	if len(f.KillCalls) != 1 {
		t.Fatalf("KillCalls = %v, want one recorded attempt", f.KillCalls)
	}
}

func TestFakeProcWaitReturnsPushedError(t *testing.T) {
	p := NewFakeProc(1234)

	if p.PID() != 1234 {
		t.Fatalf("PID = %d, want 1234", p.PID())
	}

	sentinel := errForTest("worldserver exited with code 1")

	got := make(chan error, 1)
	go func() { got <- p.Wait() }()

	// Wait must block until the channel is pushed to, or the fake would
	// silently pass a test that means to prove the exit is awaited.
	select {
	case v := <-got:
		t.Fatalf("Wait returned %v before anything was pushed", v)
	case <-time.After(50 * time.Millisecond):
	}

	p.WaitCh <- sentinel
	select {
	case v := <-got:
		if v != sentinel {
			t.Fatalf("Wait = %v, want the pushed error", v)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Wait did not return after the push")
	}
}

func TestFakeClockAfterDoesNotFireUntilFire(t *testing.T) {
	c := NewFakeClock()
	start := c.Now()

	ch := c.After(30 * time.Second)

	select {
	case <-ch:
		t.Fatal("After fired on its own; the clock must only move when a test says so")
	case <-time.After(50 * time.Millisecond):
	}

	c.Fire(30 * time.Second)

	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		t.Fatal("After did not fire after Fire()")
	}

	if got := c.Now(); !got.After(start) {
		t.Fatalf("Now = %v, want it advanced past %v", got, start)
	}
	if c.Pending() != 0 {
		t.Fatalf("Pending = %d, want 0 after Fire", c.Pending())
	}
}

func TestFakeClockNowIsFixedAtConstruction(t *testing.T) {
	// Two fakes made in the same test must agree, so a test never depends
	// on the wall clock or on ordering between them.
	a, b := NewFakeClock(), NewFakeClock()
	if !a.Now().Equal(b.Now()) {
		t.Fatalf("Now differs: %v vs %v", a.Now(), b.Now())
	}
}

func TestFakeClockFireOnlyReleasesCurrentWaiters(t *testing.T) {
	c := NewFakeClock()

	first := c.After(time.Second)
	c.Fire(time.Second)
	<-first

	// A waiter registered after the Fire must stay blocked, otherwise a
	// test could not tell which wait a given Fire satisfied.
	second := c.After(time.Second)
	select {
	case <-second:
		t.Fatal("a waiter registered after Fire was released by it")
	case <-time.After(50 * time.Millisecond):
	}
}

type errForTest string

func (e errForTest) Error() string { return string(e) }
