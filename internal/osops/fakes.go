package osops

import (
	"bytes"
	"context"
	"io"
	"sync"
	"time"
)

// FakeOps is a scriptable stand-in for the operating system.
//
// It lives in a non-test file on purpose: the stop ladder, the start
// sequence and the health checks are all in other packages, and Go does
// not export anything from a _test file to another package. Its compiled
// weight is nil - the linker drops it from the shipped binary.
type FakeOps struct {
	// State the fake answers from.
	Procs     map[string][]int // image name -> PIDs FindByName reports
	Listening map[int]bool     // port -> Listen reports true
	Memory    map[int]int      // PID -> megabytes
	Dead      map[int]bool     // PIDs that no longer exist
	PortPIDs  map[int]int      // port -> the PID that owns it

	// Children maps a PID to the PIDs it spawned, so a test can describe a
	// process tree. It exists because the real one matters: MySQL on this
	// repack is a mysqld.exe parent whose buffer pool lives in a mysqld.exe
	// child, and a fake that could only model single processes could not
	// tell the two apart.
	Children map[int][]int

	// Recorded calls, in order.
	KillCalls  []int
	RunCalls   [][]string
	RunToCalls []RunToCall
	// StartCalls records exe followed by its arguments for every Start, so a
	// test can assert how a service was launched - the mysqld flags, above
	// all, which have to match MySQL.bat.
	StartCalls [][]string
	// Lines records SendLine calls, so a test can assert the shutdown
	// command reached the console.
	Lines      []string
	CtrlCCalls []int

	// Overrides. A nil field means "behave normally". Non-nil wins over
	// the state above, so a test can make one call fail without having to
	// describe a whole world.
	OnFindByName func(name string) ([]int, error)
	OnStart      func(exe string, args []string, dir string, out io.Writer) (Proc, error)
	OnSendLine   func(pid int, text string) error
	OnSendCtrlC  func(pid int) error
	OnKill       func(pid int) error
	OnRun        func(exe string, args ...string) error
	OnDiskFree   func(dir string) (int64, int64, error)
	// OnRunTo replaces the default body. It receives the same writer the
	// real implementation would hand the child's stdout, so a test can
	// decide what a successful dump leaves on disk.
	//
	// ctx is passed through so a test can model the real CommandContext
	// behaviour - the child dying when the context is cancelled - without
	// having to spawn anything.
	OnRunTo func(ctx context.Context, exe string, args []string, dir string, out io.Writer) (string, error)

	// RunToBody is what a RunTo with no override writes to out. A test
	// that wants the caller's completeness check to pass sets it to a
	// dump trailer; leaving it empty is how a truncated dump is modelled.
	RunToBody string

	// OpenCalls records every path OpenPath was given, so a test can assert
	// which folder a button reveals without launching Explorer.
	OpenCalls []string
	// OnOpenPath replaces the default, which records and succeeds.
	OnOpenPath func(path string) error

	// DiskFreeBytes and DiskTotalBytes are what DiskFree reports when no
	// override is set. Zero means "no override", so a test that cares about
	// the figure sets it and every other test sees an ordinary disk.
	DiskFreeBytes  int64
	DiskTotalBytes int64

	mu sync.Mutex
}

// NewFake returns a FakeOps with empty state and no overrides.
func NewFake() *FakeOps {
	return &FakeOps{
		Procs:     map[string][]int{},
		Listening: map[int]bool{},
		Memory:    map[int]int{},
		Dead:      map[int]bool{},
		PortPIDs:  map[int]int{},
		Children:  map[int][]int{},
	}
}

var _ Ops = (*FakeOps)(nil)

// FindByName reports the live processes with this image name.
//
// Dead PIDs are filtered out, because that is what the operating system
// does: tasklist does not list a process that has exited. Without the filter
// a test that stopped a process would still be told it is running, and the
// dependency guard - which asks exactly this question - could never be
// exercised.
func (f *FakeOps) FindByName(name string) ([]int, error) {
	if f.OnFindByName != nil {
		return f.OnFindByName(name)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	pids, ok := f.Procs[name]
	if !ok || len(pids) == 0 {
		return []int{}, nil
	}
	out := make([]int, 0, len(pids))
	for _, pid := range pids {
		if !f.Dead[pid] {
			out = append(out, pid)
		}
	}
	return out, nil
}

func (f *FakeOps) Start(exe string, args []string, dir string, out io.Writer) (Proc, error) {
	call := make([]string, 0, len(args)+1)
	call = append(call, exe)
	call = append(call, args...)
	f.mu.Lock()
	f.StartCalls = append(f.StartCalls, call)
	f.mu.Unlock()

	if f.OnStart != nil {
		return f.OnStart(exe, args, dir, out)
	}
	return NewFakeProc(4242), nil
}

// MarkDead records that a process has exited.
//
// Tests use it to model a server obeying its console command, or mysqladmin
// shutting mysqld down - the two ways a process actually leaves in practice.
func (f *FakeOps) MarkDead(pid int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Dead[pid] = true
}

// SendLineDefaultSuccess makes a console-typed command stop the process, the
// way a server that reads its console would. Tests that want it to fail say
// so with OnSendLine.
func (f *FakeOps) SendLine(pid int, text string) error {
	f.mu.Lock()
	f.Lines = append(f.Lines, text)
	f.mu.Unlock()
	if f.OnSendLine != nil {
		return f.OnSendLine(pid, text)
	}
	f.Dead[pid] = true
	return nil
}

func (f *FakeOps) SendCtrlC(pid int) error {
	f.mu.Lock()
	f.CtrlCCalls = append(f.CtrlCCalls, pid)
	f.mu.Unlock()
	if f.OnSendCtrlC != nil {
		return f.OnSendCtrlC(pid)
	}
	f.Dead[pid] = true
	return nil
}

func (f *FakeOps) Kill(pid int) error {
	f.mu.Lock()
	f.KillCalls = append(f.KillCalls, pid)
	f.mu.Unlock()
	if f.OnKill != nil {
		return f.OnKill(pid)
	}
	f.Dead[pid] = true
	return nil
}

func (f *FakeOps) HideConsole(pid int) {}

func (f *FakeOps) MemoryMB(pid int) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.Memory[pid]
}

// TreeMemoryMB sums pid and its descendants, using Children as the parent
// links the OS would report.
//
// Dead processes are skipped, as the real thing skips them: a PID that has
// exited has no working set to read. A stale parent link to a dead child
// would otherwise inflate the figure forever.
//
// Memory holds whole megabytes, so this fake cannot reproduce the real
// one's byte-level rounding - it sums what it was given. That behaviour
// belongs to WinOps and is pinned there, against a real cmd.exe and a real
// ping child, by TestWinOpsTreeMemoryMBAddsARealChild.
func (f *FakeOps) TreeMemoryMB(pid int) int {
	f.mu.Lock()
	defer f.mu.Unlock()

	total := 0
	seen := map[int]bool{pid: true}
	queue := []int{pid}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		if f.Dead[cur] {
			continue
		}
		total += f.Memory[cur]
		for _, kid := range f.Children[cur] {
			if seen[kid] {
				continue
			}
			seen[kid] = true
			queue = append(queue, kid)
		}
	}
	return total
}

func (f *FakeOps) Run(exe string, args ...string) error {
	f.mu.Lock()
	call := make([]string, 0, len(args)+1)
	call = append(call, exe)
	call = append(call, args...)
	f.RunCalls = append(f.RunCalls, call)
	f.mu.Unlock()
	if f.OnRun != nil {
		return f.OnRun(exe, args...)
	}
	return nil
}

// RunToCall is one recorded RunTo invocation.
//
// Dir is kept separate from the argument list because it is the one
// parameter the command line cannot show: snapshot-mysql-data.cmd is
// invoked by bare name precisely so the parenthesised repack path stays
// out of the command line, and only the working directory says which
// repack it ran against.
type RunToCall struct {
	Exe  string
	Args []string
	Dir  string
}

func (f *FakeOps) RunTo(ctx context.Context, exe string, args []string, dir string, out io.Writer) (string, error) {
	f.mu.Lock()
	f.RunToCalls = append(f.RunToCalls, RunToCall{Exe: exe, Args: args, Dir: dir})
	body := f.RunToBody
	f.mu.Unlock()

	if f.OnRunTo != nil {
		return f.OnRunTo(ctx, exe, args, dir, out)
	}
	// A cancelled context kills the child, and the default body models
	// that rather than writing a whole dump anyway - otherwise a test for
	// cancellation would see a complete file and pass by accident.
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if body != "" && out != nil {
		if _, err := io.WriteString(out, body); err != nil {
			return "", err
		}
	}
	return "", nil
}

func (f *FakeOps) OpenPath(path string) error {
	f.mu.Lock()
	f.OpenCalls = append(f.OpenCalls, path)
	f.mu.Unlock()

	if f.OnOpenPath != nil {
		return f.OnOpenPath(path)
	}
	return nil
}

func (f *FakeOps) Listen(port int) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.Listening[port]
}

func (f *FakeOps) Alive(pid int) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return !f.Dead[pid]
}

func (f *FakeOps) PortOwner(port int) (int, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	pid, ok := f.PortPIDs[port]
	return pid, ok
}

func (f *FakeOps) DiskFree(dir string) (int64, int64, error) {
	if f.OnDiskFree != nil {
		return f.OnDiskFree(dir)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.DiskFreeBytes, f.DiskTotalBytes, nil
}

// FakeProc is a process the app started, in name only.
type FakeProc struct {
	// PIDVal is what PID reports. Tests that care about which process was
	// signalled set it.
	PIDVal int

	// WaitCh is what Wait blocks on. Push an error to simulate the process
	// exiting; push nil for a clean exit. Closed channel also unblocks,
	// with the value it was closed after.
	WaitCh chan error

	// stdin records whatever was written to the child, so a test can assert
	// on the exact bytes of a console command.
	stdin *fakeStdin

	// OnWrite fires after every successful stdin write.
	//
	// Tests use it to model a server that obeys the console command, which
	// is what these binaries actually do: measured against the real
	// worldserver, "server shutdown 1" on stdin runs the save countdown
	// and closes every database pool. Without this hook a fake would ignore
	// the one graceful path that works, and every stop test would have to
	// escalate all the way to a kill.
	OnWrite func()
}

type fakeStdin struct {
	mu      sync.Mutex
	buf     bytes.Buffer
	closed  bool
	onWrite func()
}

func (s *fakeStdin) Write(p []byte) (int, error) {
	s.mu.Lock()
	n, err := s.buf.Write(p)
	hook := s.onWrite
	s.mu.Unlock()
	if err == nil && hook != nil {
		hook()
	}
	return n, err
}

func (s *fakeStdin) Written() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.String()
}

// Close is a no-op that records the attempt. Closing real stdin is what
// makes these servers shut themselves down, and a test must be able to
// assert that nothing ever does it.
func (s *fakeStdin) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	return nil
}

func (s *fakeStdin) Closed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}

// NewFakeProc returns a FakeProc with the given PID and an open WaitCh.
func NewFakeProc(pid int) *FakeProc {
	return &FakeProc{PIDVal: pid, WaitCh: make(chan error, 1), stdin: &fakeStdin{}}
}

var _ Proc = (*FakeProc)(nil)

func (p *FakeProc) PID() int { return p.PIDVal }

func (p *FakeProc) Wait() error { return <-p.WaitCh }

func (p *FakeProc) Stdin() io.WriteCloser {
	p.stdin.mu.Lock()
	p.stdin.onWrite = p.OnWrite
	p.stdin.mu.Unlock()
	return p.stdin
}

// StdinWritten returns everything written to the fake child's stdin.
func (p *FakeProc) StdinWritten() string { return p.stdin.Written() }

// FakeClock is a Clock that only moves when a test tells it to.
//
// The stop ladder waits 30 seconds for a graceful exit and 60 more for a
// person to answer a dialog. Against the real clock that is a test suite
// nobody runs. Against this, it costs nothing.
type FakeClock struct {
	mu      sync.Mutex
	now     time.Time
	waiters []chan time.Time
}

var _ Clock = (*FakeClock)(nil)

// NewFakeClock returns a FakeClock parked at a fixed instant, so tests
// never depend on the wall clock.
func NewFakeClock() *FakeClock {
	return &FakeClock{
		now: time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC),
	}
}

func (c *FakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *FakeClock) After(d time.Duration) <-chan time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	ch := make(chan time.Time, 1)
	c.waiters = append(c.waiters, ch)
	return ch
}

// Fire releases every waiter registered since the last Fire, and advances
// the clock by d. This is how a test says "30 seconds passed".
func (c *FakeClock) Fire(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	pending := c.waiters
	c.waiters = nil
	c.mu.Unlock()

	for _, ch := range pending {
		select {
		case ch <- time.Now():
		default:
		}
	}
}

// Pending reports how many callers are currently blocked in After. A
// non-zero value after an operation finished means something waited for
// time that never came, which is a hang rather than a slow test.
func (c *FakeClock) Pending() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.waiters)
}
