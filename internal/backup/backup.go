// Package backup writes a single compressed archive holding a snapshot of
// every Center's database, plus a JSON manifest describing what is inside.
//
// One archive per run, covering all Centers, is deliberate: the user picks a
// backup directory once and gets one file per backup to keep track of, rather
// than a file per Center that has to be kept in sync by hand.
//
// Snapshots are taken with SQLite's VACUUM INTO, which runs inside a read
// transaction and writes a fresh, self-contained database file. That matters in
// WAL mode: copying checkin.db on its own would silently omit any committed
// transactions still sitting in the -wal sidecar, and copying it while a writer
// is active could interleave pages from different transactions. VACUUM INTO has
// neither problem -- the output is a consistent point-in-time database with no
// sidecar files to keep alongside it.
//
// Backups are taken from the startup screen, with no Center open, so each
// snapshot briefly opens the database itself (see snapshotCenter).
package backup

import (
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/pglass/checkin/internal/center"
)

// ManifestName is the JSON manifest's name inside the archive.
const ManifestName = "manifest.json"

// ArchivePrefix and archiveExt bracket a backup archive's file name, with a
// timestamp in between. Both are used to recognise this app's archives when
// listing and pruning the backup directory, so unrelated files there are left
// alone.
const (
	ArchivePrefix = "checkin-backup-"
	archiveExt    = ".zip"
)

// archiveTimeFormat is the timestamp in an archive's file name. It is
// filename-safe on Windows (no colons) and sorts lexicographically in
// chronological order, which is what lets pruning sort by name.
const archiveTimeFormat = "2006-01-02-150405"

// Manifest is the JSON document stored in every archive, describing the backup
// as a whole and each Center in it.
type Manifest struct {
	// Version is the program version that wrote the archive, so a restore can
	// tell which schema the databases came from.
	Version string `json:"version"`
	// CreatedAt is when the backup ran.
	CreatedAt time.Time `json:"created_at"`
	// Centers describes each Center included, in archive order.
	Centers []CenterInfo `json:"centers"`
}

// CenterInfo describes one Center's snapshot inside the archive.
type CenterInfo struct {
	// Name is the Center's name, which is also its directory name.
	Name string `json:"name"`
	// File is the snapshot's path within the archive.
	File string `json:"file"`
	// SizeBytes is the snapshot's uncompressed size. Recorded so a restore can
	// report and check the size without decompressing the whole archive.
	SizeBytes int64 `json:"size_bytes"`
	// StudentCount is the number of students in the snapshot, shown in the
	// restore UI so the user can tell backups apart by their contents.
	StudentCount int64 `json:"student_count"`
}

// Progress reports a backup's step-by-step progress. step counts completed
// steps, total is the number Steps returned for the same Center list, and desc
// names the step just finished. It is called from the goroutine running the
// backup, so a UI implementation must marshal to its own thread.
type Progress func(step, total int, desc string)

// Steps returns how many progress steps a backup of n Centers takes: one per
// Center snapshot, then one for writing the archive. The manifest is written
// into the archive as part of that final step, so it is not counted separately.
func Steps(n int) int { return n + 1 }

// Result describes a completed backup.
type Result struct {
	// Path is the archive that was written.
	Path string
	// Manifest is what went into it.
	Manifest Manifest
	// Pruned lists archives deleted to honour the retention count.
	Pruned []string
}

// Run snapshots every Center in centers, writes them into a single zip archive
// in destDir alongside a JSON manifest, then prunes old archives so at most
// keep remain. version is stamped into the manifest.
//
// Snapshots are written to a temporary directory and the archive is assembled
// under a temporary name, then renamed into place, so an interrupted run never
// leaves a partial archive that looks complete. A keep of zero or less disables
// pruning.
//
// onProgress may be nil. Run is safe to call with no Centers, producing an
// archive holding only a manifest.
func Run(ctx context.Context, centers []center.Center, destDir, version string, keep int, onProgress Progress) (Result, error) {
	if destDir == "" {
		return Result{}, fmt.Errorf("no backup directory is configured")
	}
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return Result{}, fmt.Errorf("create backup directory: %w", err)
	}

	// Snapshots land here and are removed once the archive is written; they are
	// a full second copy of every database, so they must not outlive the run.
	tmpDir, err := os.MkdirTemp(destDir, ".checkin-backup-*")
	if err != nil {
		return Result{}, fmt.Errorf("create temporary directory: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	total := Steps(len(centers))
	report := func(step int, desc string) {
		if onProgress != nil {
			onProgress(step, total, desc)
		}
	}

	now := time.Now()
	man := Manifest{Version: version, CreatedAt: now}
	snapshots := make([]string, 0, len(centers))

	for i, c := range centers {
		if err := ctx.Err(); err != nil {
			return Result{}, err
		}
		snapPath := filepath.Join(tmpDir, snapshotName(c.Name))
		info, err := snapshotCenter(ctx, c, snapPath)
		if err != nil {
			return Result{}, fmt.Errorf("back up Center %q: %w", c.Name, err)
		}
		snapshots = append(snapshots, snapPath)
		man.Centers = append(man.Centers, info)
		slog.Info("center snapshot taken", "center", c.Name,
			"bytes", info.SizeBytes, "students", info.StudentCount)
		report(i+1, fmt.Sprintf("Backed up %s", c.Name))
	}

	archivePath := filepath.Join(destDir, ArchivePrefix+now.Format(archiveTimeFormat)+archiveExt)
	if err := writeArchive(archivePath, man, snapshots); err != nil {
		return Result{}, err
	}
	report(total, "Archived")
	slog.Info("backup written", "path", archivePath, "centers", len(man.Centers))

	// Pruning failing does not invalidate the backup that was just written, so
	// it is reported but does not fail the run.
	pruned, err := prune(destDir, keep)
	if err != nil {
		slog.Warn("could not prune old backups", "dir", destDir, "err", err)
	}
	for _, p := range pruned {
		slog.Info("old backup pruned", "path", p)
	}

	return Result{Path: archivePath, Manifest: man, Pruned: pruned}, nil
}

// snapshotName is the file name for a Center's snapshot. Centers live in their
// own directories, so their names are already filesystem-safe and unique --
// see center.ValidateName -- and can be used as file names as they are.
// snapshotCenter nests this under "centers/" inside the archive, leaving the
// archive root for the manifest.
func snapshotName(centerName string) string {
	return centerName + ".db"
}

// writeArchive builds the zip at path from the manifest and the snapshot files,
// writing to a temporary name and renaming into place so a partial archive is
// never left behind under the real name. The rename is within destDir, so it is
// a same-filesystem rename and therefore atomic.
func writeArchive(path string, man Manifest, snapshots []string) error {
	tmpPath := path + ".partial"
	f, err := os.Create(tmpPath)
	if err != nil {
		return fmt.Errorf("create archive: %w", err)
	}
	// Both closers are best-effort on the error paths below; the deferred
	// remove is what guarantees no ".partial" file survives a failure.
	defer os.Remove(tmpPath)

	zw := zip.NewWriter(f)

	manBytes, err := json.MarshalIndent(man, "", "  ")
	if err != nil {
		f.Close()
		return fmt.Errorf("encode manifest: %w", err)
	}
	mw, err := zw.Create(ManifestName)
	if err != nil {
		f.Close()
		return fmt.Errorf("add manifest: %w", err)
	}
	if _, err := mw.Write(manBytes); err != nil {
		f.Close()
		return fmt.Errorf("write manifest: %w", err)
	}

	for i, snap := range snapshots {
		name := man.Centers[i].File
		if err := addFile(zw, snap, name); err != nil {
			f.Close()
			return fmt.Errorf("add %s to archive: %w", name, err)
		}
	}

	if err := zw.Close(); err != nil {
		f.Close()
		return fmt.Errorf("finish archive: %w", err)
	}
	// Flush to disk before the rename: a rename that lands ahead of the data
	// would publish a complete-looking archive with missing contents.
	if err := f.Sync(); err != nil {
		f.Close()
		return fmt.Errorf("flush archive: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close archive: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("finalize archive: %w", err)
	}
	return nil
}

// addFile copies src into the archive under name, compressed. SQLite pages
// compress well, so the archive is typically a fraction of the snapshot size.
func addFile(zw *zip.Writer, src, name string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	w, err := zw.CreateHeader(&zip.FileHeader{
		Name:     name,
		Method:   zip.Deflate,
		Modified: time.Now(),
	})
	if err != nil {
		return err
	}
	_, err = io.Copy(w, in)
	return err
}

// List returns this app's backup archives in dir, newest first. Files that are
// not backup archives are ignored, so a user's own files in the backup
// directory are neither listed nor at risk from pruning. A missing directory is
// not an error: it simply has no backups yet.
func List(dir string) ([]os.DirEntry, error) {
	if dir == "" {
		return nil, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []os.DirEntry
	for _, e := range entries {
		if e.IsDir() || !isArchiveName(e.Name()) {
			continue
		}
		out = append(out, e)
	}
	// Names embed a sortable timestamp, so sorting by name descending is
	// newest-first without stat-ing every file.
	sort.Slice(out, func(i, j int) bool { return out[i].Name() > out[j].Name() })
	return out, nil
}

// LastBackupTime returns the timestamp of the newest archive in dir, taken from
// its file name. ok is false when there are no backups yet.
func LastBackupTime(dir string) (t time.Time, ok bool) {
	archives, err := List(dir)
	if err != nil || len(archives) == 0 {
		return time.Time{}, false
	}
	return timeFromName(archives[0].Name())
}

// isArchiveName reports whether name is one of this app's backup archives.
// The timestamp must parse, so a ".partial" file from an interrupted run and
// any unrelated zip are both excluded.
func isArchiveName(name string) bool {
	_, ok := timeFromName(name)
	return ok
}

// timeFromName extracts the timestamp encoded in an archive's file name.
func timeFromName(name string) (time.Time, bool) {
	if !strings.HasPrefix(name, ArchivePrefix) || !strings.HasSuffix(name, archiveExt) {
		return time.Time{}, false
	}
	stamp := strings.TrimSuffix(strings.TrimPrefix(name, ArchivePrefix), archiveExt)
	t, err := time.ParseInLocation(archiveTimeFormat, stamp, time.Local)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

// prune deletes the oldest archives in dir until at most keep remain, returning
// the paths removed. keep <= 0 disables pruning, so a misconfigured retention
// count never deletes everything.
func prune(dir string, keep int) ([]string, error) {
	if keep <= 0 {
		return nil, nil
	}
	archives, err := List(dir)
	if err != nil {
		return nil, err
	}
	if len(archives) <= keep {
		return nil, nil
	}
	var pruned []string
	for _, e := range archives[keep:] {
		path := filepath.Join(dir, e.Name())
		if err := os.Remove(path); err != nil {
			return pruned, err
		}
		pruned = append(pruned, path)
	}
	return pruned, nil
}
