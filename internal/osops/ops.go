// Package osops is the app's only door to Windows.
//
// Every call the application makes on the operating system goes through
// Ops. That is not architectural neatness for its own sake: the stop
// ladder decides whether a worldserver process dies gracefully or gets
// killed, and a mistake there costs a characters database. Behind this
// interface that ladder can be tested exhaustively against fake
// processes, with a fake clock, in milliseconds.
package osops

import (
	"context"
	"io"
	"time"
)

// Proc is a process this application started itself.
type Proc interface {
	PID() int
	// Wait blocks until the process exits and reports how it exited.
	Wait() error
	// Stdin is the write end of the child's stdin pipe, handed to it at
	// Start.
	//
	// It exists for the one command that matters: these servers read console
	// commands from stdin, and "server shutdown 1" is the orderly path -
	// the only one that runs the main loop's save countdown. Writing a
	// newline-terminated command here is therefore the graceful stop, if
	// the server's reader can see a pipe at all. See
	// docs/superpowers/spikes/2026-09-29-ctrl-break.md for what was
	// measured; spec 12.1's claim that this path cannot work was derived
	// from symbols in a PDB, which proves nothing.
	//
	// Closing it is safe but pointless: the child holds the write end too,
	// so the pipe can no longer reach end-of-stream however this process
	// ends. That is the point of Start - the servers shut themselves down
	// at end of stream, and a pipe only this process writes to took the
	// world down with the panel every time the panel was closed.
	//
	// And it is the end of the line for a world this panel did not start.
	// One that outlived the panel that launched it cannot be reached at all
	// - see the measurements in supervisor's stop ladder - so stopping an
	// adopted world is a kill, and the dialog that asks says so.
	Stdin() io.WriteCloser
}

// Ops is the whole surface of the operating system the app uses.
//
// The method list is not a wish list: each entry has a caller in a later
// task. FindByName and Alive exist for processes the app did not start
// (adoption), Start and Proc.Wait for the ones it did, Run for
// mysqladmin, TreeMemoryMB and HideConsole for the Server Control table, and
// Listen both for readiness waits and for the Health screen.
type Ops interface {
	// FindByName returns every PID whose executable image has this name.
	// It returns an empty slice and a nil error when nothing matches:
	// "nothing found" is a normal state, not a failure. Several matches
	// is also normal - this machine ran two explorer.exe at once while
	// this was being written.
	FindByName(name string) ([]int, error)

	// Start launches exe with args in working directory dir, routing both
	// its stdout and its stderr to out.
	//
	// dir is explicit and not taken from the app's own working directory
	// because these servers resolve their paths relative to where they
	// were launched. worldserver rejects an absolute --config outright:
	// it wants "configs/worldserver.conf", and finds it relative to its
	// own working directory. Launched from the wrong directory it dies in
	// the first second with a path error, so the caller must say where
	// the process belongs rather than inheriting wherever the user
	// happened to double-click the app.
	//
	// out is supplied by the caller because the caller is the one that has
	// to interpret what comes out: the start sequence waits for a
	// readiness line in exactly this stream.
	Start(exe string, args []string, dir string, out io.Writer) (Proc, error)

	// Kill terminates pid and its children without ceremony. It is the
	// last resort: the stop ladder only reaches it once the console command
	// and a real Ctrl+C have both been tried and both failed, and only once
	// the user has agreed to it. Killing a live worldserver is how an
	// unsaved character is lost.
	Kill(pid int) error

	// HideConsole minimizes the console window a started process owns, so
	// the three server consoles do not sit in the taskbar.
	HideConsole(pid int)

	// MemoryMB reports the private working set of pid alone, in whole
	// megabytes, or 0 if it cannot be read.
	//
	// The private working set, not the total one, because that is the
	// figure Task Manager puts in its "Память" column and therefore the
	// one a person checks the table against. The two are not close: a
	// process that mostly maps DLLs spends most of its working set on
	// pages it shares with everyone else. Measured here, authserver reads
	// 4 MB privately and 18 MB in total, ping 1 MB against 8 MB. Reporting
	// the total made the table look wrong by a factor of four on a row
	// that was in fact fine.
	MemoryMB(pid int) int

	// TreeMemoryMB reports the private working set of pid together with
	// every process below it, in whole megabytes.
	//
	// The tree, not the process, is the unit the Server Control table
	// needs. MySQL 8.4.7 on Windows is a parent/child pair of mysqld.exe:
	// the parent holds roughly 18 MB, and the child - the one that owns
	// 3306 and the 512 MB buffer pool - holds 443 MB of it. The app starts
	// the parent, so asking about that PID alone reported MySQL as using
	// 18 MB while Task Manager showed the pair at 460. Summing is what a
	// person means by "how much memory does MySQL take".
	//
	// Private pages are private, so the sum does not double-count
	// anything: 18 + 443 is the same 461 a single grouped number would
	// give.
	TreeMemoryMB(pid int) int

	// Run executes a command to completion and reports its exit status.
	// Used for mysqladmin shutdown, which is how MySQL is stopped.
	Run(exe string, args ...string) error

	// RunTo executes a command to completion with its stdout written to
	// out, and returns its stderr separately along with the exit status.
	//
	// A separate method from Run and Start, because a dump is neither.
	// Start merges stdout and stderr into one writer, and for mysqldump
	// stdout IS the .sql file - merging would write the error message into
	// the backup, producing a file that restores to something subtly wrong.
	// Run discards both, and a failed dump with no explanation is a
	// support ticket.
	//
	// stderr is returned rather than returned-as-error because it is not
	// only an error channel here: mysqldump 8.4 writes a warning about the
	// PROCESS privilege to stderr and still exits 0 with a complete dump.
	// The caller has to be able to read it and decide.
	//
	// ctx is honoured by killing the child. This is the one method in Ops
	// that takes one, and the reason is duration: a dump of all four
	// databases of this repack takes 105 seconds and writes 385 MB, so a
	// cancel button that only took effect between databases would look
	// broken for a minute. The caller owns the mess a killed dump leaves
	// behind - see dumps.Logical, which deletes the whole folder.
	RunTo(ctx context.Context, exe string, args []string, dir string, out io.Writer) (stderr string, err error)

	// Listen reports whether something accepts TCP connections on
	// 127.0.0.1:port. Used for readiness waits and Health checks.
	Listen(port int) bool

	// SendLine types text into pid's console and presses Enter, as a person
	// at the keyboard would.
	//
	// The fallback for the shutdown command: stdin is the primary route and
	// was measured working, but it depends on how a build reads input. A
	// process reading its console through ReadConsole would never see a byte
	// written to a pipe, while injected keystrokes arrive by the same route
	// a typist uses.
	SendLine(pid int, text string) error

	// SendCtrlC delivers a real Ctrl+C to pid's console.
	//
	// Not a group-directed signal: measured, that left a fully-initialized
	// worldserver untouched for 60 seconds, because a process in its own
	// group stops receiving control events delivered that way. Attaching to
	// the server's console and raising the event there reaches it.
	SendCtrlC(pid int) error

	// Alive reports whether pid still exists. Unlike Proc.Wait this works
	// for processes the app did not start, which is the only case that
	// needs polling.
	Alive(pid int) bool

	// PortOwner returns the PID of whatever process is listening on
	// 127.0.0.1:port, and whether anything was found.
	//
	// It exists because matching an image name is not enough. MySQL 8.4.7
	// on Windows runs as a parent/child pair of mysqld.exe, and only the
	// child owns port 3306; picking the lower PID adopts the parent, which
	// listens on nothing. Two worldserver.exe processes cannot both serve
	// 8085 either, so the port identifies the live one where the image name
	// cannot.
	//
	// Ports below 1024 require elevation to bind, so nothing this app
	// starts can own them; any owner reported there belongs to something
	// else and is reported as absent.
	PortOwner(port int) (int, bool)

	// OpenPath reveals path in the shell's own way: a folder in Explorer, a
	// file with whatever is registered for it.
	//
	// It exists for the two things a person does next after reading a
	// screen - look at the folder a rollback point went into, and open the
	// log they were told to send. Both are otherwise a hunt through
	// Explorer starting from wherever the repack happens to live.
	OpenPath(path string) error

	// DiskFree reports the free and total bytes on the volume holding
	// dir, which must be a directory that exists.
	//
	// It exists because a backup on this repack costs 385 MB and the
	// backups folder only ever grows: there is no rotation anywhere, and
	// the scripts that make the dumps never check. Running out of space
	// during a dump produces a truncated .sql file that looks like a
	// backup until the day it is needed, so the figure is worth showing
	// before the button is pressed rather than after.
	DiskFree(dir string) (free, total int64, err error)
}

// Clock is time, made injectable.
//
// The stop ladder waits 30 seconds for a graceful exit and 60 more for a
// person to answer a dialog. Tested against the real clock, that is a
// two-minute test suite nobody runs. Tested against this, it is
// instantaneous.
type Clock interface {
	Now() time.Time
	After(d time.Duration) <-chan time.Time
}
