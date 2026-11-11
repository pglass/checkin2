package backup

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/pglass/checkin/internal/center"
)

// InternalDir returns the internal backup directory for appDir: a sibling of
// it named "<appDir>-backups" (for example ".../checkin-backups"). Every backup
// is written and fully verified here first, then copied to the external
// directories.
//
// A sibling rather than a subdirectory: every directory inside appDir is a
// Center, so a backups folder there would show up on the selection screen.
func InternalDir(appDir string) string {
	appDir = filepath.Clean(appDir)
	return filepath.Join(filepath.Dir(appDir), filepath.Base(appDir)+"-backups")
}

// CopyJob is one archive to be copied into one external directory.
type CopyJob struct {
	Archive Archive
	DestDir string
}

// CopyFailure is a copy that did not happen: either one archive failed, or the
// whole directory was unavailable (Archive is empty then).
type CopyFailure struct {
	DestDir string
	Archive string
	Err     error
}

// CopyResult describes the copies made to the external directories.
type CopyResult struct {
	// Dirs are the external directories considered, in configured order, with
	// repeats and the internal directory itself left out.
	Dirs []string
	// Copied are the copies that were made and checked.
	Copied []CopyJob
	// Failures are the copies that were not.
	Failures []CopyFailure
}

// OK reports whether every external directory received every archive.
func (r CopyResult) OK() bool { return len(r.Failures) == 0 }

// copyPlan is what planCopies found: the archives each available directory is
// missing, and the directories that could not be used at all.
type copyPlan struct {
	dirs   []string
	jobs   []CopyJob
	failed []CopyFailure
}

// planCopies lists the archives in srcDir missing from each of destDirs.
// External directories are add-only: an archive already there (by name) is left
// as it is, and nothing is ever deleted from them, so a drive that has been in
// a drawer for a year keeps every archive it has.
//
// A directory that cannot be listed or created is recorded as failed rather
// than stopping the plan, so one unplugged drive does not hold up the rest.
func planCopies(srcDir string, destDirs []string) copyPlan {
	var p copyPlan
	src, err := ListArchives(srcDir)
	if err != nil {
		// Nothing can be copied without the source listing, so every
		// directory fails with the same reason.
		for _, d := range uniqueDestDirs(srcDir, destDirs) {
			p.dirs = append(p.dirs, d)
			p.failed = append(p.failed, CopyFailure{DestDir: d, Err: err})
		}
		return p
	}
	for _, d := range uniqueDestDirs(srcDir, destDirs) {
		p.dirs = append(p.dirs, d)
		if err := ensureDestDir(d); err != nil {
			p.failed = append(p.failed, CopyFailure{DestDir: d, Err: err})
			continue
		}
		have, err := List(d)
		if err != nil {
			p.failed = append(p.failed, CopyFailure{DestDir: d, Err: err})
			continue
		}
		present := make(map[string]bool, len(have))
		for _, e := range have {
			present[e.Name()] = true
		}
		// Oldest first, so an interrupted run leaves the drive with a
		// contiguous history rather than only the newest archives.
		for i := len(src) - 1; i >= 0; i-- {
			if !present[src[i].Name] {
				p.jobs = append(p.jobs, CopyJob{Archive: src[i], DestDir: d})
			}
		}
	}
	return p
}

// uniqueDestDirs drops repeats and the internal directory from destDirs, keeping
// their order. Copying the internal directory onto itself would be pointless,
// and a directory listed twice would only be copied to twice.
func uniqueDestDirs(srcDir string, destDirs []string) []string {
	seen := map[string]bool{filepath.Clean(srcDir): true}
	var out []string
	for _, d := range destDirs {
		key := filepath.Clean(d)
		if d == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, d)
	}
	return out
}

// errDirUnavailable is reported for an external directory whose parent does not
// exist, which is what an unplugged drive looks like.
var errDirUnavailable = errors.New("directory is not available (is the drive connected?)")

// ensureDestDir makes sure dir exists, creating only the last path element.
// MkdirAll is deliberately not used: with a removable drive unplugged, it would
// recreate the drive's mount path on the system disk (/Volumes/Backup on macOS)
// and quietly fill that with backups instead of reporting the drive missing.
func ensureDestDir(dir string) error {
	info, err := os.Stat(dir)
	if err == nil {
		if !info.IsDir() {
			return fmt.Errorf("%s is not a directory", dir)
		}
		return nil
	}
	if !os.IsNotExist(err) {
		return err
	}
	if _, err := os.Stat(filepath.Dir(dir)); err != nil {
		return errDirUnavailable
	}
	if err := os.Mkdir(dir, 0o755); err != nil && !os.IsExist(err) {
		return fmt.Errorf("create backup directory: %w", err)
	}
	return nil
}

// copyArchive copies a into destDir and checks the copy's SHA-256 against the
// source. The archive was fully verified when it was written to the internal
// directory, so a matching hash is all a copy needs to be just as good.
//
// The copy is written under a ".partial" name and renamed into place only once
// it checks out, so an interrupted or bad copy never looks like an archive and
// the next run tries it again.
func copyArchive(a Archive, destDir string) error {
	dest := filepath.Join(destDir, a.Name)
	tmp := dest + ".partial"
	// Removes the partial file on any failure; after the rename it is gone and
	// this is a no-op.
	defer os.Remove(tmp)

	srcSum, err := copyAndHash(a.Path, tmp)
	if err != nil {
		return err
	}
	dstSum, err := hashFile(tmp)
	if err != nil {
		return fmt.Errorf("read back copy: %w", err)
	}
	if !bytes.Equal(srcSum, dstSum) {
		return errors.New("copy does not match the original (SHA-256 differs)")
	}
	if err := os.Rename(tmp, dest); err != nil {
		return fmt.Errorf("finalize copy: %w", err)
	}
	return nil
}

// copyAndHash copies src to dst, flushed to disk, returning src's SHA-256.
func copyAndHash(src, dst string) ([]byte, error) {
	in, err := os.Open(src)
	if err != nil {
		return nil, err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return nil, err
	}
	h := sha256.New()
	if _, err := io.Copy(io.MultiWriter(out, h), in); err != nil {
		out.Close()
		return nil, err
	}
	// Flushed before the hash is re-read and the file renamed into place, as
	// writeArchive does: a rename landing ahead of the data would publish a
	// complete-looking archive with missing contents.
	if err := out.Sync(); err != nil {
		out.Close()
		return nil, err
	}
	if err := out.Close(); err != nil {
		return nil, err
	}
	return h.Sum(nil), nil
}

// hashFile returns the SHA-256 of the file at path.
func hashFile(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return nil, err
	}
	return h.Sum(nil), nil
}

// CopyToDirs copies every archive in srcDir that is missing from each of
// destDirs. Failures are collected rather than returned: an unavailable drive
// or a failed copy does not stop the others. A cancelled ctx stops it between
// copies.
//
// onProgress may be nil. It is called as each copy begins, numbered from 1, so
// a caller can show "Copy backup <name> to <dir>".
func CopyToDirs(ctx context.Context, srcDir string, destDirs []string, onProgress Progress) CopyResult {
	p := planCopies(srcDir, destDirs)
	return runCopies(ctx, p, 0, len(p.jobs), onProgress)
}

// runCopies carries out plan's copies, numbering progress from offset+1 out of
// total. CopyToDirs and RunAndCopy both use it, the latter to continue the
// backup's own step numbering.
func runCopies(ctx context.Context, p copyPlan, offset, total int, onProgress Progress) CopyResult {
	res := CopyResult{Dirs: p.dirs, Failures: p.failed}
	for _, f := range p.failed {
		slog.Warn("backup directory unavailable", "dir", f.DestDir, "err", f.Err)
	}
	for i, job := range p.jobs {
		if err := ctx.Err(); err != nil {
			res.Failures = append(res.Failures, CopyFailure{DestDir: job.DestDir, Archive: job.Archive.Name, Err: err})
			continue
		}
		if onProgress != nil {
			onProgress(offset+i+1, total, fmt.Sprintf("Copy backup %s to %s", job.Archive.Name, job.DestDir))
		}
		if err := copyArchive(job.Archive, job.DestDir); err != nil {
			slog.Warn("backup copy failed", "archive", job.Archive.Name, "dir", job.DestDir, "err", err)
			res.Failures = append(res.Failures, CopyFailure{DestDir: job.DestDir, Archive: job.Archive.Name, Err: err})
			continue
		}
		slog.Info("backup copied", "archive", job.Archive.Name, "dir", job.DestDir)
		res.Copied = append(res.Copied, job)
	}
	return res
}

// RunAndCopy takes a backup into internalDir with Run, then copies every
// archive missing from each of destDirs (the new one included) with CopyToDirs.
//
// Progress continues Run's step numbering through the copies. The total is
// estimated before the backup from what is missing then, plus one new archive
// per directory; it is recalculated once the backup is written, so a total
// passed to onProgress can change between calls, and a caller should take it
// from each call rather than fixing it at the start.
//
// The error is Run's: copies are only attempted after a backup that wrote and
// verified, and copy failures are reported in Result.Copies, not as an error,
// because the backup itself is safe in internalDir either way.
func RunAndCopy(ctx context.Context, centers []center.Center, internalDir string, destDirs []string, version string, onProgress Progress) (Result, error) {
	runSteps := Steps(len(centers))
	before := planCopies(internalDir, destDirs)
	estimate := runSteps + len(before.jobs) + (len(before.dirs) - len(before.failed))

	res, err := Run(ctx, centers, internalDir, version, func(step, _ int, desc string) {
		if onProgress != nil {
			onProgress(step, estimate, desc)
		}
	})
	if err != nil {
		return res, err
	}

	p := planCopies(internalDir, destDirs)
	started := time.Now()
	res.Copies = runCopies(ctx, p, runSteps, runSteps+len(p.jobs), onProgress)
	if len(p.jobs) > 0 {
		slog.Info("backups copied", "copies", len(res.Copies.Copied),
			"failures", len(res.Copies.Failures), "elapsed", time.Since(started))
	}
	return res, nil
}
