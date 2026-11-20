package archive

import (
	"archive/zip"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// tree returns a fake mysql\data directory: a few InnoDB-looking files, one
// nested folder, an empty folder, and the binlogs the archiver must leave out.
func tree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	data := filepath.Join(root, "mysql", "data")
	for _, p := range []string{
		filepath.Join(data, "ibdata1"),
		filepath.Join(data, "ib_logfile0"),
		filepath.Join(data, "binlog.000123"),
		filepath.Join(data, "acore_world", "character.ibd"),
		filepath.Join(data, "acore_world", "inner", "guild.ibd"),
	} {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("data of "+filepath.Base(p)), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// An empty directory: only an explicit dir entry brings it back.
	if err := os.MkdirAll(filepath.Join(data, "#innodb_redo"), 0o755); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestRoundTripRestoresTheTreeAndExcludesBinlogs(t *testing.T) {
	root := tree(t)
	zipPath := filepath.Join(t.TempDir(), "mysql_data.zip")

	progress := 0
	if err := CreateMySQLDataZip(context.Background(), root, zipPath, func(int64) {
		progress++
	}); err != nil {
		t.Fatalf("create: %v", err)
	}

	// The binlog never entered the archive. Read the central directory
	// directly rather than trusting the unpack below to mask it.
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	var names []string
	for _, f := range zr.File {
		names = append(names, f.Name)
		if strings.Contains(strings.ToLower(f.Name), "binlog") {
			t.Errorf("binlog file archived: %s", f.Name)
		}
		if !strings.HasPrefix(f.Name, "mysql/data/") {
			t.Errorf("entry not rooted at mysql/data/: %s", f.Name)
		}
	}
	for _, want := range []string{
		"mysql/data/",
		"mysql/data/#innodb_redo/",
		"mysql/data/acore_world/character.ibd",
		"mysql/data/acore_world/inner/guild.ibd",
	} {
		found := false
		for _, n := range names {
			if n == want {
				found = true
			}
		}
		if !found {
			t.Errorf("entry %s missing from the archive; names: %v", want, names)
		}
	}

	dest := t.TempDir()
	if err := UnpackMySQLDataZip(context.Background(), zipPath, dest); err != nil {
		t.Fatalf("unpack: %v", err)
	}

	// Everything that was archived is back, byte for byte; the binlog is not.
	for _, p := range []string{
		filepath.Join(dest, "mysql", "data", "ibdata1"),
		filepath.Join(dest, "mysql", "data", "acore_world", "inner", "guild.ibd"),
	} {
		body, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("restored file %s: %v", p, err)
		}
		if !strings.HasPrefix(string(body), "data of ") {
			t.Errorf("restored file %s has wrong content: %q", p, body)
		}
	}
	if _, err := os.Stat(filepath.Join(dest, "mysql", "data", "binlog.000123")); !os.IsNotExist(err) {
		t.Errorf("binlog came back with the archive")
	}
	if _, err := os.Stat(filepath.Join(dest, "mysql", "data", "#innodb_redo")); err != nil {
		t.Errorf("empty directory did not come back: %v", err)
	}
	if progress == 0 {
		t.Errorf("progress was never reported")
	}
}

func TestCreateRefusesAMissingDataFolder(t *testing.T) {
	root := t.TempDir()
	err := CreateMySQLDataZip(context.Background(), root, filepath.Join(t.TempDir(), "x.zip"), nil)
	if err == nil || !strings.Contains(err.Error(), ErrMissingData.Error()) {
		t.Fatalf("want ErrMissingData, got %v", err)
	}
}

func TestUnpackRefusesAnEntryThatClimbsOut(t *testing.T) {
	zipPath := filepath.Join(t.TempDir(), "evil.zip")
	f, _ := os.Create(zipPath)
	zw := zip.NewWriter(f)
	// An entry that cleans to outside the destination.
	for _, name := range []string{"../evil.txt", "mysql/data/../../evil2.txt", `C:\evil3.txt`} {
		w, _ := zw.Create(name)
		w.Write([]byte("no"))
	}
	zw.Close()
	f.Close()

	dest := t.TempDir()
	err := UnpackMySQLDataZip(context.Background(), zipPath, dest)
	if err == nil || !strings.Contains(err.Error(), ErrUnsafePath.Error()) {
		t.Fatalf("want ErrUnsafePath, got %v", err)
	}
	entries, _ := os.ReadDir(dest)
	if len(entries) != 0 {
		t.Errorf("something was written before the refusal: %v", entries)
	}
}

func TestUnpackRefusesANonRegularEntry(t *testing.T) {
	zipPath := filepath.Join(t.TempDir(), "link.zip")
	f, _ := os.Create(zipPath)
	zw := zip.NewWriter(f)
	h := &zip.FileHeader{Name: "mysql/data/link", Modified: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)}
	h.SetMode(os.ModeSymlink | 0o777)
	w, _ := zw.CreateHeader(h)
	w.Write([]byte("../somewhere"))
	zw.Close()
	f.Close()

	err := UnpackMySQLDataZip(context.Background(), zipPath, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), ErrUnsafePath.Error()) {
		t.Fatalf("want ErrUnsafePath for a symlink entry, got %v", err)
	}
}

func TestCreateStopsWhenTheContextIsCancelled(t *testing.T) {
	root := tree(t)
	// A file far bigger than one progress step, and the cancel fires from the
	// progress callback itself: deterministic, with no race against how fast
	// deflate happens to be on this machine.
	big := filepath.Join(root, "mysql", "data", "ibdata1")
	if err := os.WriteFile(big, make([]byte, 64<<20), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	zipPath := filepath.Join(t.TempDir(), "mysql_data.zip")
	err := CreateMySQLDataZip(ctx, root, zipPath, func(int64) {
		cancel()
	})
	if err == nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled, got %v", err)
	}
}
