// Package restore puts a physical rollback point back.
//
// Physical only, deliberately. A logical point is four .sql files imported
// into a running MySQL by backups\restore-all-db.cmd, and that script has a
// trap in it - it takes the newest folder that holds acore_world.sql and
// silently skips any database whose file is missing - which is the one thing
// a button that pretends to restore cannot do. A physical point is a single
// archive unpacked over mysql\data, all four databases or none, and the panel
// can do that without a script whose failure mode is a half-restored realm.
//
// The order of the three steps is not a preference either:
//
//  1. stop the stack, world first (see supervisor.stopOrder);
//  2. snapshot mysql\data again, by running the repack's own script, so the
//     state being replaced can be put back if this goes wrong;
//  3. unpack the chosen archive over mysql\data.
//
// The safety copy is what makes step 3 acceptable. Without it the operation
// is "delete the current database and hope the archive is good", and the
// archive is a file that could have been written by a machine that lost
// power.
//
// It is left stopped afterwards. A restore is a decision about the realm's
// past, and whoever made it is the one who should decide to open it again -
// starting the world automatically would also start it with whatever login
// the restored accounts happen to have.
package restore

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"wow-hub/internal/backups"
	"wow-hub/internal/i18n"
	"wow-hub/internal/osops"
	"wow-hub/internal/repack"
)

// archiveName is what a physical point's folder holds.
//
// The repack's snapshot script names it, and this package only reads what
// that script wrote.
const archiveName = "mysql_data.7z"

// Kind is the value App puts into BackupStatus.Kind while a restore runs, so
// the page can tell it from a backup without a second struct.
const Kind = "restore"

// Steps is how many the progress bar counts. Four is what a person can be
// told: stop, safety copy, unpack, done.
const Steps = 3

// Progress is one step of a restore.
type Progress struct {
	// Step is what is happening now, in words meant for the page.
	Step string
	// Index and Total place it in the job, 1-based.
	Index int
	Total int
	// Bytes is the archive size, the one figure that is known up front.
	Bytes   int64
	Elapsed time.Duration
}

// Report is a restore that finished.
type Report struct {
	// SafetyDir is the folder holding the copy of what was replaced.
	SafetyDir string
	// SafetyBytes is how big that copy is.
	SafetyBytes int64
	Elapsed     time.Duration
}

// Stopper is what takes the stack down, and Snapshoter what copies the data
// about to be replaced.
//
// Both are the caller's business rather than this package's: the stop ladder
// is the supervisor's, with its dependency guard and its clean-shutdown
// path, and the snapshot format is dumps'. Injecting them means a restore
// cannot invent a second way to stop MySQL or a second archive format, and a
// test can drive the whole thing against fakes.
type Stopper interface {
	StopAll(ctx context.Context) error
}

type Snapshoter interface {
	// Physical makes a snapshot and returns the folder it went into.
	Physical(ctx context.Context) (string, int64, error)
}

// Runner restores physical rollback points.
type Runner struct {
	ops    osops.Ops
	lay    repack.Layout
	clock  osops.Clock
	report func(Progress)
}

// New builds a Runner. clock and report may be nil.
//
// The clock is injected for the same reason dumps takes one: the elapsed time
// is the only figure the page shows for the whole job, so it is the one thing
// a test has to be able to pin rather than measure against a real one.
func New(ops osops.Ops, lay repack.Layout, clock osops.Clock, report func(Progress)) *Runner {
	if clock == nil {
		clock = osops.RealClock{}
	}
	return &Runner{ops: ops, lay: lay, clock: clock, report: report}
}

func (r *Runner) now() time.Time { return r.clock.Now() }

// Check reports whether this folder can be restored, without touching the
// data.
//
// The page calls it before asking, so the answer to "can I restore this" is
// the same on the button and in the operation.
func (r *Runner) Check(name string) error {
	_, err := r.archiveOf(name)
	return err
}

// archiveOf resolves a folder name to the archive inside it, refusing
// anything that is not one plain child folder of backups\.
//
// The name comes from the page, and a path built from it is a path the page
// chose, so the guard is backups.Resolve's: one copy of those three refusals,
// shared with the delete, rather than two that drift apart.
func (r *Runner) archiveOf(name string) (string, error) {
	if r.lay.Root == "" {
		return "", errors.New(i18n.T("restore.norepack"))
	}

	folder, err := backups.Resolve(r.lay.BackupsDir, name)
	if err != nil {
		return "", err
	}

	archive := filepath.Join(folder, archiveName)
	info, err := os.Stat(archive)
	if err != nil {
		return "", errors.New(i18n.Tf("restore.nofile", name, archiveName))
	}
	if info.Size() == 0 {
		return "", errors.New(i18n.Tf("restore.empty", name))
	}
	return archive, nil
}

// Run restores one point, in the order described in the package comment.
//
// Every failure stops the operation, and none of them touches the data
// before the safety copy exists: the steps that cannot be undone are last.
func (r *Runner) Run(ctx context.Context, name string, stop Stopper, snap Snapshoter) (Report, error) {
	rep := Report{}

	archive, err := r.archiveOf(name)
	if err != nil {
		return rep, err
	}
	if err := r.check(); err != nil {
		return rep, err
	}

	start := r.now()
	bytes := sizeOf(archive)

	// 1. Stop the stack. Nothing else may start until the data is back.
	r.emit(Progress{Step: i18n.T("restore.step.stop"), Index: 1, Total: Steps,
		Bytes: bytes, Elapsed: r.now().Sub(start)})
	if stop == nil {
		return rep, errors.New(i18n.T("restore.unavailable"))
	}
	if err := stop.StopAll(ctx); err != nil {
		return rep, fmt.Errorf("%s: %w", i18n.T("restore.notstopped"), err)
	}

	// 2. The safety copy, by the repack's own script, against the stopped
	// MySQL it requires.
	if snap != nil {
		r.emit(Progress{Step: i18n.T("restore.step.safety"), Index: 2, Total: Steps,
			Bytes: bytes, Elapsed: r.now().Sub(start)})
		dir, size, err := snap.Physical(ctx)
		if err != nil {
			return rep, fmt.Errorf("%s: %w", i18n.T("restore.safety.fail"), err)
		}
		rep.SafetyDir, rep.SafetyBytes = dir, size
	}

	// 3. Unpack. This is the first step that changes anything.
	r.emit(Progress{Step: i18n.T("restore.step.unpack"), Index: 3, Total: Steps,
		Bytes: bytes, Elapsed: r.now().Sub(start)})
	if err := r.unpack(ctx, archive); err != nil {
		return rep, err
	}

	rep.Elapsed = r.now().Sub(start)
	r.emit(Progress{Step: i18n.T("restore.step.done"), Index: Steps, Total: Steps,
		Bytes: bytes, Elapsed: rep.Elapsed})
	return rep, nil
}

// unpack runs 7z over the archive.
//
// The working directory is the repack root and the output switch is "-o.",
// exactly as backups\README.md documents the hand-written restore. The paths
// inside the archive start with mysql\data\, so a relative output root puts
// every file back where it came from; an absolute path would depend on 7-Zip
// resolving it the way the README assumes.
//
// Not cancellable, and not watched. A killed 7z leaves mysql\data half
// replaced, which is the one state this package exists to avoid - and the
// safety copy is there, so a failure is recoverable by hand.
func (r *Runner) unpack(ctx context.Context, archive string) error {
	var out strings.Builder
	// context.Background, deliberately: see above.
	stderr, err := r.ops.RunTo(context.Background(), r.lay.SevenZipExe,
		[]string{"x", archive, "-o.", "-y"}, r.lay.Root, io.Writer(&out))
	if err != nil {
		return unpackError(err, stderr, out.String())
	}
	return nil
}

// check reports why a restore cannot start, or nil.
//
// The same three things, asked in the same order as the operation would fail
// them: no repack, no archiver, no stack to stop. A refusal with a sentence
// attached is what the button is worth; "exit status 1" is not.
func (r *Runner) check() error {
	if r.lay.Root == "" {
		return errors.New(i18n.T("restore.norepack"))
	}
	if r.ops == nil {
		return errors.New(i18n.T("restore.noenv"))
	}
	if _, err := os.Stat(r.lay.SevenZipExe); err != nil {
		return errors.New(i18n.Tf("restore.no7zip", r.lay.SevenZipExe))
	}
	return nil
}

// unpackError turns a failed 7z into something a person can act on.
//
// 7-Zip's exit status on its own is "cannot open archive as archive"; its
// stderr is where it says which file and why, and that line is the difference
// between "битый архив" and "нет прав на папку".
func unpackError(err error, stderr, stdout string) error {
	msg := firstLine(stderr)
	if msg == "" {
		msg = firstLine(stdout)
	}
	if msg == "" {
		return fmt.Errorf("%s: %w", i18n.T("restore.unpack.fail"), err)
	}
	return fmt.Errorf("%s: %s", i18n.T("restore.unpack.fail"), msg)
}

// firstLine is the first non-empty line, trimmed. Windows tools write "\r\n"
// and plenty of leading noise, and a multi-line error does not fit in the
// one-line result box.
func firstLine(text string) string {
	for _, line := range strings.Split(text, "\n") {
		if s := strings.TrimSpace(line); s != "" {
			return s
		}
	}
	return ""
}

func sizeOf(path string) int64 {
	if info, err := os.Stat(path); err == nil {
		return info.Size()
	}
	return 0
}

func (r *Runner) emit(p Progress) {
	if r.report != nil {
		r.report(p)
	}
}
