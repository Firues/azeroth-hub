// Package backups reads the rollback points that the repack's own scripts
// write into backups\.
//
// Read-only, and deliberately so. The scripts (backup-table.cmd,
// restore-table.cmd, snapshot-mysql-data.cmd, restore-all-db.cmd) own the
// format; this package only reports what is on disk. Inventing a second
// backup format in the app would produce two sets of files that look alike
// and are not interchangeable, and the day someone needs a rollback is the
// worst possible day to discover that.
//
// Resolve is the one thing here that is not reading: it turns a folder name
// from the page into a path, and it is the guard every writer of backups\
// goes through - the unpack and the delete alike. One copy of that guard
// beats two that drift apart.
package backups

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"wow-hub/internal/i18n"
	"wow-hub/internal/osops"
	"wow-hub/internal/repack"
)

// Resolve turns a folder name from the page into the folder it means inside
// dir, and refuses anything that is not one plain child of it.
//
// Three separate refusals, because each catches something the others do not:
// an empty or dotted name is not a folder; a separator in it is a path
// rather than a name; and the final containment check is what actually holds
// - Join already cleans "..", so the name could be perfectly innocent and
// still resolve outside backups\.
//
// It also insists the folder exists. os.RemoveAll treats a missing path as
// success, so a delete that trusted it would answer "удалено" about a copy
// that was never there.
func Resolve(dir, name string) (string, error) {
	if dir == "" {
		return "", errors.New(i18n.T("backups.nodir"))
	}
	if name == "" {
		return "", errors.New(i18n.T("backups.nopoint"))
	}
	if name != filepath.Base(name) || name == "." || name == ".." {
		return "", badName(name)
	}

	folder := filepath.Join(dir, name)
	if !withinDir(dir, folder) {
		return "", badName(name)
	}

	info, err := os.Stat(folder)
	if err != nil {
		return "", errors.New(i18n.Tf("backups.nofolder", name))
	}
	if !info.IsDir() {
		return "", errors.New(i18n.Tf("backups.isfile", name))
	}
	return folder, nil
}

func badName(name string) error {
	return errors.New(i18n.Tf("backups.badname", name))
}

// withinDir reports whether path is dir or something under it.
//
// Compared case-insensitively and on cleaned absolute paths, because Windows
// treats C:\Backups and c:\backups as one folder while a string comparison
// does not.
func withinDir(dir, path string) bool {
	absDir, err1 := filepath.Abs(dir)
	absPath, err2 := filepath.Abs(path)
	if err1 != nil || err2 != nil {
		return false
	}
	rel, err := filepath.Rel(absDir, absPath)
	if err != nil {
		return false
	}
	if rel == "." {
		return true
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// Kind is what sort of rollback point a folder is.
type Kind string

const (
	// KindLogical is a folder of per-database dumps: acore_world.sql and
	// friends, made by mysqldump against a running MySQL.
	KindLogical Kind = "logical"
	// KindPhysical is a folder holding mysql_data.7z, a snapshot of the
	// whole mysql\data directory taken with MySQL stopped.
	KindPhysical Kind = "physical"
	// KindTables is a folder of single-table dumps, as backup-table.cmd
	// writes: game_event_20260929_091154.sql and the like.
	KindTables Kind = "tables"
	// KindOther is a folder that matches none of the above. It is reported
	// rather than skipped: a folder someone made by hand is still a copy of
	// their data, and a list that silently omits it reads as "there is
	// nothing here".
	KindOther Kind = "other"
)

// Point is one folder under backups\.
type Point struct {
	Name string
	Kind Kind
	Path string
	// Created comes from the folder name when it carries a timestamp, and
	// from the newest file inside otherwise. The name is preferred because
	// it is what the scripts mean by the moment of the snapshot.
	Created time.Time
	// SizeBytes is the sum of every regular file in the folder.
	SizeBytes int64
	Files     int
	// Databases are the per-database dumps present, for a logical point.
	Databases []string
	// Note says what is unusual: a database missing from a logical dump, a
	// folder that is not a rollback point at all. Empty when nothing is.
	Note string
}

// Inventory is the whole of backups\ at one instant.
type Inventory struct {
	Dir    string
	Points []Point
	// TotalBytes is what the points take together.
	TotalBytes int64
	// FreeBytes and TotalDiskBytes describe the volume the folder is on.
	// Zero when the figure could not be read.
	FreeBytes      int64
	TotalDiskBytes int64
	// Err is set when the folder itself could not be read. Points is then
	// empty, and the caller must show the error rather than "no backups" -
	// an empty list and an unreadable folder mean opposite things.
	Err string
}

// Scan reads dir and describes every rollback point in it, newest first.
//
// ops may be nil; the disk figures are then left at zero.
func Scan(ops osops.Ops, dir string) Inventory {
	inv := Inventory{Dir: dir}

	entries, err := os.ReadDir(dir)
	if err != nil {
		inv.Err = i18n.Tf("dump.read.fail", dir) + ": " + err.Error()
		return inv
	}

	for _, e := range entries {
		// README.md sits here too. Only folders are rollback points.
		if !e.IsDir() {
			continue
		}
		p := describe(filepath.Join(dir, e.Name()), e.Name())
		inv.Points = append(inv.Points, p)
		inv.TotalBytes += p.SizeBytes
	}

	sort.Slice(inv.Points, func(i, j int) bool {
		return inv.Points[i].Created.After(inv.Points[j].Created)
	})

	if ops != nil {
		if free, total, derr := ops.DiskFree(dir); derr == nil {
			inv.FreeBytes, inv.TotalDiskBytes = free, total
		}
	}

	return inv
}

func describe(path, name string) Point {
	p := Point{Name: name, Path: path, Kind: KindOther}

	var (
		databases  []string
		newest     time.Time
		hasArchive bool
		hasTable   bool
	)

	// A walk rather than one ReadDir: the scripts write flat folders today,
	// and a person who groups their snapshots into subfolders should not
	// have those counted as zero bytes.
	_ = filepath.WalkDir(path, func(_ string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			// Unreadable entries are skipped rather than failing the whole
			// scan: one locked file must not hide every other rollback
			// point.
			return nil
		}

		if info, ierr := d.Info(); ierr == nil {
			p.Files++
			p.SizeBytes += info.Size()
			if info.ModTime().After(newest) {
				newest = info.ModTime()
			}
		}

		base := d.Name()
		lower := strings.ToLower(base)

		if lower == "mysql_data.7z" {
			hasArchive = true
			return nil
		}
		if !strings.HasSuffix(lower, ".sql") {
			return nil
		}
		if db := databaseOf(base); db != "" {
			if !slices.Contains(databases, db) {
				databases = append(databases, db)
			}
			return nil
		}
		hasTable = true
		return nil
	})

	switch {
	case hasArchive:
		p.Kind = KindPhysical
	case len(databases) > 0:
		p.Kind = KindLogical
		p.Databases = databases
	case hasTable:
		p.Kind = KindTables
	default:
		p.Note = i18n.T("backups.notpoint")
	}

	if p.Kind == KindLogical {
		if missing := missingDatabases(databases); len(missing) > 0 {
			p.Note = i18n.Tf("backups.nodump", strings.Join(missing, ", "))
		}
	}

	// Prefer the moment the snapshot was taken over the moment the folder
	// was last written to, but only when the name actually carries a time.
	// "20260930_075446_datadir" does; "2026-09-29_bots_current" does not,
	// and taking midnight from it would place every logical dump at the
	// start of its day - which is exactly when none of them were made.
	if t, ok := stampWithTime(name); ok {
		p.Created = t
	} else if !newest.IsZero() {
		p.Created = newest
	} else if t, ok := stampDate(name); ok {
		p.Created = t
	}

	return p
}

// databaseOf reports which schema a dump file holds, or "" when it is not a
// whole-database dump.
func databaseOf(base string) string {
	for _, db := range repack.Databases {
		if strings.EqualFold(base, db+".sql") {
			return db
		}
	}
	return ""
}

func missingDatabases(present []string) []string {
	var missing []string
	for _, db := range repack.Databases {
		if !slices.Contains(present, db) {
			missing = append(missing, db)
		}
	}
	return missing
}

// stampWithTime reads a full timestamp out of a folder name.
//
// Only snapshot-mysql-data.cmd writes one: "20260930_075446_datadir".
func stampWithTime(name string) (time.Time, bool) {
	const layout = "20060102_150405"
	if len(name) < len(layout) {
		return time.Time{}, false
	}
	t, err := time.ParseInLocation(layout, name[:len(layout)], time.Local)
	return t, err == nil
}

// stampDate reads a bare date out of a folder name, as the logical dumps
// and the per-table folders have: "2026-09-29_current", "2026-09-29".
func stampDate(name string) (time.Time, bool) {
	const layout = "2006-01-02"
	if len(name) < len(layout) {
		return time.Time{}, false
	}
	t, err := time.ParseInLocation(layout, name[:len(layout)], time.Local)
	return t, err == nil
}
