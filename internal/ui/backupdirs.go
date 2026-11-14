package ui

import (
	"log/slog"
	"path/filepath"

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
