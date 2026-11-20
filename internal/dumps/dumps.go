// Package dumps creates rollback points under backups\.
//
// Two kinds, matching the two the repack's own scripts make:
//
//	logical   four per-database dumps, made by mysql\bin\mysqldump.exe
//	          against a RUNNING MySQL. Portable and readable, and
//	          restorable with backups\restore-all-db.cmd.
//	physical  mysql\data archived into one .7z by
//	          backups\snapshot-mysql-data.cmd, against a STOPPED MySQL.
//	          Restores everything at once, InnoDB dictionary included.
//
// The formats are not invented here. A logical point is exactly the four
// files restore-all-db.cmd looks for, in the folder layout it expects, made
// with the flags backups\backup-table.cmd uses; a physical point is made by
// running the repack's own script rather than reimplementing it. The
// alternative - a second, app-specific backup format - would produce two
// sets of files that look alike and are not interchangeable, and the day
// someone needs a rollback is the worst possible day to find that out.
//
// Package backups reads what this writes.
package dumps

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"wow-hub/internal/i18n"
	"wow-hub/internal/osops"
	"wow-hub/internal/repack"
)

// Kind is which sort of rollback point is being made.
type Kind string

const (
	// KindLogical is the four per-database dumps.
	KindLogical Kind = "logical"
	// KindPhysical is mysql\data as a single archive.
	KindPhysical Kind = "physical"
)

// Names this package and the repack's scripts agree on.
const (
	// LogicalSuffix ends a folder of per-database dumps, as in
	// 2026-09-29_bots_current. The scripts put it there; matching it keeps
	// the app's points reading the same way as the hand-made ones.
	LogicalSuffix = "_current"
	// PhysicalSuffix ends a folder holding mysql_data.7z. The script
	// builds this name itself.
	PhysicalSuffix = "_datadir"
	// ArchiveName is the file the physical script writes.
	ArchiveName = "mysql_data.7z"
	// snapshotScript is the repack's own physical-snapshot script.
	snapshotScript = "snapshot-mysql-data.cmd"
)

// stampLayout is the folder-name timestamp.
//
// Chosen to match what snapshot-mysql-data.cmd already writes
// (20260930_075446_datadir), and read back by backups.describe's
// stampWithTime, so a point made here dates and sorts correctly in the list
// without any change to that code. A human-readable 2026-09-30_143000 would
// not: that parser takes the first fifteen characters, and a name with
// dashes in it parses as a bare date, which would place every point at
// midnight on the day it was made.
const stampLayout = "20060102_150405"

// dumpTrailer is the line mysqldump writes after the last statement.
//
// The only completeness signal a dump file carries, and the reason
// verifyDump exists: a file without it was cut short by a full disk, a
// killed process or a server that went away, and a truncated dump restores
// without complaint into a database missing its last tables.
const dumpTrailer = "-- Dump completed on"

// progressEvery is how much a dump may grow between two progress reports.
//
// By volume, not by time, so a report does not depend on a clock. A 329 MB
// dump produces about forty reports instead of the tens of thousands one
// per write would, and a test can predict exactly when they come.
const progressEvery = 8 << 20

// tailBytes is how much of a dump's end is read to look for the trailer.
// The trailer is one short line, so this is generous.
const tailBytes = 4096

// Progress is one step of a backup, reported while it runs.
//
// Reported at all because a backup is not instant: measured on this repack,
// the four dumps take 105 seconds and 385 MB between them, and the physical
// snapshot another 31 seconds. A button that shows nothing for two minutes
// is a button people press twice.
type Progress struct {
	Kind Kind
	// Step is what is happening now: a schema name, or the archive.
	Step string
	// Index and Total place that step in the whole job, 1-based.
	Index int
	Total int
	// Bytes is how much has been written for the current step.
	Bytes int64
	// Elapsed is the time since the whole job started.
	Elapsed time.Duration
}

// Report is the outcome of a backup that finished.
type Report struct {
	Kind Kind
	// Dir is the folder that was created, as an absolute path.
	Dir string
	// Databases are the dumps written. Logical only.
	Databases []string
	// Bytes is everything written, all steps together.
	Bytes   int64
	Elapsed time.Duration
	// Warnings are things that are not failures but that a person should
	// see: output on mysqldump's stderr that came with exit status 0.
	Warnings []string
}

// Runner makes rollback points.
//
// A struct rather than free functions because both operations share four
// collaborators, and every one of them is injected. The Ops is what makes
// this testable without mysqldump, the Layout is what keeps paths from
// being retyped, and the Clock is what makes a folder name assertable.
type Runner struct {
	ops    osops.Ops
	lay    repack.Layout
	clock  osops.Clock
	report func(Progress)
}

// New builds a Runner. clock may be nil, and report may be nil.
func New(ops osops.Ops, lay repack.Layout, clock osops.Clock, report func(Progress)) *Runner {
	if clock == nil {
		clock = osops.RealClock{}
	}
	return &Runner{ops: ops, lay: lay, clock: clock, report: report}
}

// Logical writes one dump per database into a new folder under backups\.
//
// The folder is created before the first dump and deleted again if any of
// them fails, so what is on disk afterwards is either a complete rollback
// point or nothing at all. See cleanup for why the partial case is not left
// for the user to sort out.
func (r *Runner) Logical(ctx context.Context) (Report, error) {
	rep := Report{Kind: KindLogical}
	start := r.now()

	if err := r.checkLogical(); err != nil {
		return rep, err
	}

	dir, err := r.makeLogicalDir()
	if err != nil {
		return rep, err
	}
	rep.Dir = dir

	total := len(repack.Databases)
	for i, db := range repack.Databases {
		if ctx.Err() != nil {
			r.cleanup(dir, KindLogical)
			return Report{Kind: KindLogical}, errors.New(i18n.T("dump.cancelled"))
		}

		path := filepath.Join(dir, db+".sql")
		size, stderr, derr := r.dumpOne(ctx, db, path, i+1, total, start)
		if derr != nil {
			r.cleanup(dir, KindLogical)
			return Report{Kind: KindLogical}, derr
		}

		rep.Databases = append(rep.Databases, db)
		rep.Bytes += size
		if stderr != "" {
			rep.Warnings = append(rep.Warnings, fmt.Sprintf("%s: %s", db, oneLine(stderr)))
		}

		// Emitted after every database, not only every 8 MiB: the auth
		// dump is 90 KB, so a volume-triggered report alone would leave
		// the bar motionless for the whole of it.
		r.emit(Progress{
			Kind: KindLogical, Step: db, Index: i + 1, Total: total,
			Bytes: size, Elapsed: time.Since(start),
		})
	}

	rep.Elapsed = time.Since(start)
	return rep, nil
}

// dumpOne runs mysqldump for one schema into path and returns its size.
func (r *Runner) dumpOne(ctx context.Context, db, path string, index, total int, start time.Time) (int64, string, error) {
	f, err := os.Create(path)
	if err != nil {
		return 0, "", fmt.Errorf("%s: %w", i18n.Tf("dump.create.fail", path), err)
	}
	defer f.Close()

	cw := &countingWriter{
		w:     f,
		every: progressEvery,
		next:  progressEvery,
		on: func(n int64) {
			r.emit(Progress{
				Kind: KindLogical, Step: db, Index: index, Total: total,
				Bytes: n, Elapsed: time.Since(start),
			})
		},
	}

	// The working directory is the repack root rather than whatever
	// directory the window was launched from. --defaults-file means no
	// option file is searched for, so this is about predictability
	// rather than about finding the right config.
	stderr, runErr := r.ops.RunTo(ctx, r.lay.MySQLdumpExe, dumpArgs(r.lay, db), r.lay.Root, cw)
	if err := f.Sync(); err != nil {
		return 0, stderr, fmt.Errorf("%s: %w", i18n.Tf("dump.write.fail", path), err)
	}

	if runErr != nil {
		if ctx.Err() != nil {
			return 0, stderr, errors.New(i18n.T("dump.cancelled"))
		}
		// stderr is where mysqldump says what went wrong, and an exit
		// status on its own is not something a user can act on.
		if msg := oneLine(stderr); msg != "" {
			return 0, stderr, errors.New(i18n.Tf("dump.fail.msg", db, msg))
		}
		return 0, stderr, fmt.Errorf("%s: %w", i18n.Tf("dump.fail", db), runErr)
	}

	if err := verifyDump(path); err != nil {
		return 0, stderr, err
	}

	info, err := os.Stat(path)
	if err != nil {
		return 0, stderr, fmt.Errorf("%s: %w", i18n.Tf("dump.read.fail", path), err)
	}
	return info.Size(), stderr, nil
}

// Physical archives mysql\data by running the repack's own script.
//
// The script owns the format, the exclusions (binlog.*), the compression
// level and the refusal to run against a live database. Running it rather
// than reimplementing it means a point made from this window is
// indistinguishable from one made by hand, which is what the README's
// rollback instructions assume.
func (r *Runner) Physical(ctx context.Context) (Report, error) {
	rep := Report{Kind: KindPhysical}
	start := r.now()

	if err := r.checkPhysical(); err != nil {
		return rep, err
	}
	if ctx.Err() != nil {
		return rep, errors.New(i18n.T("dump.cancelled"))
	}

	before := datadirFolders(r.lay.BackupsDir)

	// Deliberately not ctx. Killing cmd.exe does not kill the 7-Zip it
	// started: the archiver would keep running, writing into a folder this
	// call then reports as failed and deletes underneath it. Cancellation
	// is honoured before the script starts and not during, and the 31
	// seconds it takes is short enough to wait out.
	var out bytes.Buffer
	stderr, err := r.ops.RunTo(context.Background(), osops.CmdExe(),
		[]string{"/c", snapshotScript}, r.lay.BackupsDir, &out)

	// The script names its own folder - it takes the timestamp from
	// PowerShell - so the only way to learn which one it made is to look
	// before and after. More robust than parsing its console output, which
	// is written for a person and has already changed once.
	dir := newestNewDatadir(r.lay.BackupsDir, before)

	if err != nil {
		r.cleanup(dir, KindPhysical)
		return Report{Kind: KindPhysical}, physicalError(err, stderr, out.String())
	}

	if dir == "" {
		return rep, errors.New(i18n.Tf("dump.phys.nodir", PhysicalSuffix, r.lay.BackupsDir))
	}
	rep.Dir = dir

	archive := filepath.Join(dir, ArchiveName)
	info, serr := os.Stat(archive)
	if serr != nil {
		r.cleanup(dir, KindPhysical)
		return Report{Kind: KindPhysical}, fmt.Errorf("%s: %w", i18n.Tf("dump.phys.noarchive", archive), serr)
	}
	if info.Size() == 0 {
		r.cleanup(dir, KindPhysical)
		return Report{Kind: KindPhysical}, errors.New(i18n.Tf("dump.phys.empty", archive))
	}

	rep.Bytes = info.Size()
	rep.Elapsed = time.Since(start)
	if stderr != "" {
		rep.Warnings = append(rep.Warnings, oneLine(stderr))
	}
	r.emit(Progress{
		Kind: KindPhysical, Step: ArchiveName, Index: 1, Total: 1,
		Bytes: rep.Bytes, Elapsed: rep.Elapsed,
	})
	return rep, nil
}

// checkLogical reports why a logical dump cannot be made, or nil.
//
// Every check is one the dump would otherwise fail on partway through, with
// three of the four databases already written and 385 MB of a disk that may
// not have room for them.
func (r *Runner) checkLogical() error {
	if r.lay.Root == "" {
		return errors.New(i18n.T("dump.norepack"))
	}
	if _, err := os.Stat(r.lay.MySQLdumpExe); err != nil {
		return errors.New(i18n.Tf("dump.nomysqldump", r.lay.MySQLdumpExe))
	}
	if _, err := os.Stat(r.lay.MySQLPassConf); err != nil {
		return errors.New(i18n.Tf("dump.nopassconf", r.lay.MySQLPassConf))
	}
	if _, err := os.Stat(r.lay.BackupsDir); err != nil {
		return fmt.Errorf("%s: %w", i18n.T("dump.backupsdir.fail"), err)
	}
	// The direct question, rather than asking the supervisor: a dump needs
	// something to connect to, and a port that accepts connections is
	// exactly that.
	if !r.ops.Listen(repack.MySQLPort) {
		return errors.New(i18n.T("dump.mysql.required"))
	}
	return nil
}

// checkPhysical reports why a physical snapshot cannot be made, or nil.
//
// The script checks the same two processes itself. Doing it here as well is
// not redundant: the script's refusal arrives as one line on a stdout we
// captured, while this arrives as a sentence next to the button.
func (r *Runner) checkPhysical() error {
	if r.lay.Root == "" {
		return errors.New(i18n.T("dump.norepack"))
	}
	if _, err := os.Stat(filepath.Join(r.lay.BackupsDir, snapshotScript)); err != nil {
		return errors.New(i18n.Tf("dump.noscript", snapshotScript, r.lay.BackupsDir))
	}
	if r.anyRunning("mysqld.exe") {
		return errors.New(i18n.T("dump.mysql.off"))
	}
	if r.anyRunning("worldserver.exe") {
		return errors.New(i18n.T("dump.world.off"))
	}
	return nil
}

// anyRunning reports whether any process with this image name is alive.
//
// A failure to read the process list counts as "not running": refusing to
// take a backup because tasklist could not be read would be the wrong way
// round, and the script checks again anyway.
func (r *Runner) anyRunning(image string) bool {
	pids, err := r.ops.FindByName(image)
	return err == nil && len(pids) > 0
}

// makeLogicalDir creates the folder for a logical point and returns it.
//
// os.Mkdir, not MkdirAll, and the difference matters: Mkdir fails when the
// name is taken, so a folder handed back from here is always one this call
// created. cleanup deletes whatever is in that folder, and a folder that
// already existed must never reach it - that would delete somebody's
// earlier rollback point.
func (r *Runner) makeLogicalDir() (string, error) {
	base := r.lay.BackupsDir
	stamp := r.now().Format(stampLayout)

	// A second backup inside the same second would collide. The counter
	// keeps the name usable rather than refusing to work, and does not
	// disturb the timestamp: backups.describe reads the first fifteen
	// characters and ignores the rest.
	//
	// The counter goes before the suffix, not after it, so the name still
	// ENDS with _current. removable keys off that suffix, and a folder it
	// refused to recognise would be one cleanup could not delete.
	for n := 1; n <= 100; n++ {
		name := stamp + LogicalSuffix
		if n > 1 {
			name = fmt.Sprintf("%s_%d%s", stamp, n, LogicalSuffix)
		}
		dir := filepath.Join(base, name)

		err := os.Mkdir(dir, 0o755)
		if err == nil {
			return dir, nil
		}
		if !os.IsExist(err) {
			return "", fmt.Errorf("%s: %w", i18n.Tf("dump.create.fail", dir), err)
		}
	}
	return "", errors.New(i18n.Tf("dump.names.spent", base, stamp))
}

// cleanup removes a folder this run created.
//
// A partial set of dumps is worse than none at all. backups\restore-all-db.cmd
// picks the newest folder that holds acore_world.sql and then skips any
// database whose file is missing, so a folder with three of the four would
// import three, leave the fourth at its current state, and report success -
// a realm whose world and characters came from different days. Deleting is
// also what gives the space back when the failure was a full disk.
//
// Guarded by removable, because this is the one place the app deletes
// something it did not write in this very call.
func (r *Runner) cleanup(dir string, kind Kind) {
	if !removable(dir, r.lay.BackupsDir, kind) {
		return
	}
	_ = os.RemoveAll(dir)
}

// removable reports whether dir is a folder this package may delete: one
// sitting directly under the backups directory whose name carries the
// suffix for the kind being made.
func removable(dir, backupsDir string, kind Kind) bool {
	if dir == "" || backupsDir == "" {
		return false
	}
	if !strings.EqualFold(filepath.Clean(filepath.Dir(dir)), filepath.Clean(backupsDir)) {
		return false
	}

	suffix := LogicalSuffix
	if kind == KindPhysical {
		suffix = PhysicalSuffix
	}
	name := filepath.Base(dir)
	return len(name) > len(suffix) && strings.HasSuffix(strings.ToLower(name), strings.ToLower(suffix))
}

// dumpArgs is the mysqldump command line for one schema.
//
// The flags are backups\backup-table.cmd's - this repack's own idea of a
// correct dump - plus --routines, which the full dumps already in backups\
// were made with. This install has no routines, so the section is an empty
// header either way; on an install that had some, a dump made without the
// flag would lose them silently.
//
//	--single-transaction   one consistent snapshot without stopping the
//	                       server. This is what makes a dump of a live
//	                       database safe, and why the logical backup wants
//	                       MySQL running rather than stopped.
//	--skip-lock-tables     implied by --single-transaction; said out loud
//	                       because the two are documented as mutually
//	                       exclusive and the default is to lock.
//	--no-tablespaces       the working account has no PROCESS privilege,
//	                       and without this mysqldump writes "Access denied
//	                       ... PROCESS privilege" to stderr and still
//	                       exits 0 - a warning that reads like a failure.
//	--default-character-set=utf8mb4
//	                       the databases are utf8mb4. Left to negotiate,
//	                       the server default is used and a comment with a
//	                       stray byte comes back mangled.
//
// No --databases: the schema is named positionally, so the dump carries no
// CREATE DATABASE and no USE, and imports into whatever schema
// restore-all-db.cmd names on its own command line. That is what makes
// these four files interchangeable with the ones already in backups\.
//
// --defaults-file must come first; mysqldump rejects it anywhere else. The
// password is not on this command line at all - it comes out of that file,
// which is the whole reason the file exists.
func dumpArgs(lay repack.Layout, db string) []string {
	return []string{
		"--defaults-file=" + lay.MySQLPassConf,
		"-u", repack.MySQLUser(lay.MySQLPassConf),
		"--single-transaction",
		"--skip-lock-tables",
		"--no-tablespaces",
		"--routines",
		"--default-character-set=utf8mb4",
		db,
	}
}

// verifyDump checks that a dump file ends the way mysqldump ends one.
func verifyDump(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("%s: %w", i18n.Tf("dump.tail.read.fail", path), err)
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return fmt.Errorf("%s: %w", i18n.Tf("dump.tail.read.fail", path), err)
	}
	if info.Size() == 0 {
		return errors.New(i18n.Tf("dump.empty", path))
	}

	n := int64(tailBytes)
	if info.Size() < n {
		n = info.Size()
	}
	buf := make([]byte, n)
	if _, err := f.ReadAt(buf, info.Size()-n); err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("%s: %w", i18n.Tf("dump.tail.end.fail", path), err)
	}

	// The last non-empty line, not the whole tail: a dump of a table whose
	// data happened to contain the trailer text would otherwise pass while
	// being truncated.
	lines := strings.Split(strings.TrimRight(string(buf), "\r\n \t"), "\n")
	if !strings.HasPrefix(strings.TrimSpace(lines[len(lines)-1]), dumpTrailer) {
		return errors.New(i18n.Tf("dump.truncated", filepath.Base(path), dumpTrailer))
	}
	return nil
}

// datadirFolders lists the *_datadir folders under dir, as a set.
func datadirFolders(dir string) map[string]bool {
	out := map[string]bool{}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return out
	}
	for _, e := range entries {
		if e.IsDir() && strings.HasSuffix(strings.ToLower(e.Name()), PhysicalSuffix) {
			out[e.Name()] = true
		}
	}
	return out
}

// newestNewDatadir returns the path of the newest *_datadir folder that was
// not present in before, or "" when there is none.
func newestNewDatadir(dir string, before map[string]bool) string {
	var fresh []string
	for name := range datadirFolders(dir) {
		if !before[name] {
			fresh = append(fresh, name)
		}
	}
	if len(fresh) == 0 {
		return ""
	}
	// The name starts with a timestamp, so the lexicographically greatest
	// is the most recent. Sorted rather than taken from the map, whose
	// order is random, so two new folders could not be told apart.
	sort.Strings(fresh)
	return filepath.Join(dir, fresh[len(fresh)-1])
}

// physicalError explains a failed snapshot.
//
// The script's own output is included verbatim. That is where its reason
// lives - it refuses a live MySQL, a missing mysql\data and a missing
// 7-Zip, and each of those prints a sentence a person can act on - and
// rewriting it in Russian here would be a second copy of a fact that
// already exists in one place. Without it the error is an exit status.
func physicalError(err error, stderr, stdout string) error {
	if why := firstLineWith(stdout, "ERROR"); why != "" {
		return errors.New(i18n.Tf("dump.snapshot.fail.msg", why))
	}
	if why := oneLine(stderr); why != "" {
		return errors.New(i18n.Tf("dump.snapshot.fail.msg", why))
	}
	return fmt.Errorf("%s: %w", i18n.T("dump.snapshot.fail"), err)
}

// firstLineWith returns the first line containing want, trimmed.
func firstLineWith(text, want string) string {
	for _, line := range strings.Split(text, "\n") {
		if line = strings.TrimSpace(line); strings.Contains(line, want) {
			return line
		}
	}
	return ""
}

// oneLine flattens a multi-line message into its first non-empty line.
//
// mysqldump's stderr is a sentence and occasionally a paragraph; the whole
// thing belongs in the log, and one line belongs in the window.
func oneLine(text string) string {
	for _, line := range strings.Split(text, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			return line
		}
	}
	return ""
}

// emit reports progress, if anyone is listening.
func (r *Runner) emit(p Progress) {
	if r.report != nil {
		r.report(p)
	}
}

func (r *Runner) now() time.Time { return r.clock.Now() }

// countingWriter reports how much has been written, once per every bytes.
//
// Throttled by volume rather than by time so the reports do not depend on a
// clock: with a fake clock a time-based throttle would fire once and then
// never, and a test could not tell a working reporter from a silent one.
type countingWriter struct {
	w     io.Writer
	every int64
	next  int64
	n     int64
	on    func(int64)
}

func (c *countingWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.n += int64(n)
	if c.n >= c.next {
		c.next = c.n + c.every
		if c.on != nil {
			c.on(c.n)
		}
	}
	return n, err
}
