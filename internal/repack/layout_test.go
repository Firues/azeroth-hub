package repack

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// makeRepack builds a directory shaped like a repack root and returns it.
//
// Two of the three markers - configs and data\dbc - are DIRECTORIES. A
// probe that only accepts files reports "repack not found" while standing
// in the right folder; the spike shipped that bug and hit it on its first
// run.
func makeRepack(t *testing.T, name string) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), name)
	for _, d := range []string{"configs", filepath.Join("data", "dbc"), "logs"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatalf("MkdirAll(%s): %v", d, err)
		}
	}
	for _, f := range []string{"worldserver.exe", "authserver.exe"} {
		if err := os.WriteFile(filepath.Join(root, f), []byte{}, 0o644); err != nil {
			t.Fatalf("WriteFile(%s): %v", f, err)
		}
	}
	for _, f := range []string{
		filepath.Join("mysql", "bin", "mysqld.exe"),
		filepath.Join("mysql", "bin", "mysqladmin.exe"),
		filepath.Join("configs", "worldserver.conf"),
		filepath.Join("configs", "authserver.conf"),
		filepath.Join("temp", "mysql_ac.conf"),
	} {
		p := filepath.Join(root, f)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatalf("MkdirAll(%s): %v", filepath.Dir(f), err)
		}
		if err := os.WriteFile(p, []byte{}, 0o644); err != nil {
			t.Fatalf("WriteFile(%s): %v", f, err)
		}
	}
	return root
}

// nest creates n nested directories under root and returns the deepest.
func nest(t *testing.T, root string, n int) string {
	t.Helper()
	cur := root
	for i := 0; i < n; i++ {
		cur = filepath.Join(cur, "a")
	}
	if err := os.MkdirAll(cur, 0o755); err != nil {
		t.Fatalf("MkdirAll nested: %v", err)
	}
	return cur
}

func TestLocateFromFindsRootFromNestedDir(t *testing.T) {
	root := makeRepack(t, "repack")
	start := nest(t, root, 3)

	lay, err := LocateFrom(start)
	if err != nil {
		t.Fatalf("LocateFrom: %v", err)
	}
	if lay.Root != root {
		t.Fatalf("Root = %q, want %q", lay.Root, root)
	}
}

func TestLocateFromFillsEveryPath(t *testing.T) {
	root := makeRepack(t, "repack")
	lay, err := LocateFrom(root)
	if err != nil {
		t.Fatalf("LocateFrom: %v", err)
	}

	// Every path must actually exist. A Layout with a wrong MySQLdExe would
	// fail much later, inside a start sequence, as "file not found".
	for name, p := range map[string]string{
		"WorldserverExe": lay.WorldserverExe,
		"AuthserverExe":  lay.AuthserverExe,
		"MySQLdExe":      lay.MySQLdExe,
		"MySQLadminExe":  lay.MySQLadminExe,
		"WorldConf":      lay.WorldConf,
		"AuthConf":       lay.AuthConf,
		"MySQLPassConf":  lay.MySQLPassConf,
	} {
		if p == "" {
			t.Errorf("%s is empty", name)
			continue
		}
		if _, err := os.Stat(p); err != nil {
			t.Errorf("%s = %q does not exist: %v", name, p, err)
		}
		if !strings.HasPrefix(strings.ToLower(p), strings.ToLower(root)) {
			t.Errorf("%s = %q escapes Root %q", name, p, root)
		}
	}
}

func TestLocateFromHandlesSpacesAndParens(t *testing.T) {
	// The real repack folder is named
	// "AzerothCore WoTLK Repack (Playerbots + Individual Progression)".
	root := makeRepack(t, "AzerothCore WoTLK Repack (Playerbots + Individual Progression)")
	start := nest(t, root, 2)

	lay, err := LocateFrom(start)
	if err != nil {
		t.Fatalf("LocateFrom on a path with spaces and parentheses: %v", err)
	}
	if lay.Root != root {
		t.Fatalf("Root = %q, want %q", lay.Root, root)
	}
}

func TestLocateFromStopsAtTenLevels(t *testing.T) {
	// The bound must be a real bound. Walking to the filesystem root would
	// find some unrelated worldserver.exe on someone's drive and adopt it.
	root := makeRepack(t, "repack")
	start := nest(t, root, 12)

	if _, err := LocateFrom(start); err == nil {
		t.Fatalf("LocateFrom succeeded from 12 levels below the root; the 10-level bound is not enforced")
	}
}

func TestLocateFromFindsRootAtExactlyTenLevels(t *testing.T) {
	root := makeRepack(t, "repack")
	start := nest(t, root, 9) // the root itself is the 10th directory examined

	lay, err := LocateFrom(start)
	if err != nil {
		t.Fatalf("LocateFrom at the bound: %v", err)
	}
	if lay.Root != root {
		t.Fatalf("Root = %q, want %q", lay.Root, root)
	}
}

func TestLocateFromPrefersNearestRoot(t *testing.T) {
	// A repack unpacked inside a repack: the inner one is the live install.
	outer := makeRepack(t, "outer")
	inner := makeRepack(t, "inner")
	if err := os.Rename(inner, filepath.Join(outer, "inner")); err != nil {
		t.Fatalf("moving the inner repack: %v", err)
	}
	start := nest(t, filepath.Join(outer, "inner"), 1)

	lay, err := LocateFrom(start)
	if err != nil {
		t.Fatalf("LocateFrom: %v", err)
	}
	if lay.Root != filepath.Join(outer, "inner") {
		t.Fatalf("Root = %q, want the inner %q", lay.Root, filepath.Join(outer, "inner"))
	}
}

func TestLocateFromFailsWhenNoMarkers(t *testing.T) {
	empty := t.TempDir()

	_, err := LocateFrom(empty)
	if err == nil {
		t.Fatal("LocateFrom found a repack in an empty directory")
	}
	// The message must say what was looked for, or the user is left
	// guessing which part of their install is wrong.
	for _, want := range []string{"worldserver.exe", "configs", "dbc"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %q, want it to mention %q", err, want)
		}
	}
}

func TestLocateFromNamesTheMissingMarkerInAPartialDirectory(t *testing.T) {
	// A half-unpacked copy must not be adopted, and the message must say
	// which piece is absent rather than "not found".
	root := filepath.Join(t.TempDir(), "partial")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "worldserver.exe"), []byte{}, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	_, err := LocateFrom(root)
	if err == nil {
		t.Fatal("LocateFrom accepted a directory holding only worldserver.exe")
	}
	if !strings.Contains(err.Error(), "configs") {
		t.Errorf("error = %q, want it to name the missing configs directory", err)
	}
	if !strings.Contains(err.Error(), root) {
		t.Errorf("error = %q, want it to name the offending directory %q", err, root)
	}
}

func TestLocateFromStartsAtTheGivenDirNotAboveIt(t *testing.T) {
	// A repack at start itself must win over nothing, and a start that IS
	// the root must not require stepping up first.
	root := makeRepack(t, "repack")

	lay, err := LocateFrom(root)
	if err != nil {
		t.Fatalf("LocateFrom(root): %v", err)
	}
	if lay.Root != root {
		t.Fatalf("Root = %q, want %q", lay.Root, root)
	}
}

func TestLayoutMySQLadminNextToMySQLd(t *testing.T) {
	// Task 5 stops MySQL with mysqladmin, and Task 6 starts it with mysqld.
	// Both live in mysql\bin; a layout that got them mixed up would show up
	// as "process exited immediately".
	root := makeRepack(t, "repack")
	lay, err := LocateFrom(root)
	if err != nil {
		t.Fatalf("LocateFrom: %v", err)
	}

	if filepath.Dir(lay.MySQLdExe) != filepath.Dir(lay.MySQLadminExe) {
		t.Fatalf("mysqld in %q but mysqladmin in %q", lay.MySQLdExe, lay.MySQLadminExe)
	}
	if filepath.Base(lay.MySQLdExe) != "mysqld.exe" {
		t.Fatalf("MySQLdExe = %q", lay.MySQLdExe)
	}
	if filepath.Base(lay.MySQLadminExe) != "mysqladmin.exe" {
		t.Fatalf("MySQLadminExe = %q", lay.MySQLadminExe)
	}
}
