package osops

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// syncBuffer is a concurrency-safe sink for process output. The pipe
// reader goroutine in Start writes to it while the test reads, and
// bytes.Buffer is not safe for that - a plain bytes.Buffer here is a data
// race, not just a theoretical one.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.String()
}

// sleeperArgs keeps a harmless process alive long enough to interrogate.
//
// It is ping.exe run DIRECTLY, not "cmd /c ping". cmd.exe spawns ping as a
// child, and that grandchild inherits the stdout pipe: after taskkill kills
// the tree, cmd.Wait still blocks waiting for an EOF that the surviving
// handle delays. The test that needs Wait() to return reliably cannot use a
// process with children.
var sleeperArgs = []string{"-n", "30", "127.0.0.1"}

func startSleeper(t *testing.T, w *WinOps) Proc {
	t.Helper()
	p, err := w.Start("ping.exe", sleeperArgs, "", io.Discard)
	if err != nil {
		t.Fatalf("Start(sleeper): %v", err)
	}
	t.Cleanup(func() {
		w.Kill(p.PID())
		waitFor(t, 10*time.Second, func() bool { return !w.Alive(p.PID()) })
	})
	return p
}

func waitFor(t *testing.T, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("condition not met within %s", timeout)
}

func TestWinOpsStartReturnsPID(t *testing.T) {
	w := New()
	p := startSleeper(t, w)

	if p.PID() <= 0 {
		t.Fatalf("PID = %d, want > 0", p.PID())
	}
	if !w.Alive(p.PID()) {
		t.Fatalf("Alive(%d) = false immediately after Start", p.PID())
	}

	if err := w.Kill(p.PID()); err != nil {
		t.Fatalf("Kill: %v", err)
	}
	waitFor(t, 10*time.Second, func() bool { return !w.Alive(p.PID()) })
}

func TestWinOpsStartWritesOutputToWriter(t *testing.T) {
	w := New()
	out := &syncBuffer{}

	p, err := w.Start("cmd.exe", []string{"/c", "echo hello-from-wow-hub"}, "", out)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { w.Kill(p.PID()) })

	// This is the assertion the whole start sequence rests on: routing a
	// pipe to a child that also owns a fresh console, and still reading
	// its output. If the creation flags break it, the app can never detect
	// worldserver finishing its load.
	waitFor(t, 10*time.Second, func() bool {
		return strings.Contains(out.String(), "hello-from-wow-hub")
	})
}

// readerArgs is a child that blocks on one line of stdin and then reports it.
//
// It stands in for worldserver, which behaves the same way: read a line from
// stdin, and treat the end of that stream as a request to stop. Delayed
// expansion (!x!) is the only form that is evaluated after "set /p" has run -
// %x% is expanded when the whole line is parsed, before the read happens.
var readerArgs = []string{"/v:on", "/c", "set /p x= & echo GOT:!x!"}

func startReader(t *testing.T, w *WinOps, out io.Writer) Proc {
	t.Helper()
	p, err := w.Start("cmd.exe", readerArgs, "", out)
	if err != nil {
		t.Fatalf("Start(reader): %v", err)
	}
	t.Cleanup(func() { w.Kill(p.PID()) })
	return p
}

func TestWinOpsStartChildSurvivesThePanelClosingStdin(t *testing.T) {
	// The panel holds the only writer of the child's stdin, so the panel's
	// death used to be the child's end of stream - and these servers read
	// that as "stop". Measured on this worldserver: with the writer gone it
	// finished its own initialization and shut down without serving anybody.
	//
	// The child now inherits the writer as well, so the stream has one
	// writer left after this process stops being it. Closing Stdin here is
	// exactly what the panel's exit does to that pipe, minus the process
	// going away with it.
	w := New()
	out := &syncBuffer{}
	p := startReader(t, w, out)

	if err := p.Stdin().Close(); err != nil {
		t.Fatalf("closing the panel's end of stdin: %v", err)
	}

	time.Sleep(2 * time.Second)
	if !w.Alive(p.PID()) {
		t.Fatalf("child is gone after its stdin writer closed: it saw an "+
			"end of stream and treated it as a stop request. Output was %q", out.String())
	}
	// Nothing came out of it either, and that is the same fact said the other
	// way round: "set /p" only returns at end of stream, and the echo behind
	// it runs only after that. An output here means the read did finish.
	if s := out.String(); s != "" {
		t.Fatalf("child printed %q after its stdin writer closed, so it read to the end of the stream", s)
	}
}

func TestWinOpsStartStdinStillReachesTheChild(t *testing.T) {
	// The counterpart of the test above, and the reason the pipe is kept at
	// all: "server shutdown 1" goes through it, and it is the only route
	// measured to give the world its save countdown.
	w := New()
	out := &syncBuffer{}
	p := startReader(t, w, out)

	if _, err := fmt.Fprintf(p.Stdin(), "server shutdown 1\n"); err != nil {
		t.Fatalf("writing to the child's stdin: %v", err)
	}

	waitFor(t, 10*time.Second, func() bool {
		return strings.Contains(out.String(), "GOT:server shutdown 1")
	})
}

func TestWinOpsFindByNameFindsOwnProcess(t *testing.T) {
	w := New()
	self := filepath.Base(os.Args[0])

	pids, err := w.FindByName(self)
	if err != nil {
		t.Fatalf("FindByName(%q): %v", self, err)
	}

	found := false
	for _, pid := range pids {
		if pid == os.Getpid() {
			found = true
		}
	}
	if !found {
		t.Fatalf("FindByName(%q) = %v, does not contain own pid %d", self, pids, os.Getpid())
	}
}

func TestWinOpsFindByNameNoMatchReturnsEmpty(t *testing.T) {
	w := New()

	pids, err := w.FindByName("no-such-proc-xyz.exe")
	if err != nil {
		t.Fatalf("FindByName on a missing image: %v, want nil error", err)
	}
	if pids == nil {
		t.Fatal("FindByName returned a nil slice, want an empty non-nil one")
	}
	if len(pids) != 0 {
		t.Fatalf("FindByName = %v, want empty", pids)
	}
}

// totalAndPrivateWorkingSet reads both figures straight from psapi for one
// PID, so a test can say which of the two MemoryMB chose.
func totalAndPrivateWorkingSet(t *testing.T, pid int) (total, priv uint64) {
	t.Helper()
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		t.Fatalf("OpenProcess(%d): %v", pid, err)
	}
	defer windows.CloseHandle(h)

	var ex2 processMemoryCountersEx2
	ex2.Cb = uint32(unsafe.Sizeof(ex2))
	if r, _, e := procGetProcessMemoryInfo.Call(
		uintptr(h), uintptr(unsafe.Pointer(&ex2)), uintptr(ex2.Cb)); r == 0 {
		t.Fatalf("GetProcessMemoryInfo(%d): %v", pid, e)
	}
	return uint64(ex2.WorkingSetSize), uint64(ex2.PrivateWorkingSetSize)
}

func TestProcessMemoryCountersEx2Layout(t *testing.T) {
	// A wrong layout does not fail loudly. GetProcessMemoryInfo writes what
	// fits and returns success, and the panel shows a number that is merely
	// wrong - which is the failure mode this whole area already produced
	// once. On the 64-bit ABI psapi.h gives 96 bytes with
	// PrivateWorkingSetSize at offset 80: eight SIZE_T of plain counters
	// (72), PrivateUsage (8), then the field itself.
	if got := unsafe.Sizeof(processMemoryCountersEx2{}); got != 96 {
		t.Fatalf("sizeof(PROCESS_MEMORY_COUNTERS_EX2) = %d, want 96", got)
	}
	if got := unsafe.Offsetof(processMemoryCountersEx2{}.PrivateUsage); got != 72 {
		t.Fatalf("offset of PrivateUsage = %d, want 72", got)
	}
	if got := unsafe.Offsetof(processMemoryCountersEx2{}.PrivateWorkingSetSize); got != 80 {
		t.Fatalf("offset of PrivateWorkingSetSize = %d, want 80", got)
	}
}

func TestWinOpsMemoryMBIsPositiveForSelf(t *testing.T) {
	w := New()

	mb := w.MemoryMB(os.Getpid())
	// The Go test binary keeps megabytes of heap to itself, so anything
	// below one means the private working set was not read at all.
	if mb < 1 {
		t.Fatalf("MemoryMB(self) = %d, want >= 1 (test binary is far larger than 1 MB)", mb)
	}
}

func TestWinOpsMemoryMBReportsThePrivateWorkingSet(t *testing.T) {
	// Read through GetProcessMemoryInfo, not by parsing tasklist. A wrong
	// struct layout or a missing PROCESS_QUERY_LIMITED_INFORMATION would
	// come back as 0, and the table would show every server using no memory.
	//
	// The figure has to be the private working set, because that is the
	// column Task Manager shows and therefore the one the table is checked
	// against. GetProcessMemoryInfo's plain counters report the total,
	// shared pages included, and for a process like ping that is eight
	// times the private figure - so returning the total is not a rounding
	// difference, it is a different number.
	w := New()
	p := startSleeper(t, w)

	total, priv := totalAndPrivateWorkingSet(t, p.PID())
	if priv == 0 {
		t.Fatal("private working set read as 0: the EX2 layout was not understood")
	}
	if priv >= total {
		t.Fatalf("private working set %d >= total %d, want the private figure to be the smaller one",
			priv, total)
	}
	if got, want := w.MemoryMB(p.PID()), toMB(priv); got != want {
		t.Fatalf("MemoryMB(sleeper %d) = %d, want %d (the private working set, not the total %d)",
			p.PID(), got, want, toMB(total))
	}
	if got := w.MemoryMB(p.PID()); got > 1024*64 {
		t.Fatalf("MemoryMB(sleeper) = %d MB, implausible for a ping process", got)
	}
}

func TestWinOpsTreeMemoryMBAddsARealChild(t *testing.T) {
	// MySQL on this repack is exactly this shape: a mysqld.exe parent whose
	// buffer pool lives in a mysqld.exe child. Reading the parent alone is
	// how the panel came to claim 18 MB for a database that Task Manager
	// showed at 460.
	//
	// cmd.exe is the honest stand-in: /c runs the child and waits for it, so
	// the pair really is two live processes, and the child is the one that
	// holds memory worth measuring.
	//
	// This is also what keeps the tree sum from being a sum of rounded
	// megabytes. cmd is about 2 MB and its ping about 1; summed as whole
	// megabytes after rounding each, the child can land on 0 and the tree
	// comes out equal to the root.
	w := New()
	p, err := w.Start("cmd.exe", []string{"/c", "ping", "-n", "8", "127.0.0.1"}, "", io.Discard)
	if err != nil {
		t.Fatalf("Start(cmd): %v", err)
	}
	t.Cleanup(func() {
		w.Kill(p.PID())
		waitFor(t, 10*time.Second, func() bool { return !w.Alive(p.PID()) })
	})

	if w.memoryBytes(p.PID()) == 0 {
		t.Fatalf("private working set of cmd %d could not be read at all", p.PID())
	}

	// A process that has just been created has barely touched any memory,
	// and its private working set is smaller than the total one - so both
	// figures start out at zero megabytes, and the child has not appeared
	// yet either. Poll for both rather than assume either.
	var own, tree int
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		own = w.MemoryMB(p.PID())
		tree = w.TreeMemoryMB(p.PID())
		if tree > own {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if tree <= own {
		t.Fatalf("TreeMemoryMB(cmd %d) = %d, want more than the root's own %d: the ping child was not counted",
			p.PID(), tree, own)
	}
}

func TestWinOpsAliveIsTrueThenFalse(t *testing.T) {
	w := New()
	p := startSleeper(t, w)

	if !w.Alive(p.PID()) {
		t.Fatalf("Alive(%d) = false while running", p.PID())
	}
	if err := w.Kill(p.PID()); err != nil {
		t.Fatalf("Kill: %v", err)
	}
	waitFor(t, 10*time.Second, func() bool { return !w.Alive(p.PID()) })
}

func TestWinOpsKillTerminatesProcess(t *testing.T) {
	w := New()
	out := &syncBuffer{}
	p, err := w.Start("ping.exe", sleeperArgs, "", out)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	if err := w.Kill(p.PID()); err != nil {
		t.Fatalf("Kill: %v", err)
	}

	done := make(chan error, 1)
	go func() { done <- p.Wait() }()

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatalf("Wait() did not return within 10s after Kill(%d)", p.PID())
	}
}

func TestWinOpsListenDetectsOpenAndClosedPort(t *testing.T) {
	w := New()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port

	if !w.Listen(port) {
		t.Fatalf("Listen(%d) = false while the port is open", port)
	}

	ln.Close()
	waitFor(t, 5*time.Second, func() bool { return !w.Listen(port) })
}

func TestWinOpsRunReturnsErrorOnNonZeroExit(t *testing.T) {
	w := New()

	if err := w.Run("cmd.exe", "/c", "exit 0"); err != nil {
		t.Fatalf("Run(exit 0) = %v, want nil", err)
	}
	err := w.Run("cmd.exe", "/c", "exit 3")
	if err == nil {
		t.Fatal("Run(exit 3) = nil, want an error")
	}
	if !strings.Contains(err.Error(), "3") {
		t.Fatalf("Run(exit 3) error = %q, want it to carry the exit status", err)
	}
}

func TestWinOpsStartRunsInTheGivenWorkingDirectory(t *testing.T) {
	// worldserver rejects an absolute --config and resolves
	// "configs/worldserver.conf" against its own working directory. Started
	// anywhere else it dies in the first second with a path error, so this
	// is the difference between a working server and a dead one.
	w := New()
	out := &syncBuffer{}
	want := t.TempDir()

	p, err := w.Start("cmd.exe", []string{"/c", "cd"}, want, out)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { w.Kill(p.PID()) })

	waitFor(t, 10*time.Second, func() bool {
		got := strings.TrimSpace(out.String())
		got = strings.Trim(got, `"`)
		// Windows reports the short or long form depending on the path;
		// compare case-insensitively on the last path element.
		return got != "" && strings.EqualFold(filepath.Base(got), filepath.Base(want))
	})
	if got := strings.TrimSpace(out.String()); got == "" {
		t.Fatal("the child printed nothing, so 'cd' never ran")
	}
}

func TestWinOpsPortOwnerFindsTheListeningProcess(t *testing.T) {
	// MySQL 8.4.7 on Windows runs as a parent/child pair of mysqld.exe and
	// only the child owns port 3306. Picking the lowest PID - the parent -
	// would show the user a process that listens on nothing, so the app asks
	// the port instead of guessing.
	w := New()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port

	pid, ok := w.PortOwner(port)
	if !ok {
		t.Fatalf("PortOwner(%d) found nothing while this test process is listening on it", port)
	}
	if pid != os.Getpid() {
		t.Fatalf("PortOwner(%d) = %d, want our own pid %d", port, pid, os.Getpid())
	}
}

func TestWinOpsPortOwnerReportsNoOwner(t *testing.T) {
	w := New()
	// Bind and immediately release, so nothing is listening.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()

	if pid, ok := w.PortOwner(port); ok {
		t.Fatalf("PortOwner(%d) = %d, want no owner after the listener closed", port, pid)
	}
}

func TestWinOpsPortOwnerRejectsUnprivilegedPort(t *testing.T) {
	// Ports below 1024 need elevation to bind, so any process "listening"
	// there belongs to something else entirely. Reporting our own PID for
	// port 80 would be worse than reporting nothing.
	w := New()
	if pid, ok := w.PortOwner(80); ok && pid == os.Getpid() {
		t.Fatal("PortOwner(80) claimed this process owns a privileged port")
	}
}

func TestWinOpsStdinNeverReachesEOF(t *testing.T) {
	// The bug this app shipped with. exec.Cmd gives a child os.DevNull when
	// Stdin is nil, and this worldserver reads that EOF as a stop request:
	// it completes every line of initialization and then shuts itself down
	// before serving anything, silently. Verified against the real binary.
	//
	// "more" stands in for it: it reads stdin until EOF. On the null device
	// it exits immediately; on a pipe this app holds open it blocks
	// forever. Which of the two happened is the whole assertion.
	w := New()

	p, err := w.Start("cmd.exe", []string{"/c", "more"}, "", io.Discard)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { w.Kill(p.PID()) })

	exited := make(chan struct{})
	go func() {
		p.Wait()
		close(exited)
	}()

	select {
	case <-exited:
		t.Fatal("the child saw EOF on stdin and exited; it would have shut the worldserver down on its own")
	case <-time.After(3 * time.Second):
		// Still reading, which is what we want.
	}
}

func TestWinOpsKillErrorCarriesPID(t *testing.T) {
	w := New()
	// A PID that cannot exist. taskkill must fail, and the error must name
	// the process: the stop ladder shows this text verbatim, and "operation
	// failed" without a PID tells the user nothing.
	err := w.Kill(0x7FFFFFF0)
	if err == nil {
		t.Skip("taskkill reported success for an impossible PID; nothing to assert")
	}
	if !strings.Contains(err.Error(), strconv.Itoa(0x7FFFFFF0)) {
		t.Fatalf("Kill error = %q, want it to contain the PID", err)
	}
}

func TestHiddenCmdHidesItsWindow(t *testing.T) {
	// The app is a GUI binary and every helper it runs - tasklist, netstat,
	// taskkill, mysqladmin - is a console program. Without HideWindow each
	// call flashes a console window, and the status loop makes several calls
	// a second for stopped services, so the user saw windows blinking
	// forever.
	cmd := hiddenCmd("cmd.exe", "/c", "echo hi")

	if cmd.SysProcAttr == nil || !cmd.SysProcAttr.HideWindow {
		t.Fatal("SysProcAttr.HideWindow is not set; a console window will flash on every call")
	}
	// The child still needs its own console where the app depends on one -
	// CREATE_NO_WINDOW here would break SendLine and SendCtrlC.
	if cmd.SysProcAttr.CreationFlags&windows.CREATE_NO_WINDOW != 0 {
		t.Error("CREATE_NO_WINDOW is set on a helper command; it is the wrong tool and hides real failures")
	}
}

func TestRunDoesNotFlashAWindow(t *testing.T) {
	// Exercises the path that actually caused it: a console program launched
	// through Run, which the stop path uses for mysqladmin.
	before := countVisibleWindows(t)

	for i := 0; i < 3; i++ {
		if err := New().Run("cmd.exe", "/c", "echo hidden"); err != nil {
			t.Fatalf("Run: %v", err)
		}
	}

	if after := countVisibleWindows(t); after > before {
		t.Fatalf("visible windows went from %d to %d across three Run calls; "+
			"something is flashing a console", before, after)
	}
}

// countVisibleWindows counts top-level windows that are currently on screen.
func countVisibleWindows(t *testing.T) int {
	t.Helper()
	n := 0
	cb := windows.NewCallback(func(hwnd windows.HWND, _ uintptr) uintptr {
		if windows.IsWindowVisible(hwnd) {
			n++
		}
		return 1
	})
	if err := windows.EnumWindows(cb, unsafe.Pointer(nil)); err != nil {
		t.Fatalf("EnumWindows: %v", err)
	}
	return n
}

// The disk figure decides whether the next 385 MB backup fits, and a wrong
// one is worse than none: it either blocks a backup that would have
// succeeded or green-lights one that fills the volume halfway.
func TestWinOpsDiskFreeReportsARealVolume(t *testing.T) {
	dir := t.TempDir()

	free, total, err := New().DiskFree(dir)
	if err != nil {
		t.Fatalf("DiskFree(%s): %v", dir, err)
	}
	if total <= 0 {
		t.Errorf("total = %d, want a positive volume size", total)
	}
	if free <= 0 {
		t.Errorf("free = %d, want a positive free space", free)
	}
	if free > total {
		t.Errorf("free %d exceeds total %d, which is impossible", free, total)
	}
}

// A missing folder must fail rather than silently falling back to the
// current drive: the caller passes the backups folder precisely because a
// folder that is not there is what it needs to report.
func TestWinOpsDiskFreeFailsOnAMissingPath(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "definitely", "not", "here")

	if _, _, err := New().DiskFree(missing); err == nil {
		t.Fatal("want an error for a path that does not exist")
	}
}
