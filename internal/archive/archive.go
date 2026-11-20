// Package archive is the panel's own archiver for mysql\data.
//
// It replaces two outside dependencies the panel used to carry: the repack's
// snapshot-mysql-data.cmd (which named the folder and drove an installed
// 7-Zip) and the 7-Zip installation itself. The archive is an ordinary zip
// whose entries are rooted at "mysql/data/...", exactly the shape the 7-Zip
// command line produced, so unpacking into the repack root puts every file
// back where it came from - the same restore-over-root semantics the repack's
// README documents for the hand procedure.
//
// binlog.* files are excluded, as the script excluded them: the repack runs
// MySQL with --skip-log-bin, so binlogs on disk are legacy leftovers, and a
// snapshot that carries them is not more correct than one that does not.
package archive

import (
	"archive/zip"
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// progressEvery is how often the archiver reports and checks for
// cancellation, in bytes written. Same cadence as the logical dumps' reports:
// "a 329 MB dump produces about forty reports".
const progressEvery = 8 << 20

// ErrMissingData says the repack root has no mysql\data folder to archive.
var ErrMissingData = errors.New("mysql\\data not found")

// ErrUnsafePath says an archive entry would land outside the destination
// folder - an absolute path, a drive letter, or a ".." climb. Refused rather
// than followed: the archive came from somewhere and a restore overwrites
// live files.
var ErrUnsafePath = errors.New("archive entry escapes the destination folder")

func init() {
	// No custom compressor registration: archive/zip already registers
	// Deflate and panics on a second attempt, and the stdlib's flate is
	// within the time budget the 7-Zip script had (31 s for this data set).
}

// CreateMySQLDataZip archives <repackRoot>\mysql\data into a zip file.
//
// onProgress is called with the number of archive bytes written so far, no
// more often than once per progressEvery; it may be nil. Cancellation is
// honoured between chunks, so a cancelled snapshot stops within a second and
// the caller deletes the half-written folder it named.
func CreateMySQLDataZip(ctx context.Context, repackRoot, zipPath string, onProgress func(int64)) error {
	dataDir := filepath.Join(repackRoot, "mysql", "data")
	if _, err := os.Stat(dataDir); err != nil {
		return ErrMissingData
	}

	out, err := os.Create(zipPath)
	if err != nil {
		return err
	}
	defer out.Close()

	// Every byte the zip writes goes through this writer, so cancellation is
	// noticed at the pace io.Copy flushes its 32 KB chunks - within a second
	// even against a multi-gigabyte ibdata1.
	cw := &countingWriter{ctx: ctx, w: out, on: onProgress}
	zw := zip.NewWriter(cw)

	err = filepath.WalkDir(dataDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		// -xr!binlog.*: the recursive, case-insensitive exclusion the 7-Zip
		// command line used. The repack runs MySQL without binlogs, so what
		// is on disk is legacy.
		if strings.HasPrefix(strings.ToLower(d.Name()), "binlog.") {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		rel, err := filepath.Rel(repackRoot, path)
		if err != nil {
			return err
		}
		// Forward slashes, per the zip spec, so any unzip tool - not only
		// this one - reads the archive the way it was written.
		name := filepath.ToSlash(rel)

		if d.IsDir() {
			h := &zip.FileHeader{Name: name + "/", Modified: modTime(d)}
			_, err := zw.CreateHeader(h)
			return err
		}

		if err := ctx.Err(); err != nil {
			return err
		}

		info, err := d.Info()
		if err != nil {
			return err
		}
		h := &zip.FileHeader{Name: name, Modified: info.ModTime()}
		h.SetMode(info.Mode())

		w, err := zw.CreateHeader(h)
		if err != nil {
			return err
		}
		src, err := os.Open(path)
		if err != nil {
			return err
		}
		defer src.Close()

		_, err = io.Copy(w, src)
		return err
	})
	if err != nil {
		// The caller deletes the whole point folder on failure, so the
		// half-written zip does not need its own cleanup here - only a close
		// so the file handle does not linger.
		return err
	}
	return zw.Close()
}

// UnpackMySQLDataZip extracts a mysql\data snapshot into destRoot.
//
// destRoot is the repack root, because the entries are stored rooted at
// mysql\data/. Every entry name is checked against the destination before
// anything is written: an absolute path, a drive letter, a backslash or a
// ".." climb is refused rather than followed.
func UnpackMySQLDataZip(ctx context.Context, zipPath, destRoot string) error {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer zr.Close()

	dest := filepath.Clean(destRoot)

	for _, f := range zr.File {
		if err := ctx.Err(); err != nil {
			return err
		}

		target, err := safeTarget(dest, f.Name)
		if err != nil {
			return err
		}

		if strings.HasSuffix(f.Name, "/") {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		// Only regular files and folders are in these archives. Anything else
		// - a symlink, a device - is not something mysql\data contains, and
		// writing it blind would be trusting the archive with the disk.
		if !f.Mode().IsRegular() {
			return ErrUnsafePath
		}

		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}

		src, err := f.Open()
		if err != nil {
			return err
		}
		dst, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
		if err != nil {
			src.Close()
			return err
		}
		_, err = io.Copy(dst, src)
		src.Close()
		if closeErr := dst.Close(); err == nil {
			err = closeErr
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// safeTarget resolves one archive entry name to a path under dest, refusing
// anything that would land outside it.
func safeTarget(dest, name string) (string, error) {
	if name == "" || strings.ContainsRune(name, '\\') || filepath.IsAbs(name) ||
		strings.HasPrefix(name, "/") || isDrivePath(name) {
		return "", ErrUnsafePath
	}
	// A ".." element is refused outright rather than clamped: our archives
	// never contain one, so its presence means the archive was not made by
	// the panel, and clamping would quietly extract it somewhere the name
	// does not say.
	for _, part := range strings.Split(name, "/") {
		if part == ".." {
			return "", ErrUnsafePath
		}
	}

	target := filepath.Join(dest, filepath.FromSlash(filepath.Clean("/"+name)))
	// The "/"+name trick makes Clean resolve climbs, and the Rel check then
	// answers the only question that matters: is the result still inside?
	rel, err := filepath.Rel(dest, target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", ErrUnsafePath
	}
	return target, nil
}

func isDrivePath(name string) bool {
	if len(name) < 2 || name[1] != ':' {
		return false
	}
	c := name[0]
	return (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z')
}

func modTime(d fs.DirEntry) time.Time {
	if info, err := d.Info(); err == nil {
		return info.ModTime()
	}
	return time.Time{}
}

// countingWriter counts bytes written through it, calls back at most once per
// progressEvery, and refuses new writes once the context is done.
type countingWriter struct {
	ctx     context.Context
	w       io.Writer
	on      func(int64)
	written int64
	next    int64
}

func (c *countingWriter) Write(p []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := c.w.Write(p)
	c.written += int64(n)
	if c.on != nil && c.written >= c.next {
		c.next = c.written + progressEvery
		c.on(c.written)
	}
	return n, err
}
