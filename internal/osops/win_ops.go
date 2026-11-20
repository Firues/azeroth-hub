//go:build windows

package osops

import (
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"

	"wow-hub/internal/i18n"
)

// stillActive is STILL_ACTIVE from winbase.h, which
// golang.org/x/sys/windows does not export. Verified with `go doc` before
// writing code against it, which is why it is a local constant rather than a
// reference.
const stillActive = 259

// WinOps is the real implementation of Ops, talking to Windows.
//
// The only field callers should touch is CreationFlags, and only Task 2 -
// the spike - has a reason to. It is exported so the spike can try both
// flag combinations without editing this file.
type WinOps struct {
	// CreationFlags are passed to CreateProcess.
	//
	// CREATE_NEW_CONSOLE and nothing else. A real console is required:
	// SendLine and SendCtrlC attach to it and write to its input buffer, and
	// a process without one cannot be given either.
	//
	// CREATE_NEW_PROCESS_GROUP is deliberately NOT set. Measured here: with
	// it, GenerateConsoleCtrlEvent left a fully-initialized worldserver
	// completely untouched for 60 seconds - zero pool closures, zero save
	// countdown. A process in its own group stops receiving console control
	// events delivered by group, which is exactly what the console-attach
	// path depends on. Removing it is what makes Ctrl+C reachable.
	//
	// CREATE_NO_WINDOW is likewise forbidden: it would leave the process
	// with no console at all.
	CreationFlags uint32

	// HideWindow hides the console at creation. The servers print to a log
	// file regardless, so the window is only there to be attached to.
	HideWindow bool
}

// New returns a WinOps with the flags this app is designed around.
func New() *WinOps {
	return &WinOps{
		CreationFlags: windows.CREATE_NEW_CONSOLE,
		HideWindow:    true,
	}
}

// hiddenCmd builds a command that will not flash a console window.
//
// The app is a GUI binary, and tasklist, netstat, taskkill and mysqladmin
// are all console-subsystem programs. Launched without HideWindow, each one
// makes a console window appear for the few milliseconds it runs. The status
// loop calls into them once a second for every stopped service, so the user
// saw windows blinking forever - a loop with no cause visible from outside.
//
// This is the fix, and it belongs on every helper command. CREATE_NO_WINDOW
// would also suppress it, but it is the wrong tool here: these are short
// utilities, not servers that need a console of their own.
func hiddenCmd(exe string, args ...string) *exec.Cmd {
	cmd := exec.Command(exe, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	return cmd
}

// Compile-time proof that the real thing satisfies the interface the rest
// of the app programs against.
var _ Ops = (*WinOps)(nil)

// osProc is a process we started ourselves, so we hold the *exec.Cmd.
type osProc struct {
	cmd *exec.Cmd

	// stdin is the never-closing pipe described in Start. Held so the
	// cleanup goroutine can close it once the child is gone.
	stdin io.WriteCloser

	// waitOnce makes Wait idempotent, and it has to be: two callers wait on
	// every process - the cleanup goroutine Start spawns, which closes
	// stdin once the child is gone, and the supervisor's exit watcher.
	// exec.Cmd.Wait is documented for a single caller. Today the second one
	// happens to block and return the same result, but that is incidental
	// behaviour, not a contract, and the stop ladder must not rest on it.
	waitOnce sync.Once
	waitErr  error
}

func (p *osProc) Stdin() io.WriteCloser { return p.stdin }

func (p *osProc) PID() int { return p.cmd.Process.Pid }

// Wait reaps the process and closes its stdin, exactly once no matter how
// many callers ask.
func (p *osProc) Wait() error {
	p.waitOnce.Do(func() {
		p.waitErr = p.cmd.Wait()
		p.stdin.Close()
	})
	return p.waitErr
}

func (w *WinOps) Start(exe string, args []string, dir string, out io.Writer) (Proc, error) {
	cmd := exec.Command(exe, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: w.CreationFlags,
		HideWindow:    w.HideWindow,
	}
	// An empty dir means "inherit", which is what the tests want. The app
	// always passes the repack root, because worldserver resolves its
	// config and its data relative to its own working directory.
	cmd.Dir = dir

	// Both streams go to the same writer on purpose. worldserver writes
	// some diagnostics to stderr, and losing them would mean a failure
	// that the app can see but cannot explain.
	cmd.Stdout = out
	cmd.Stderr = out

	// Stdin gets a pipe, and the child is made a second writer of it.
	//
	// Leaving Stdin nil hands the child os.DevNull (documented behaviour of
	// exec.Cmd), and that is fatal here. Measured on this worldserver: with
	// /dev/null on stdin it finishes its whole initialization - every one
	// of ~140 lines of module init, DBCs, travel caches - and then shuts
	// itself down before serving a single request, with no error and no
	// exit code that says why. The cause is the console reader: it sees EOF
	// and treats it as a stop request.
	//
	// A pipe this process alone writes to is the same trap with a delay. EOF
	// arrives when every writer is gone, so closing the panel's end - which
	// is what closing the panel does to it - shut the world down by itself,
	// with no save countdown, minutes after the panel had gone. Measured on
	// the real logs: the world died seconds after each panel restart, and
	// once on a run where no human player had ever connected.
	//
	// So the write end is inherited by the child as well and never touched
	// there. One writer always remains, and the pipe can no longer reach
	// end-of-stream no matter how this process ends. It is a handle the
	// child holds and never uses; nothing in this design depends on it.
	//
	// The pipe is still the primary route for "server shutdown 1" - the only
	// route measured to give the world its save countdown. Inheritance does
	// not make that harder: a reader cannot tell who wrote the line.
	stdinR, stdinW, err := os.Pipe()
	if err != nil {
		return nil, fmt.Errorf("%s: %w", i18n.Tf("ops.stdin", exe), err)
	}
	cmd.Stdin = stdinR
	cmd.SysProcAttr.AdditionalInheritedHandles = []syscall.Handle{
		syscall.Handle(stdinW.Fd()),
	}

	if err := cmd.Start(); err != nil {
		stdinR.Close()
		stdinW.Close()
		return nil, fmt.Errorf("%s: %w", i18n.Tf("ops.launch", exe), err)
	}
	// Our copy of the read end goes away with Start, as it does for
	// StdinPipe: a panel still holding it would be a second reader on a
	// stream nobody can serve. Both os.Pipe ends are inheritable, and exec
	// passes the write end as an explicit handle list, so no unrelated
	// handle of this process leaks into the child.
	stdinR.Close()

	p := &osProc{cmd: cmd, stdin: stdinW}
	// Reap the child and close the write end even if nobody ever calls Wait,
	// or a long-lived app accumulates one handle per server start. Wait is
	// idempotent, so the supervisor's exit watcher calling it as well is
	// harmless.
	go p.Wait()
	return p, nil
}

// taskRow is one line of `tasklist /FO CSV /NH`.
//
// Verified format on this machine:
//
//	"explorer.exe","21856","Console","1","227 460 КБ"
//
// Only the image name and the PID are read from it. The memory column is
// localized - the unit and even the thousands separator differ by locale -
// which is one of the reasons MemoryMB asks Win32 instead.
func tasklist() ([][]string, error) {
	out, err := hiddenCmd("tasklist", "/FO", "CSV", "/NH").Output()
	if err != nil {
		return nil, fmt.Errorf("tasklist: %w", err)
	}

	r := csv.NewReader(strings.NewReader(string(out)))
	r.FieldsPerRecord = -1
	records, err := r.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("%s: %w", i18n.T("ops.tasklist"), err)
	}
	return records, nil
}

func (w *WinOps) FindByName(name string) ([]int, error) {
	// Empty and non-nil on purpose: callers range over this result to
	// adopt processes, and "nothing is running" is the normal case at
	// startup, not a failure.
	pids := []int{}

	rows, err := tasklist()
	if err != nil {
		return nil, err
	}

	want := strings.ToLower(name)
	for _, row := range rows {
		if len(row) < 2 {
			continue
		}
		if !strings.EqualFold(row[0], want) {
			continue
		}
		if pid, err := strconv.Atoi(strings.TrimSpace(row[1])); err == nil {
			pids = append(pids, pid)
		}
	}
	return pids, nil
}

// processMemoryCounters is PROCESS_MEMORY_COUNTERS from psapi.h. SIZE_T is
// uintptr, and the struct is laid out for the 64-bit ABI this app is built
// for.
type processMemoryCounters struct {
	Cb                         uint32
	PageFaultCount             uint32
	PeakWorkingSetSize         uintptr
	WorkingSetSize             uintptr
	QuotaPeakPagedPoolUsage    uintptr
	QuotaPagedPoolUsage        uintptr
	QuotaPeakNonPagedPoolUsage uintptr
	QuotaNonPagedPoolUsage     uintptr
	PagefileUsage              uintptr
	PeakPagefileUsage          uintptr
}

// processMemoryCountersEx2 is PROCESS_MEMORY_COUNTERS_EX2: the counters
// above, then PrivateUsage, then the two fields Windows 10 1607 added.
//
// PrivateWorkingSetSize is the figure Task Manager's "Память" column shows,
// and it is the one the table reports. WorkingSetSize is bigger, because it
// counts shared pages too - and for a process that mostly maps DLLs the
// difference is not small. Measured on this repack, with MySQL and
// authserver running:
//
//	                 total WS   private WS
//	mysqld child        478         443
//	authserver           18           4
//	ping                  8           1
//
// Reporting the total is how the table came to disagree with Task Manager
// by a factor of four on authserver and eight on a sleeper.
type processMemoryCountersEx2 struct {
	processMemoryCounters
	PrivateUsage          uintptr
	PrivateWorkingSetSize uintptr
	SharedCommitUsage     uintptr
}

var (
	psapi                    = windows.NewLazySystemDLL("psapi.dll")
	procGetProcessMemoryInfo = psapi.NewProc("GetProcessMemoryInfo")
)

const mb = 1024 * 1024

// toMB rounds bytes to whole megabytes, to the nearest one.
//
// Rounding to nearest rather than truncating matters at the small end: a
// process sitting on 1.4 MB is one megabyte of server, not zero.
func toMB(b uint64) int { return int((b + mb/2) / mb) }

// memoryBytes reports the private working set of pid in bytes, or 0 when it
// cannot be read.
//
// Win32, not `tasklist`. This runs once a second for every running service,
// and shelling out to a console program at that rate is both wasteful and -
// because it used to happen with the supervisor's state lock held - a source
// of contention with the stop path, which is the last place that should ever
// be made to wait.
func (w *WinOps) memoryBytes(pid int) uint64 {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return 0
	}
	defer windows.CloseHandle(h)

	var ex2 processMemoryCountersEx2
	ex2.Cb = uint32(unsafe.Sizeof(ex2))
	if r, _, _ := procGetProcessMemoryInfo.Call(
		uintptr(h), uintptr(unsafe.Pointer(&ex2)), uintptr(ex2.Cb)); r != 0 {
		return uint64(ex2.PrivateWorkingSetSize)
	}

	// Windows older than 10 1607 does not know the EX2 layout and rejects
	// it outright. Fall back to the plain counters rather than showing
	// nothing; that figure is the TOTAL working set, so on such a machine
	// the column reads larger than Task Manager would show it.
	var plain processMemoryCounters
	plain.Cb = uint32(unsafe.Sizeof(plain))
	if r, _, _ := procGetProcessMemoryInfo.Call(
		uintptr(h), uintptr(unsafe.Pointer(&plain)), uintptr(plain.Cb)); r != 0 {
		return uint64(plain.WorkingSetSize)
	}
	return 0
}

// MemoryMB reports the private working set of pid alone, in whole megabytes,
// or 0 when it cannot be read. TreeMemoryMB is the one the table uses.
func (w *WinOps) MemoryMB(pid int) int { return toMB(w.memoryBytes(pid)) }

// childMap reads the whole process table once and returns parent PID ->
// child PIDs.
//
// One Toolhelp32 snapshot rather than a query per process: this is walked
// once a second for every running service, and a snapshot of the process
// list costs milliseconds where shelling out to tasklist costs a hundred.
func childMap() (map[int][]int, error) {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", i18n.T("ops.pslist"), err)
	}
	defer windows.CloseHandle(snap)

	kids := map[int][]int{}
	var entry windows.ProcessEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))
	for e := windows.Process32First(snap, &entry); e == nil; e = windows.Process32Next(snap, &entry) {
		parent, child := int(entry.ParentProcessID), int(entry.ProcessID)
		kids[parent] = append(kids[parent], child)
	}
	return kids, nil
}

// TreeMemoryMB reports the private working set of root and of everything it
// spawned. The interface says why the tree is the right unit; this is the
// walk.
//
// Bytes are summed and rounded once, not summed as megabytes. Rounding each
// process first loses up to a megabyte each, and in a two-process tree that
// is the whole difference being measured: cmd.exe at 2 MB with a 1 MB ping
// under it adds up to 3 MB, but round the child to 0 and the tree looks
// exactly like the root alone.
func (w *WinOps) TreeMemoryMB(root int) int {
	if root <= 0 {
		return 0
	}

	kids, err := childMap()
	if err != nil {
		// The root's own figure is still worth having: a process table we
		// could not read is no reason to report nothing at all.
		return w.MemoryMB(root)
	}

	var total uint64
	seen := map[int]bool{root: true}
	queue := []int{root}
	for len(queue) > 0 {
		pid := queue[0]
		queue = queue[1:]
		total += w.memoryBytes(pid)
		for _, kid := range kids[pid] {
			if seen[kid] {
				// Windows reuses PIDs, so a parent link left behind by an
				// exited process can point back into this tree. Without
				// seen, that walk never ends.
				continue
			}
			seen[kid] = true
			queue = append(queue, kid)
		}
	}
	return toMB(total)
}

// SendLine and SendCtrlC forward to the console implementation. They are
// methods on WinOps rather than free functions so that Ops covers every
// Windows call the app makes - a supervisor that reached past the interface
// into package-level Win32 could not be tested at all.
func (w *WinOps) SendLine(pid int, text string) error { return SendLine(pid, text) }

func (w *WinOps) SendCtrlC(pid int) error { return SendCtrlC(pid) }

func (w *WinOps) Kill(pid int) error {
	if err := hiddenCmd("taskkill", "/PID", strconv.Itoa(pid), "/T", "/F").Run(); err != nil {
		// Unwrapped apart from the PID: the stop ladder shows this text to
		// the user verbatim, and a failure with no process named is a
		// failure they cannot act on.
		return fmt.Errorf("%s: %w", i18n.Tf("ops.taskkill", pid), err)
	}
	return nil
}

var (
	user32         = windows.NewLazySystemDLL("user32.dll")
	procShowWindow = user32.NewProc("ShowWindow")
)

// ShowWindow is not exported by golang.org/x/sys/windows, so it is bound
// here. SW_MINIMIZE, not SW_HIDE: a hidden console can still be brought
// back but not clicked into, and the manual Ctrl+C fallback depends on
// the window being reachable.
func showWindow(hwnd windows.HWND, cmd uintptr) {
	procShowWindow.Call(uintptr(hwnd), cmd)
}

func (w *WinOps) HideConsole(pid int) {
	target := uint32(pid)
	// A PID can own more than one window, so this keeps going after a
	// match rather than returning on the first one.
	cb := windows.NewCallback(func(hwnd windows.HWND, _ uintptr) uintptr {
		var owner uint32
		windows.GetWindowThreadProcessId(hwnd, &owner)
		if owner != target {
			return 1 // keep enumerating
		}
		if !windows.IsWindowVisible(hwnd) {
			return 1
		}
		showWindow(hwnd, windows.SW_MINIMIZE)
		return 1
	})
	windows.EnumWindows(cb, unsafe.Pointer(nil))
}

func (w *WinOps) Run(exe string, args ...string) error {
	return hiddenCmd(exe, args...).Run()
}

// RunTo is Run with the streams kept apart; see the note on Ops.
//
// dir is explicit because a .cmd has to be started from its own directory:
// this repack's path contains parentheses, and cmd.exe mangles a quoted
// path containing them. Running the script by bare filename with the
// working directory set to its folder keeps the path out of the command
// line entirely - and the scripts resolve the repack root from %~dp0, so
// they do not care where they were started from.
func (w *WinOps) RunTo(ctx context.Context, exe string, args []string, dir string, out io.Writer) (string, error) {
	cmd := exec.CommandContext(ctx, exe, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	cmd.Dir = dir
	cmd.Stdout = out

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	err := cmd.Run()
	return stderr.String(), err
}

// CmdExe is the command interpreter, for running the repack's own .cmd
// scripts.
//
// Resolved from %SystemRoot% rather than left as a bare "cmd.exe": this app
// is a GUI binary with no console and no inherited PATH worth trusting, and
// a bare name would be resolved against whatever directory the user
// happened to launch it from. A cmd.exe planted next to the exe would then
// run instead of the system one.
//
// It is a function rather than a Layout field because it is not a fact about
// the repack - every Windows machine has one, at the same place.
func CmdExe() string {
	if root := os.Getenv("SystemRoot"); root != "" {
		return filepath.Join(root, "System32", "cmd.exe")
	}
	return "cmd.exe"
}

// OpenPath hands path to Explorer.
//
// explorer.exe rather than `cmd /c start`: start goes through the command
// interpreter, whose argument parsing mangles a path containing spaces and
// parentheses - and this repack's folder name contains both. Explorer takes
// the path as one argument and opens the folder.
//
// Explorer's exit status is not a success signal - it returns 1 whether or
// not a window appeared - so the error is only about failing to launch it
// at all. The process is reaped in the background rather than waited for:
// with an Explorer already running this one exits at once, and with none
// running it lives as long as the window does.
//
// No HideWindow. Explorer is a GUI program with no console to flash, and
// suppressing its window would hide the very folder this was called to
// show.
func (w *WinOps) OpenPath(path string) error {
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("%s: %w", i18n.Tf("ops.open", path), err)
	}

	cmd := exec.Command("explorer.exe", path)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("%s: %w", i18n.Tf("ops.open", path), err)
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

func (w *WinOps) Listen(port int) bool {
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 300*time.Millisecond)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

func (w *WinOps) Alive(pid int) bool {
	// OpenProcess rather than tasklist: Alive is called from polling
	// loops, and spawning a process per poll to answer "is this PID still
	// there" would be absurd.
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	defer windows.CloseHandle(h)

	var code uint32
	if err := windows.GetExitCodeProcess(h, &code); err != nil {
		return false
	}
	return code == stillActive
}

func (w *WinOps) PortOwner(port int) (int, bool) {
	// Binding a port under 1024 needs elevation, so no process this app
	// starts can own one. Reporting an owner there would name a privileged
	// service the app has nothing to do with.
	if port < 1024 {
		return 0, false
	}

	out, err := hiddenCmd("netstat", "-ano", "-p", "TCP").Output()
	if err != nil {
		return 0, false
	}

	// Format:
	//   TCP    127.0.0.1:3306   0.0.0.0:0   LISTENING   74248
	want := fmt.Sprintf(":%d", port)
	fields := strings.Fields(string(out))
	for i := 0; i+3 < len(fields); i++ {
		if !strings.EqualFold(fields[i], "TCP") {
			continue
		}
		if fields[i+1] != want && !strings.HasSuffix(fields[i+1], want) {
			continue
		}
		if !strings.EqualFold(fields[i+3], "LISTENING") {
			continue
		}
		if pid, err := strconv.Atoi(fields[i+4]); err == nil {
			return pid, true
		}
	}
	return 0, false
}

// RealClock is the production Clock. Tests never see it.
type RealClock struct{}

var _ Clock = RealClock{}

func (RealClock) Now() time.Time { return time.Now() }

func (RealClock) After(d time.Duration) <-chan time.Time { return time.After(d) }

// DiskFree reports the free and total bytes on the volume holding dir.
//
// GetDiskFreeSpaceEx wants a directory, and a path that does not exist
// fails rather than falling back to the current drive - which is the
// behaviour worth having here, because the caller passes the backups
// folder and a missing folder is exactly the thing it should report.
func (w *WinOps) DiskFree(dir string) (int64, int64, error) {
	p, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		return 0, 0, err
	}

	var freeToCaller, total, totalFree uint64
	if err := windows.GetDiskFreeSpaceEx(p, &freeToCaller, &total, &totalFree); err != nil {
		return 0, 0, err
	}

	// freeToCaller, not totalFree: they differ by the quota reserved for
	// the caller on a volume with per-user quotas, and freeToCaller is the
	// one that decides whether the next write succeeds.
	return int64(freeToCaller), int64(total), nil
}
