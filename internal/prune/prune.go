// Package prune deletes a rollback point from backups\.
//
// It is its own package because internal\backups is read-only by design - it
// reports what the repack's scripts wrote and never touches it. Deleting is
// the third thing that acts on that folder, next to creating and restoring,
// and it is the one with no undo: nothing here asks twice, nothing moves it
// aside first, and backups\README.md documents no way back.
//
// It does not stop the server, and does not need to. A copy on disk is not in
// use by anything: the dump finished writing before the folder appeared in the
// list, and restoring reads an archive it has already unpacked over the data.
// What must be refused instead is a delete while a copy is being made, and
// that is the caller's business - it knows whether a backup is running.
package prune

import (
	"errors"
	"fmt"
	"os"

	"wow-hub/internal/backups"
	"wow-hub/internal/i18n"
	"wow-hub/internal/repack"
)

// Remove deletes one rollback point: the folder and everything in it.
//
// The name is checked by backups.Resolve, the same guard the unpack goes
// through, because "a path built from what the page sent" is one risk and it
// should have one owner. The scripts in backups\ and README.md beside them
// are not folders and cannot be named here.
func Remove(lay repack.Layout, name string) error {
	if lay.Root == "" {
		return errors.New(i18n.T("prune.norepack"))
	}
	folder, err := backups.Resolve(lay.BackupsDir, name)
	if err != nil {
		return err
	}
	if err := os.RemoveAll(folder); err != nil {
		return fmt.Errorf("%s: %w", i18n.Tf("prune.rm.fail", name), err)
	}
	return nil
}
