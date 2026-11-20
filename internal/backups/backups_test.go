package backups

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"wow-hub/internal/osops"
)

// write makes a file of the given size and modification time.
//
// The time matters: for a folder whose name carries only a date, the newest
// file inside is the only honest answer to "when was this taken", so a test
// has to be able to place a file at an exact moment.
func write(t *testing.T, path string, size int, mod time.Time) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, make([]byte, size), 0o644); err != nil {
		t.Fatalf("WriteFile(%s): %v", path, err)
	}
	if !mod.IsZero() {
		if err := os.Chtimes(path, mod, mod); err != nil {
			t.Fatalf("Chtimes(%s): %v", path, err)
		}
	}
}

func find(t *testing.T, inv Inventory, name string) Point {
	t.Helper()
	for _, p := range inv.Points {
		if p.Name == name {
			return p
		}
	}
	t.Fatalf("point %q not in the inventory (%d points)", name, len(inv.Points))
	return Point{}
}

// The four shapes the repack's scripts actually produce, plus a folder that
// is none of them.
func TestScanRecognisesEveryShapeTheScriptsWrite(t *testing.T) {
	dir := t.TempDir()

	// snapshot-mysql-data.cmd
	write(t, filepath.Join(dir, "20260930_075446_datadir", "mysql_data.7z"), 1000, time.Time{})
	// restore-all-db.cmd / the *_current folders
	for _, db := range []string{"acore_world", "acore_characters", "acore_playerbots", "acore_auth"} {
		write(t, filepath.Join(dir, "2026-09-29_bots_current", db+".sql"), 100, time.Time{})
	}
	// backup-table.cmd
	write(t, filepath.Join(dir, "2026-09-29", "game_event_20260929_091154.sql"), 50, time.Time{})
	// someone's own folder
	write(t, filepath.Join(dir, "my copy", "notes.txt"), 10, time.Time{})

	inv := Scan(osops.NewFake(), dir)
	if inv.Err != "" {
		t.Fatalf("unexpected Err: %q", inv.Err)
	}
	if len(inv.Points) != 4 {
		t.Fatalf("want 4 points, got %d", len(inv.Points))
	}

	cases := []struct {
		name string
		kind Kind
	}{
		{"20260930_075446_datadir", KindPhysical},
		{"2026-09-29_bots_current", KindLogical},
		{"2026-09-29", KindTables},
		{"my copy", KindOther},
	}
	for _, c := range cases {
		if got := find(t, inv, c.name).Kind; got != c.kind {
			t.Errorf("%s: kind = %q, want %q", c.name, got, c.kind)
		}
	}

	// The whole reason this screen exists: you can see what is in a dump
	// without unpacking it.
	if got := find(t, inv, "2026-09-29_bots_current").Databases; len(got) != 4 {
		t.Errorf("logical point lists %v, want all four databases", got)
	}
	if got := find(t, inv, "my copy").Note; got == "" {
		t.Error("a folder that is not a rollback point must say so, not look like an empty backup")
	}
}

// A logical dump missing a database is the dangerous case: it looks
// complete, restores without complaint, and leaves one schema at whatever
// the server has now. It must be visible in the list.
func TestScanNamesTheDatabaseMissingFromALogicalDump(t *testing.T) {
	dir := t.TempDir()
	for _, db := range []string{"acore_world", "acore_characters", "acore_playerbots"} {
		write(t, filepath.Join(dir, "2026-09-29_partial", db+".sql"), 100, time.Time{})
	}

	p := find(t, Scan(osops.NewFake(), dir), "2026-09-29_partial")
	if p.Kind != KindLogical {
		t.Fatalf("kind = %q, want logical", p.Kind)
	}
	if !strings.Contains(p.Note, "acore_auth") {
		t.Errorf("note %q does not name the missing database", p.Note)
	}
}

// A name with a time is the moment of the snapshot; a name with only a date
// is not, and the newest file inside is closer to the truth than midnight.
func TestScanPrefersTheTimeInTheNameAndFallsBackToTheNewestFile(t *testing.T) {
	dir := t.TempDir()

	write(t, filepath.Join(dir, "20260930_075446_datadir", "mysql_data.7z"), 10,
		time.Date(2026, 9, 30, 23, 0, 0, 0, time.Local))

	// The name says only 29 September. The dump was written at 12:47.
	write(t, filepath.Join(dir, "2026-09-29_bots_current", "acore_world.sql"), 10,
		time.Date(2026, 9, 29, 12, 47, 0, 0, time.Local))

	inv := Scan(osops.NewFake(), dir)

	if got := find(t, inv, "20260930_075446_datadir").Created; !got.Equal(time.Date(2026, 9, 30, 7, 54, 46, 0, time.Local)) {
		t.Errorf("physical point created = %v, want the time from the folder name", got)
	}
	if got := find(t, inv, "2026-09-29_bots_current").Created; !got.Equal(time.Date(2026, 9, 29, 12, 47, 0, 0, time.Local)) {
		t.Errorf("logical point created = %v, want the newest file inside", got)
	}

	// Newest first, which is the order the screen shows.
	if inv.Points[0].Name != "20260930_075446_datadir" {
		t.Errorf("first point is %q, want the newest", inv.Points[0].Name)
	}
}

// An unreadable folder and an empty one mean opposite things, and the
// screen must be able to tell them apart.
func TestScanReportsAMissingFolderInsteadOfAnEmptyList(t *testing.T) {
	inv := Scan(osops.NewFake(), filepath.Join(t.TempDir(), "nope"))

	if inv.Err == "" {
		t.Fatal("a missing backups folder must set Err, not read as 'no backups'")
	}
	if len(inv.Points) != 0 {
		t.Errorf("want no points, got %d", len(inv.Points))
	}
}

func TestScanSumsSizesAndReportsDiskSpace(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "a_datadir", "mysql_data.7z"), 300, time.Time{})
	write(t, filepath.Join(dir, "b_current", "acore_world.sql"), 200, time.Time{})

	ops := osops.NewFake()
	ops.DiskFreeBytes = 5_000
	ops.DiskTotalBytes = 10_000

	inv := Scan(ops, dir)
	if inv.TotalBytes != 500 {
		t.Errorf("TotalBytes = %d, want 500", inv.TotalBytes)
	}
	if inv.FreeBytes != 5_000 || inv.TotalDiskBytes != 10_000 {
		t.Errorf("disk = %d/%d, want 5000/10000", inv.FreeBytes, inv.TotalDiskBytes)
	}
}

func TestResolveReturnsTheFolderInsideBackups(t *testing.T) {
	dir := t.TempDir()
	want := filepath.Join(dir, "20260930_075446_datadir")
	if err := os.MkdirAll(want, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	got, err := Resolve(dir, "20260930_075446_datadir")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got != want {
		t.Errorf("Resolve = %q, want %q", got, want)
	}
}

// The name arrives from the page, and a path built from it is a path the page
// chose. Every refusal here is a way the folder could leave backups\ - and the
// callers are a delete and an unpack, so the damage is not undoable.
func TestResolveRefusesAnythingThatIsNotAPlainChildName(t *testing.T) {
	dir := t.TempDir()
	outside := filepath.Join(dir, "..", "outside")
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	write(t, filepath.Join(dir, "2026-09-29", "acore_world.sql"), 10, time.Time{})
	write(t, filepath.Join(dir, "loose.sql"), 10, time.Time{})

	for _, name := range []string{
		"",                   // not a folder at all
		".",                  // backups\ itself
		"..",                 // the folder above it
		`..\outside`,         // a sibling of backups\
		"../outside",         // the same, with the other separator
		"2026-09-29\\nested", // a path rather than a name
		"2026-09-29/nested",  // the same, with the other separator
		"nothing_here",       // plain name, but no such folder
		"loose.sql",          // a file, not a rollback point
	} {
		got, err := Resolve(dir, name)
		if err == nil {
			t.Errorf("Resolve(%q) = %q, want a refusal", name, got)
		}
	}
}

// The guard has to hold on Windows, where C:\Backups and c:\backups are one
// folder and a string comparison says otherwise.
func TestResolveComparesCaseInsensitively(t *testing.T) {
	dir := t.TempDir()
	name := "MiXeD_Case_Current"
	if err := os.MkdirAll(filepath.Join(dir, name), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	if _, err := Resolve(strings.ToUpper(dir), name); err != nil {
		t.Errorf("Resolve with a differently cased root = %v, want the same folder", err)
	}
}

// withinDir is the check that actually holds: Join cleans "..", so a name that
// escapes looks innocent by the time it is joined. It is pinned directly
// because Windows treating C:\Backups and c:\backups as one folder is what
// makes it correct, and a string comparison would quietly disagree.
func TestWithinDirComparesCleanedAbsolutePaths(t *testing.T) {
	dir := t.TempDir()

	if !withinDir(dir, filepath.Join(dir, "2026-09-29_current")) {
		t.Error("a point inside backups\\ must be inside")
	}
	if withinDir(dir, filepath.Dir(dir)) {
		t.Error("the folder above backups\\ must be outside it")
	}
	if !withinDir(dir, filepath.Join(dir, "a", "b", "c")) {
		t.Error("a point in a subfolder of a point must be inside")
	}
}

// A nil Ops must not panic: the disk figure is decoration, the list is not.
func TestScanSurvivesWithoutAnOps(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "a_datadir", "mysql_data.7z"), 10, time.Time{})

	inv := Scan(nil, dir)
	if len(inv.Points) != 1 {
		t.Fatalf("want 1 point, got %d", len(inv.Points))
	}
	if inv.FreeBytes != 0 {
		t.Errorf("FreeBytes = %d, want 0 when there is no Ops", inv.FreeBytes)
	}
}
