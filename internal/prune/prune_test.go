package prune

import (
	"os"
	"path/filepath"
	"testing"

	"wow-hub/internal/repack"
)

// A copy is a whole folder of dumps - 385 МБ in four files, or 130 Мб in one
// archive. Leaving any of it behind would be the pointlessness.
func TestRemoveDeletesTheWholeCopyFolder(t *testing.T) {
	lay := fixture(t)
	point := filepath.Join(lay.BackupsDir, "2026-09-29_bots_current")
	for _, f := range []string{"acore_world.sql", "acore_auth.sql"} {
		if err := os.WriteFile(filepath.Join(point, f), []byte("x"), 0o644); err != nil {
			t.Fatalf("write %s: %v", f, err)
		}
	}

	if err := Remove(lay, "2026-09-29_bots_current"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, err := os.Stat(point); !os.IsNotExist(err) {
		t.Errorf("the folder survived a successful delete (err = %v)", err)
	}

	// The others are not collateral.
	if _, err := os.Stat(filepath.Join(lay.BackupsDir, "20260930_075446_datadir")); err != nil {
		t.Errorf("the delete reached a neighbouring copy: %v", err)
	}
}

// The folder name comes from the page and this deletes what it resolves to,
// so the one thing that must never happen is a delete that lands outside
// backups\. Each name below would find something real if it got through.
func TestRemoveRefusesNamesThatAreNotOneFolderInBackups(t *testing.T) {
	lay := fixture(t)
	// A sibling of backups\ holding what a copy of the world looks like.
	outside := filepath.Join(lay.Root, "2026-09-29_bots_current")
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatalf("mkdir outside: %v", err)
	}
	victim := filepath.Join(outside, "acore_world.sql")
	if err := os.WriteFile(victim, []byte("x"), 0o644); err != nil {
		t.Fatalf("write outside: %v", err)
	}

	for _, name := range []string{
		"",
		".",
		"..",
		`..\2026-09-29_bots_current`,
		"../2026-09-29_bots_current",
		`2026-09-29_bots_current\..\..`,
		"nothing_here",
	} {
		if err := Remove(lay, name); err == nil {
			t.Errorf("Remove(%q) was allowed", name)
		}
	}

	if _, err := os.Stat(victim); err != nil {
		t.Errorf("a refused delete still touched the folder above backups\\: %v", err)
	}
}

// Without a repack there is no backups\ and nothing that could be deleted.
// Saying so beats a RemoveAll against an empty path.
func TestRemoveRefusesWhenThereIsNoRepack(t *testing.T) {
	if err := Remove(repack.Layout{}, "2026-09-29_bots_current"); err == nil {
		t.Fatal("Remove with an empty layout was allowed")
	}
}

// fixture builds a repack root with two copies in it, plus a README.md and the
// .cmd scripts, which is what backups\ actually looks like.
func fixture(t *testing.T) repack.Layout {
	t.Helper()
	root := t.TempDir()
	lay := repack.Layout{Root: root, BackupsDir: filepath.Join(root, "backups")}

	for _, d := range []string{
		filepath.Join(lay.BackupsDir, "2026-09-29_bots_current"),
		filepath.Join(lay.BackupsDir, "20260930_075446_datadir"),
	} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("MkdirAll(%s): %v", d, err)
		}
	}
	if err := os.WriteFile(filepath.Join(lay.BackupsDir, "20260930_075446_datadir", "mysql_data.zip"),
		[]byte("x"), 0o644); err != nil {
		t.Fatalf("write archive: %v", err)
	}
	for _, f := range []string{"README.md", "backup-table.cmd", "snapshot-mysql-data.cmd"} {
		if err := os.WriteFile(filepath.Join(lay.BackupsDir, f), []byte("x"), 0o644); err != nil {
			t.Fatalf("write %s: %v", f, err)
		}
	}
	return lay
}
