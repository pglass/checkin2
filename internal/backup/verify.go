package backup

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Check names one verification step for a Center's snapshot. They run in this
// order, cheapest and most-likely-to-fail first: a truncated archive is caught
// by size before the file is hashed, a file that fails its hash is not worth
// opening as a database, and a database that fails SQLite's own integrity check
// is not worth counting rows in.
type Check int

const (
	// CheckSize compares the extracted snapshot's size to the manifest.
	CheckSize Check = iota
	// CheckIntegrity compares its SHA-256 to the manifest.
	CheckIntegrity
	// CheckStudentCount opens it and compares the Student row count.
	CheckStudentCount
	// CheckLogCount compares the Log row count.
	CheckLogCount
	// CheckDatabase runs SQLite's PRAGMA integrity_check on it. Declared last
	// so existing values keep their numbers; Checks sets the order it runs in.
	CheckDatabase
)

// Checks lists the per-Center checks in the order Verify runs them.
var Checks = []Check{CheckSize, CheckIntegrity, CheckDatabase, CheckStudentCount, CheckLogCount}

// Label is the human-readable name of a check, used in progress messages.
func (c Check) Label() string {
	switch c {
	case CheckSize:
		return "Check file size"
	case CheckIntegrity:
		return "Check file integrity"
	case CheckStudentCount:
		return "Check Student count"
	case CheckLogCount:
		return "Check Log count"
	case CheckDatabase:
		return integrityCheckDesc
	}
	return "Check"
}

// VerifyProgress reports verification progress as each step *begins*, matching
// Progress. step is 1-based, total is VerifySteps for the same manifest, and
// desc names what is about to happen.
type VerifyProgress func(step, total int, desc string)

// VerifySteps returns how many steps verifying an archive of n Centers takes:
// one to extract the archive, then one per check per Center.
func VerifySteps(n int) int { return 1 + n*len(Checks) }

// CheckResult is the outcome of one check on one Center.
type CheckResult struct {
	// Center is the Center whose snapshot was checked.
	Center string
	// Check is which check ran.
	Check Check
	// OK reports whether it passed.
	OK bool
	// Detail explains a failure in the user's terms, and is empty when OK.
	Detail string
}

// VerifyResult is the outcome of verifying one archive.
type VerifyResult struct {
	// Path is the archive that was verified.
	Path string
	// Manifest is what the archive claims to hold.
	Manifest Manifest
	// Results holds every check that ran, in order.
	Results []CheckResult
}

// OK reports whether every check passed.
func (r VerifyResult) OK() bool {
	for _, c := range r.Results {
		if !c.OK {
			return false
		}
	}
	return true
}

// Failures returns just the checks that failed.
func (r VerifyResult) Failures() []CheckResult {
	var out []CheckResult
	for _, c := range r.Results {
		if !c.OK {
			out = append(out, c)
		}
	}
	return out
}

// Verify extracts archivePath to a temporary directory and re-derives every
// value the manifest recorded at backup time, comparing each one.
//
// A failed check is data, not an error: it is recorded in the result and
// verification continues, so the user is told everything that is wrong with an
// archive in one pass rather than one problem at a time. An error return means
// verification could not run at all -- an unreadable archive, a missing
// manifest -- which is different from an archive that ran and failed.
//
// onProgress may be nil.
func Verify(ctx context.Context, archivePath string, onProgress VerifyProgress) (VerifyResult, error) {
	tmpDir, err := os.MkdirTemp("", "checkin-verify-*")
	if err != nil {
		return VerifyResult{}, fmt.Errorf("create temporary directory: %w", err)
	}
	// The extracted snapshots are a full copy of every database, so they must
	// not outlive the check.
	defer os.RemoveAll(tmpDir)

	res := VerifyResult{Path: archivePath}

	// The step count depends on the manifest, which is not known until the
	// archive is open, so the extract step is reported against a total that
	// assumes no Centers and is corrected as soon as the manifest is read.
	report := func(step, total int, desc string) {
		if onProgress != nil {
			onProgress(step, total, desc)
		}
	}

	man, err := ReadManifest(archivePath)
	if err != nil {
		return VerifyResult{}, err
	}
	res.Manifest = man

	total := VerifySteps(len(man.Centers))
	report(1, total, "Unzipping archive")
	zr, err := zip.OpenReader(archivePath)
	if err != nil {
		return VerifyResult{}, fmt.Errorf("open archive: %w", err)
	}
	defer zr.Close()

	// Each Center's snapshot is extracted, checked, and deleted before the
	// next, so however many Centers the manifest lists, at most one snapshot
	// -- itself capped by entryLimit -- is on disk at a time.
	step := 1
	for i, ci := range man.Centers {
		if err := ctx.Err(); err != nil {
			return VerifyResult{}, err
		}
		// Numbered rather than named after the entry: the archive's own names
		// are untrusted, and a Center name only has to be unique, not safe as
		// a path component on this filesystem.
		path := filepath.Join(tmpDir, fmt.Sprintf("snapshot-%d.db", i))
		extractErr := extractNamed(zr, ci, path)
		for _, check := range Checks {
			step++
			report(step, total, fmt.Sprintf("Verifying %s backup: %s", ci.Name, check.Label()))

			if extractErr != nil {
				res.Results = append(res.Results, CheckResult{
					Center: ci.Name, Check: check, OK: false, Detail: extractErr.Error(),
				})
				continue
			}
			res.Results = append(res.Results, runCheck(ctx, check, ci, path))
		}
		os.Remove(path)
	}

	return res, nil
}

// runCheck performs one check against an extracted snapshot.
func runCheck(ctx context.Context, check Check, want CenterInfo, path string) CheckResult {
	r := CheckResult{Center: want.Name, Check: check}

	switch check {
	case CheckSize:
		st, err := os.Stat(path)
		if err != nil {
			r.Detail = err.Error()
			return r
		}
		if st.Size() != want.SizeBytes {
			r.Detail = fmt.Sprintf("expected %d bytes, found %d", want.SizeBytes, st.Size())
			return r
		}

	case CheckIntegrity:
		// An archive written before checksums were recorded has nothing to
		// compare against. Reporting that plainly beats failing an archive that
		// may be perfectly sound.
		if want.SHA256 == "" {
			r.Detail = "this backup was made before checksums were recorded, so its contents cannot be checked"
			return r
		}
		sum, err := fileSHA256(path)
		if err != nil {
			r.Detail = err.Error()
			return r
		}
		if sum != want.SHA256 {
			r.Detail = "the file's contents do not match the checksum recorded when the backup was made"
			return r
		}

	case CheckDatabase:
		if err := integrityCheck(ctx, path); err != nil {
			r.Detail = fmt.Sprintf("the database failed SQLite's integrity check: %v", err)
			return r
		}

	case CheckStudentCount:
		students, _, err := rowCounts(ctx, path)
		if err != nil {
			r.Detail = fmt.Sprintf("could not read the backup database: %v", err)
			return r
		}
		if students != want.StudentCount {
			r.Detail = fmt.Sprintf("expected %d students, found %d", want.StudentCount, students)
			return r
		}

	case CheckLogCount:
		_, logs, err := rowCounts(ctx, path)
		if err != nil {
			r.Detail = fmt.Sprintf("could not read the backup database: %v", err)
			return r
		}
		if logs != want.LogCount {
			r.Detail = fmt.Sprintf("expected %d log entries, found %d", want.LogCount, logs)
			return r
		}
	}

	r.OK = true
	return r
}

// ReadManifest reads and decodes an archive's manifest without extracting
// anything else, so a corrupt or foreign zip is rejected before any work. It is
// what the restore UI reads to list a backup's Centers without unpacking it.
func ReadManifest(archivePath string) (Manifest, error) {
	zr, err := zip.OpenReader(archivePath)
	if err != nil {
		return Manifest{}, fmt.Errorf("open archive: %w", err)
	}
	defer zr.Close()

	for _, f := range zr.File {
		if f.Name != ManifestName {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return Manifest{}, fmt.Errorf("read manifest: %w", err)
		}
		defer rc.Close()

		// Read no more than the limit (plus one byte, to tell "exactly the
		// limit" from "over it"): the manifest is untrusted until decoded, and
		// a real one is a few kilobytes.
		max := limits().manifest
		data, err := io.ReadAll(io.LimitReader(rc, max+1))
		if err != nil {
			return Manifest{}, fmt.Errorf("read manifest: %w", err)
		}
		if int64(len(data)) > max {
			return Manifest{}, fmt.Errorf("this backup's manifest is larger than the %s allowed (max_unzip_manifest_size_mb)", formatMB(max))
		}
		var man Manifest
		if err := json.Unmarshal(data, &man); err != nil {
			return Manifest{}, fmt.Errorf("read manifest: %w", err)
		}
		return man, nil
	}
	return Manifest{}, fmt.Errorf("this file is not a checkin backup: it has no %s", ManifestName)
}

// extractNamed writes the snapshot the manifest entry ci names to dest, which
// is chosen by the caller, never derived from the archive, so a crafted archive
// cannot write anywhere else. Only the first entry of that name is used, so an
// archive repeating a name cannot make it extract more than once.
//
// At most entryLimit(ci) bytes are written; see entryLimit.
func extractNamed(zr *zip.ReadCloser, ci CenterInfo, dest string) error {
	for _, f := range zr.File {
		if f.Name != ci.File {
			continue
		}
		limit := entryLimit(ci)
		err := extractOne(f, dest, limit)
		if errors.Is(err, errTooLarge) {
			if limit == ci.SizeBytes {
				return fmt.Errorf("the snapshot is larger than the %d bytes its manifest records", limit)
			}
			return fmt.Errorf("the snapshot is larger than the %s allowed (max_unzip_entry_size_mb)", formatMB(limit))
		}
		return err
	}
	return fmt.Errorf("the archive has no entry named %q", ci.File)
}

// errTooLarge is returned by extractOne for an entry over its limit.
var errTooLarge = errors.New("zip entry is over its size limit")

// entryLimit is the most a Center's snapshot may extract to: the size its
// manifest records, or the configured entry limit if that is smaller. The
// manifest's size alone is not enough, since the manifest is as untrusted as
// the rest of the archive; the configured limit is what bounds a crafted one.
// A larger file would fail the size check anyway, so stopping at the recorded
// size loses nothing.
func entryLimit(ci CenterInfo) int64 {
	max := limits().entry
	if ci.SizeBytes >= 0 && ci.SizeBytes < max {
		return ci.SizeBytes
	}
	return max
}

// extractOne writes one zip entry to dest, failing rather than writing more
// than limit bytes.
func extractOne(f *zip.File, dest string, limit int64) error {
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()

	w, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer w.Close()

	// One byte past the limit is read, to tell an entry of exactly the limit
	// from a larger one. zip.File.Open verifies the entry's CRC on EOF, so a
	// corrupt entry within the limit fails here rather than producing a short
	// file.
	n, err := io.Copy(w, io.LimitReader(rc, limit+1))
	if err != nil {
		return err
	}
	if n > limit {
		return errTooLarge
	}
	return w.Close()
}

// VerifySummary renders a result as a sentence for the UI: what passed, or what
// went wrong.
func VerifySummary(res VerifyResult) string {
	n := len(res.Manifest.Centers)
	noun := "Centers"
	if n == 1 {
		noun = "Center"
	}
	if res.OK() {
		return fmt.Sprintf("Verified %d %s. This backup is sound.", n, noun)
	}

	fails := res.Failures()
	lines := make([]string, 0, len(fails)+1)
	lines = append(lines, fmt.Sprintf("This backup has %d problem(s):", len(fails)))
	for _, f := range fails {
		lines = append(lines, fmt.Sprintf("• %s — %s: %s", f.Center, f.Check.Label(), f.Detail))
	}
	return strings.Join(lines, "\n")
}
