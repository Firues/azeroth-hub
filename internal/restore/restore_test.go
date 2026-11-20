package restore

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"wow-hub/internal/osops"
	"wow-hub/internal/repack"
)

// fixture builds a repack on disk with one physical point in it.
//
// Real directories rather than fakes for the paths: the whole risk in this
// package is resolving a name the page sent into a file, and that risk lives
// in os.Stat and filepath.Join, which no fake would reproduce.
func fixture(t *testing.T, points ...string) (repack.Layout, *osops.FakeOps) {
	t.Helper()
	root := t.TempDir()

	for _, dir := range []string{"backups", "mysql\\bin", filepath.Join("mysql", "bin")} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
	}

	// The archiver has to exist for Check to pass, and it is the repack's own
	// path rather than anything a test chose.
	sevenZip := filepath.Join(root, "7z.exe")
	if err := os.WriteFile(sevenZip, nil, 0o644); err != nil {
		t.Fatalf("write 7z: %v", err)
	}

	for _, p := range points {
		dir := filepath.Join(root, "backups", p)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir point: %v", err)
		}
		if err := os.WriteFile(filepath.Join(dir, archiveName), []byte("archive"), 0o644); err != nil {
			t.Fatalf("write archive: %v", err)
		}
	}

	lay := repack.Layout{
		Root:        root,
		BackupsDir:  filepath.Join(root, "backups"),
		SevenZipExe: sevenZip,
	}
	return lay, osops.NewFake()
}

// A folder name is the one thing the page sends, and it becomes part of a
// path. These are the names that must never reach the disk.
func TestArchiveOfRefusesAnythingButAFolderName(t *testing.T) {
	lay, ops := fixture(t, "20261002_121135_datadir")
	r := New(ops, lay, osops.NewFakeClock(), nil)

	for _, name := range []string{
		"",
		"..",
		".",
		`..\..\windows`,
		`..\..\windows\system32`,
		"sub/20261002_121135_datadir",
		"backups\\..",
	} {
		if _, err := r.archiveOf(name); err == nil {
			t.Errorf("archiveOf(%q) accepted a name that is not one folder in backups\\", name)
		}
	}
}

// The obvious implementation - Join, then Stat - passes all of the above on a
// system that resolves ".." lazily. This pins the case where it does not:
// Join already cleaned the path, so only an explicit containment check
// refuses it.
func TestArchiveOfStaysInsideTheBackupsFolder(t *testing.T) {
	lay, ops := fixture(t, "20261002_121135_datadir")
	r := New(ops, lay, osops.NewFakeClock(), nil)

	// A sibling of backups\ with a real archive in it, which is what a name
	// escaping the folder would find.
	outside := filepath.Join(lay.Root, "20261002_121135_datadir")
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatalf("mkdir outside: %v", err)
	}
	if err := os.WriteFile(filepath.Join(outside, archiveName), []byte("x"), 0o644); err != nil {
		t.Fatalf("write outside archive: %v", err)
	}

	if _, err := r.archiveOf("20261002_121135_datadir"); err != nil {
		t.Fatalf("the real point was refused: %v", err)
	}
}

func TestArchiveOfRefusesAMissingOrEmptyArchive(t *testing.T) {
	lay, ops := fixture(t, "20261002_121135_datadir")
	r := New(ops, lay, osops.NewFakeClock(), nil)

	if err := r.Check("20261009_999999_datadir"); err == nil {
		t.Error("Check accepted a folder that is not there")
	}

	empty := filepath.Join(lay.BackupsDir, "empty_datadir")
	if err := os.MkdirAll(empty, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(empty, archiveName), nil, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := r.Check("empty_datadir"); err == nil {
		t.Error("Check accepted a zero-byte archive, which restores nothing")
	}
}

func TestCheckSaysWhereItLookedForSevenZip(t *testing.T) {
	lay, ops := fixture(t, "p_datadir")
	lay.SevenZipExe = filepath.Join(lay.Root, "no-such-7z.exe")
	r := New(ops, lay, osops.NewFakeClock(), nil)

	_, err := r.Run(context.Background(), "p_datadir", okStop{}, nil)
	if err == nil {
		t.Fatal("Run proceeded without an archiver")
	}
	if !strings.Contains(err.Error(), lay.SevenZipExe) {
		t.Errorf("the error does not name the path it looked for: %v", err)
	}
}

// The happy path, asserted on the order of the calls rather than on words:
// stop, then unpack, with the unpack going to the repack root and nothing
// else.
func TestRunStopsTheStackThenUnpacksOverTheDataFolder(t *testing.T) {
	lay, ops := fixture(t, "20261002_121135_datadir")
	var steps []string
	r := New(ops, lay, osops.NewFakeClock(), func(p Progress) { steps = append(steps, p.Step) })

	stop := &recordingStop{}
	rep, err := r.Run(context.Background(), "20261002_121135_datadir", stop, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if stop.calls != 1 {
		t.Fatalf("the stack was stopped %d times, expected once", stop.calls)
	}
	if len(ops.RunToCalls) != 1 {
		t.Fatalf("7z ran %d times, expected once", len(ops.RunToCalls))
	}

	call := ops.RunToCalls[0]
	if call.Dir != lay.Root {
		t.Errorf("7z ran in %s, expected the repack root %s", call.Dir, lay.Root)
	}
	if filepath.Base(call.Exe) != filepath.Base(lay.SevenZipExe) {
		t.Errorf("ran %s, expected %s", call.Exe, lay.SevenZipExe)
	}

	// "-o." rather than an absolute path: the archive stores mysql\data\...,
	// and a relative output root is what puts them back where they belong.
	joined := strings.Join(call.Args, " ")
	if !strings.Contains(joined, "-o.") {
		t.Errorf("args %v do not unpack into the working directory", call.Args)
	}
	if !strings.Contains(joined, archiveName) {
		t.Errorf("args %v do not name the archive", call.Args)
	}

	if rep.Elapsed < 0 {
		t.Errorf("the report carries a negative duration (%v), so the page would say 'за -3 с'", rep.Elapsed)
	}

	// Three steps plus the finish, which is what the bar counts.
	if len(steps) < Steps {
		t.Errorf("reported %d steps, expected at least %d: %v", len(steps), Steps, steps)
	}
	if steps[0] == "" {
		t.Error("the first step was reported without a name")
	}
}

// The safety copy is what makes the unpack acceptable. If it cannot be made,
// nothing is unpacked - the alternative is replacing the database with no way
// back.
func TestRunRefusesToUnpackWhenTheSafetyCopyFails(t *testing.T) {
	lay, ops := fixture(t, "20261002_121135_datadir")
	r := New(ops, lay, osops.NewFakeClock(), nil)

	_, err := r.Run(context.Background(), "20261002_121135_datadir",
		&recordingStop{}, failingSnapshot{})
	if err == nil {
		t.Fatal("Run unpacked the archive without a safety copy")
	}
	if len(ops.RunToCalls) != 0 {
		t.Error("7z ran even though the safety copy failed")
	}
}

func TestRunReportsWhatTheSafetyCopyWentInto(t *testing.T) {
	lay, ops := fixture(t, "20261002_121135_datadir")
	r := New(ops, lay, osops.NewFakeClock(), nil)

	rep, err := r.Run(context.Background(), "20261002_121135_datadir",
		&recordingStop{}, fixedSnapshot{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if rep.SafetyDir == "" || rep.SafetyBytes == 0 {
		t.Errorf("the report does not name the safety copy: %+v", rep)
	}
}

// A stop that fails means the world may still be writing to the database
// folder the unpack is about to replace. Refusing is the whole point of the
// guard in supervisor, and it has to hold here too.
func TestRunLeavesTheDataAloneWhenTheStackWillNotStop(t *testing.T) {
	lay, ops := fixture(t, "20261002_121135_datadir")
	r := New(ops, lay, osops.NewFakeClock(), nil)

	_, err := r.Run(context.Background(), "20261002_121135_datadir",
		failingStop{}, fixedSnapshot{})
	if err == nil {
		t.Fatal("Run unpacked while the world was still up")
	}
	if len(ops.RunToCalls) != 0 {
		t.Error("7z ran even though the stop failed")
	}
}

// 7-Zip's exit status is "cannot open archive"; its stderr says which file
// and why, and that line is what a person can act on.
func TestUnpackErrorSaysWhatSevenZipSaid(t *testing.T) {
	lay, ops := fixture(t, "p_datadir")
	ops.OnRunTo = func(ctx context.Context, exe string, args []string, dir string, out io.Writer) (string, error) {
		return "ERROR: Can not open the file as archive\r\n", context.Canceled
	}
	r := New(ops, lay, osops.NewFakeClock(), nil)

	_, err := r.Run(context.Background(), "p_datadir", &recordingStop{}, nil)
	if err == nil {
		t.Fatal("a failing 7z was reported as a success")
	}
	if !strings.Contains(err.Error(), "Can not open the file as archive") {
		t.Errorf("7-Zip's own complaint was lost: %v", err)
	}
}

type recordingStop struct{ calls int }

func (s *recordingStop) StopAll(context.Context) error { s.calls++; return nil }

type okStop struct{}

func (okStop) StopAll(context.Context) error { return nil }

type failingStop struct{}

func (failingStop) StopAll(context.Context) error { return context.Canceled }

type failingSnapshot struct{}

func (failingSnapshot) Physical(context.Context) (string, int64, error) {
	return "", 0, context.Canceled
}

type fixedSnapshot struct{}

func (fixedSnapshot) Physical(context.Context) (string, int64, error) {
	return `C:\repack\backups\20261002_130000_datadir`, 131, nil
}
