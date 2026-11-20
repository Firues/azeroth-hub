// Package repack finds the local AzerothCore repack and the files in it
// that the app needs.
//
// Every path the app uses comes from here. Nothing else hardcodes a path,
// and nothing else guesses: a wrong guess surfaces as "file not found"
// halfway through a start sequence, with three processes already running.
package repack

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"wow-hub/internal/i18n"
)

// Layout holds every path the app uses, all of them inside one repack.
type Layout struct {
	Root string

	WorldserverExe string
	AuthserverExe  string
	MySQLdExe      string
	MySQLadminExe  string
	// MySQLdumpExe makes the logical dumps. The repack ships it in
	// mysql\bin alongside mysqld; a dump written by hand would be a second
	// implementation of charset handling and escaping, and the existing
	// dumps in backups\ were made by this binary.
	MySQLdumpExe string

	WorldConf     string
	AuthConf      string
	MySQLPassConf string
	// MySQLRootConf is optional. When it exists it overrides the
	// maintenance credentials used to shut MySQL down cleanly; see
	// MySQLRootCredentials.
	MySQLRootConf string

	// WorldConfRel and AuthConfRel are the config paths as the servers
	// themselves want them: relative to the repack root, with forward
	// slashes.
	//
	// worldserver rejects an absolute --config outright, so the supervisor
	// cannot pass the absolute WorldConf above. These two exist so that
	// fact lives in one place instead of being retyped as a string literal
	// at the call site.
	WorldConfRel string
	AuthConfRel  string

	// DataDbcDir is a repack marker, so the Health screen can verify the
	// install is complete rather than only that worldserver.exe is present.
	DataDbcDir string
	LogsDir    string
	// BackupsDir holds the rollback points: logical dumps, physical
	// snapshots and single-table dumps, all made by backups\*.cmd.
	BackupsDir string

	// SettingsFile holds the choices the panel remembers between runs.
	//
	// Beside the exe, not under %APPDATA%, for the same reason every other
	// path here is derived from the exe's own location: the repack is a
	// folder people move, and a preferences file that stayed behind would
	// describe a realm that is no longer the one next to it.
	SettingsFile string
}

// The four schemas, named once. Code that needs a specific one uses the
// constant; code that needs all of them uses Databases.
const (
	DBWorld      = "acore_world"
	DBCharacters = "acore_characters"
	DBPlayerbots = "acore_playerbots"
	DBAuth       = "acore_auth"
)

// Databases are the four schemas of this repack, in the order
// backups\restore-all-db.cmd imports them.
//
// Everything that lists, measures or dumps the databases takes the list
// from here instead of retyping the names. A typo in one of four string
// literals is invisible until something silently skips a database, and the
// one place that must never happen is a restore.
var Databases = []string{
	DBWorld,
	DBCharacters,
	DBPlayerbots,
	DBAuth,
}

// markers identify a repack root. All three must sit in the same directory.
//
// Two of them - configs and data\dbc - are directories, and that detail is
// not incidental: a probe that only accepts files reports "not found" while
// standing in the correct folder. The spike shipped exactly that bug and
// hit it on its first run.
var markers = []string{
	"worldserver.exe",
	"configs",
	filepath.Join("data", "dbc"),
}

// maxLevels bounds the upward walk. Without it, LocateFrom would climb to
// the filesystem root and could find an unrelated worldserver.exe somewhere
// on the drive and adopt that as the install.
const maxLevels = 10

// Locate finds the repack from the running executable's own directory.
func Locate() (Layout, error) {
	exe, err := os.Executable()
	if err != nil {
		return Layout{}, fmt.Errorf("%s: %w", i18n.T("layout.selfpath"), err)
	}
	return LocateFrom(filepath.Dir(exe))
}

// LocateFrom walks up from start, at most maxLevels directories, and returns
// the first repack root it finds.
//
// It walks up rather than taking the parent directory because under
// "wails dev" the binary sits three levels deeper, in build\bin, and a
// parent-relative lookup would break the development loop.
//
// When a directory holds some markers but not all, it is named in the error:
// a half-unpacked copy is the common failure and "not found" tells the user
// nothing about which piece is missing.
func LocateFrom(start string) (Layout, error) {
	if start == "" {
		return Layout{}, errors.New(i18n.T("layout.emptypath"))
	}

	dir, err := filepath.Abs(start)
	if err != nil {
		return Layout{}, fmt.Errorf("%s: %w", i18n.Tf("layout.abs", start), err)
	}

	for level := 0; level < maxLevels; level++ {
		if missing := missingMarkers(dir); len(missing) == 0 {
			return newLayout(dir), nil
		} else if len(missing) < len(markers) {
			// A partial repack: someone unpacking over an existing install
			// sees this. Say which piece is absent.
			return Layout{}, errors.New(i18n.Tf("layout.almost", missing, dir))
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	return Layout{}, errors.New(i18n.Tf("layout.notfound", start, markers))
}

// missingMarkers returns the markers absent from dir. An empty result means
// dir is a repack root.
func missingMarkers(dir string) []string {
	var missing []string
	for _, m := range markers {
		if _, err := os.Stat(filepath.Join(dir, m)); err != nil {
			missing = append(missing, m)
		}
	}
	return missing
}

func newLayout(root string) Layout {
	return Layout{
		Root:           root,
		WorldserverExe: filepath.Join(root, "worldserver.exe"),
		AuthserverExe:  filepath.Join(root, "authserver.exe"),
		MySQLdExe:      filepath.Join(root, "mysql", "bin", "mysqld.exe"),
		MySQLadminExe:  filepath.Join(root, "mysql", "bin", "mysqladmin.exe"),
		MySQLdumpExe:   filepath.Join(root, "mysql", "bin", "mysqldump.exe"),
		WorldConf:      filepath.Join(root, "configs", "worldserver.conf"),
		AuthConf:       filepath.Join(root, "configs", "authserver.conf"),
		MySQLPassConf:  filepath.Join(root, "temp", "mysql_ac.conf"),
		MySQLRootConf:  filepath.Join(root, "temp", "mysql_root.conf"),
		WorldConfRel:   "configs/worldserver.conf",
		AuthConfRel:    "configs/authserver.conf",
		DataDbcDir:     filepath.Join(root, "data", "dbc"),
		LogsDir:        filepath.Join(root, "logs"),
		BackupsDir:     filepath.Join(root, "backups"),
		SettingsFile:   filepath.Join(root, "wow-hub.settings.json"),
	}
}
