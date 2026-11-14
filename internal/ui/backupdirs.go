package ui

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/pglass/checkin/internal/backup"
	"github.com/pglass/checkin/internal/config"
	"github.com/pglass/checkin/internal/volume"
)

// useBackupDirIDs makes backups find each backup directory through the drive
// ID saved for it in cfg, so a drive mounted somewhere else since it was
// configured still receives its backups and a different drive at its old path
// does not. Called whenever the running config changes.
func useBackupDirIDs(cfg config.Config) {
	backup.SetDirResolver(func(dir string) (string, error) {
		return volume.Resolve(dir, cfg.BackupDirID(dir))
	})
}

// movedBackupDir returns where dir is now if its drive is connected and mounted
// somewhere other than dir's configured path (renamed, on macOS, or given
// another drive letter, on Windows), and "" otherwise.
func movedBackupDir(cfg config.Config, dir string) string {
	r, err := volume.Resolve(dir, cfg.BackupDirID(dir))
	if err != nil || filepath.Clean(r) == filepath.Clean(dir) {
		return ""
	}
	return r
}

// backupDirIDs returns the drive IDs to save for dirs. A directory keeps the ID
// it already had in prev; one without an ID gets the ID of the drive it is on
// now, if that drive is connected. An existing ID is never re-read from the
// path: by now another drive may be mounted there, which is exactly what the ID
// is for catching.
func backupDirIDs(dirs []string, prev map[string]string) map[string]string {
	var out map[string]string // nil when empty, matching config.Default
	for _, d := range dirs {
		key := filepath.Clean(d)
		id := prev[key]
		if id == "" {
			var err error
			if id, err = volume.ID(d); err != nil {
				// Saved without an ID: it is used at its path until the
				// directory is added again with its drive connected.
				slog.Warn("could not identify backup directory's drive", "dir", d, "err", err)
			}
		}
		if id == "" {
			continue
		}
		if out == nil {
			out = map[string]string{}
		}
		out[key] = id
	}
	return out
}

// duplicateBackupDirs finds backup directories that are the same directory as
// an earlier one in dirs, returning a map from each such index to the index of
// the first. Empty entries are skipped.
//
// Two directories with drive IDs (from prev, or read from the drive now, as
// backupDirIDs would) are the same when they have the same ID and the same path
// below the drive: on one drive, wherever it is mounted. Otherwise they are
// the same when they are one directory on disk (os.SameFile, which sees
// through symlinks and letter case), or, if either does not exist, when their
// cleaned paths match.
func duplicateBackupDirs(dirs []string, prev map[string]string) map[int]int {
	type ident struct{ id, rel, path string }
	idents := make([]ident, len(dirs))
	for i, d := range dirs {
		d = strings.TrimSpace(d)
		if d == "" {
			continue
		}
		key := filepath.Clean(d)
		id := prev[key]
		if id == "" {
			id, _ = volume.ID(d)
		}
		rel, _ := volume.Rel(d)
		idents[i] = ident{id: id, rel: rel, path: key}
	}

	dups := map[int]int{}
	for j := range idents {
		for i := 0; i < j; i++ {
			a, b := idents[i], idents[j]
			if a.path == "" || b.path == "" {
				continue
			}
			if sameBackupDir(a.id, a.rel, a.path, b.id, b.rel, b.path) {
				dups[j] = i
				break
			}
		}
	}
	return dups
}

// sameBackupDir is duplicateBackupDirs' test for one pair.
func sameBackupDir(aID, aRel, aPath, bID, bRel, bPath string) bool {
	if aID != "" && bID != "" {
		return aID == bID && filepath.Clean(aRel) == filepath.Clean(bRel)
	}
	ai, aErr := os.Stat(aPath)
	bi, bErr := os.Stat(bPath)
	if aErr == nil && bErr == nil {
		return os.SameFile(ai, bi)
	}
	return aPath == bPath
}

// backupDirNotFound returns the error to show for a backup directory that was
// added or edited in Settings, or "" if it is fine: it must be an existing
// directory. Directories already saved are not checked this way -- one on a
// drive that is unplugged right now must not stop other settings being saved,
// and one on a renamed drive is found through its drive ID.
func backupDirNotFound(dir string) string {
	info, err := os.Stat(dir)
	if err != nil {
		return "Directory not found"
	}
	if !info.IsDir() {
		return "Not a directory"
	}
	return ""
}
