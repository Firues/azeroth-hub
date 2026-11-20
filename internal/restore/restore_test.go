package restore

import (
	"archive/zip"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"wow-hub/internal/i18n"
	"wow-hub/internal/osops"
	"wow-hub/internal/repack"
)

func TestMain(m *testing.M) {
	// The assertions pin exact Russian wording; the process-wide language
	// defaults to English now, so the tests pin theirs explicitly.
	i18n.Set(i18n.Ru)
	os.Exit(m.Run())
}

// pointEntries is what a test point's archive holds: the shape of a real
// mysql\data snapshot, small enough to build in every fixture.
var pointEntries = map[string]string{
	"mysql/data/ibdata1":                   "new ibdata1",
	"mysql/data/acore_world/character.ibd": "new character.ibd",
}

// fixture builds a repack on disk with one physical point in it.
//
// Real directories rather than fakes for the paths: the whole risk in this
// package is resolving a name the page sent into a file, and that risk lives
// in os.Stat and filepath.Join, which no fake would reproduce. The archiver
// is in the exe now, so the unpack below is a real unpack - which is why the
// fixture also puts a live mysql\data on disk, for the assertions to tell
// "the archive went back over it" from "nothing happened".
func fixture(t *testing.T, points ...string) (repack.Layout, *osops.FakeOps) {
	t.Helper()
	root := t.TempDir()

	for _, dir := range []string{"backups", filepath.Join("mysql", "bin"), filepath.Join("mysql", "data")} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "mysql", "data", "ibdata1"), []byte("live ibdata1"), 0o644); err != nil {
		t.Fatalf("write live data: %v", err)
	}

	for _, p := range points {
		dir := filepath.Join(root, "backups", p)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir point: %v", err)
		}
		writePointZip(t, dir, pointEntries)
	}

	lay := repack.Layout{
		Root:       root,
		BackupsDir: filepath.Join(root, "backups"),
	}
	return lay, osops.NewFake()
}

// writePointZip writes a real mysql_data.zip into dir, holding the named
// entries.
func writePointZip(t *testing.T, dir string, entries map[string]string) {
	t.Helper()
	f, err := os.Create(filepath.Join(dir, archiveName))
	if err != nil {
		t.Fatalf("create archive: %v", err)
	}
	zw := zip.NewWriter(f)
	for name, body := range entries {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("zip entry %s: %v", name, err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatalf("zip entry %s: %v", name, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close archive: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close file: %v", err)
	}
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
	writePointZip(t, outside, pointEntries)

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

// The happy path, asserted on the result on disk rather than on the calls:
// the stack is stopped, and the archive is back over mysql\data with the
// entries it carried.
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
	// Nothing external runs: the archiver is in the exe.
	if len(ops.RunToCalls) != 0 {
		t.Errorf("an external process was started, though the archiver is built in: %v", ops.RunToCalls)
	}

	// The archive's entries are back where they came from, over the live
	// data the fixture put there.
	body, err := os.ReadFile(filepath.Join(lay.Root, "mysql", "data", "ibdata1"))
	if err != nil {
		t.Fatalf("restored ibdata1: %v", err)
	}
	if string(body) != pointEntries["mysql/data/ibdata1"] {
		t.Errorf("ibdata1 = %q, want the archived content", body)
	}
	nested := filepath.Join(lay.Root, "mysql", "data", "acore_world", "character.ibd")
	if body, err := os.ReadFile(nested); err != nil || string(body) != pointEntries["mysql/data/acore_world/character.ibd"] {
		t.Errorf("nested character.ibd: %q, %v", body, err)
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
	if body, rerr := os.ReadFile(filepath.Join(lay.Root, "mysql", "data", "ibdata1")); rerr != nil || string(body) != "live ibdata1" {
		t.Errorf("the archive went back over the data despite the failed safety copy: %q, %v", body, rerr)
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
	if body, rerr := os.ReadFile(filepath.Join(lay.Root, "mysql", "data", "ibdata1")); rerr != nil || string(body) != "live ibdata1" {
		t.Errorf("the archive went back over the data despite the failed stop: %q, %v", body, rerr)
	}
}

// An archive that is not a zip at all - a truncated download, a file renamed
// by hand - must fail with a sentence, not with a half-replaced data folder.
func TestUnpackRefusesACorruptArchive(t *testing.T) {
	lay, ops := fixture(t)
	r := New(ops, lay, osops.NewFakeClock(), nil)

	point := filepath.Join(lay.BackupsDir, "broken_datadir")
	if err := os.MkdirAll(point, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(point, archiveName), []byte("this is not a zip"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := r.Run(context.Background(), "broken_datadir", &recordingStop{}, nil)
	if err == nil {
		t.Fatal("Run accepted an archive that is not a zip")
	}
	if !strings.Contains(err.Error(), "распаковка") {
		t.Errorf("the error does not say the unpacking failed: %v", err)
	}
	// The corrupt archive never got as far as replacing anything.
	if body, rerr := os.ReadFile(filepath.Join(lay.Root, "mysql", "data", "ibdata1")); rerr != nil || string(body) != "live ibdata1" {
		t.Errorf("the corrupt archive touched the data folder: %q, %v", body, rerr)
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
