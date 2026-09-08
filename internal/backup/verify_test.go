package backup

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pglass/checkin/internal/center"
)

// makeBackup runs a backup over freshly seeded Centers and returns its archive.
func makeBackup(t *testing.T, centers map[string]int) string {
	t.Helper()
	appDir, destDir := t.TempDir(), t.TempDir()
	for name, n := range centers {
		newCenter(t, appDir, name, n)
	}
	list, err := center.List(appDir)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	res, err := Run(context.Background(), list, destDir, "test", 7, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	return res.Path
}

// rewriteArchive rebuilds an archive, passing each entry's bytes through edit.
// Returning nil from edit keeps the entry unchanged.
func rewriteArchive(t *testing.T, path string, edit func(name string, data []byte) []byte) {
	t.Helper()
	zr, err := zip.OpenReader(path)
	if err != nil {
		t.Fatalf("open archive: %v", err)
	}

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatalf("open %s: %v", f.Name, err)
		}
		data, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			t.Fatalf("read %s: %v", f.Name, err)
		}
		if replacement := edit(f.Name, data); replacement != nil {
			data = replacement
		}
		w, err := zw.Create(f.Name)
		if err != nil {
			t.Fatalf("create %s: %v", f.Name, err)
		}
		if _, err := w.Write(data); err != nil {
			t.Fatalf("write %s: %v", f.Name, err)
		}
	}
	zr.Close()
	if err := zw.Close(); err != nil {
		t.Fatalf("close archive: %v", err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatalf("rewrite archive: %v", err)
	}
}

// A sound archive passes every check.
func TestVerifyPassesForASoundArchive(t *testing.T) {
	path := makeBackup(t, map[string]int{"Alpha": 3, "Beta": 1})

	res, err := Verify(context.Background(), path, nil)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if !res.OK() {
		t.Fatalf("sound archive failed: %v", res.Failures())
	}
	if got, want := len(res.Results), 2*len(Checks); got != want {
		t.Errorf("ran %d checks, want %d (every check for every Center)", got, want)
	}
	if !strings.Contains(VerifySummary(res), "sound") {
		t.Errorf("summary = %q, want it to say the backup is sound", VerifySummary(res))
	}
}

// A snapshot altered in place without changing its length passes the size check
// and is caught by the checksum. This is the case size alone cannot catch, and
// the reason the hash is recorded.
func TestVerifyCatchesTamperedContentOfTheSameLength(t *testing.T) {
	path := makeBackup(t, map[string]int{"Alpha": 3})

	var originalLen int
	rewriteArchive(t, path, func(name string, data []byte) []byte {
		if !strings.HasPrefix(name, "centers/") {
			return nil
		}
		originalLen = len(data)
		// Flip bytes deep inside the file, leaving its length untouched.
		altered := append([]byte(nil), data...)
		for i := len(altered) / 2; i < len(altered)/2+64 && i < len(altered); i++ {
			altered[i] ^= 0xFF
		}
		return altered
	})

	res, err := Verify(context.Background(), path, nil)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if res.OK() {
		t.Fatal("a tampered snapshot passed verification")
	}

	var sizeFailed, integrityFailed bool
	for _, f := range res.Failures() {
		switch f.Check {
		case CheckSize:
			sizeFailed = true
		case CheckIntegrity:
			integrityFailed = true
		}
	}
	if sizeFailed {
		t.Errorf("the size check failed, but the file is still %d bytes; "+
			"this case is meant to be caught by the checksum", originalLen)
	}
	if !integrityFailed {
		t.Error("the checksum did not catch a snapshot altered in place")
	}
}

// A truncated snapshot is caught by the size check.
func TestVerifyCatchesTruncatedSnapshot(t *testing.T) {
	path := makeBackup(t, map[string]int{"Alpha": 3})

	rewriteArchive(t, path, func(name string, data []byte) []byte {
		if !strings.HasPrefix(name, "centers/") {
			return nil
		}
		return data[:len(data)/2]
	})

	res, err := Verify(context.Background(), path, nil)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if res.OK() {
		t.Fatal("a truncated snapshot passed verification")
	}
	if !hasFailure(res, CheckSize) {
		t.Errorf("the size check did not catch a truncated snapshot: %v", res.Failures())
	}
}

// A manifest claiming more rows than the snapshot holds is caught by the row
// counts, which is what protects against a snapshot that is internally valid
// but not the one the manifest describes.
func TestVerifyCatchesRowCountMismatch(t *testing.T) {
	path := makeBackup(t, map[string]int{"Alpha": 3})

	rewriteArchive(t, path, func(name string, data []byte) []byte {
		if name != ManifestName {
			return nil
		}
		var man Manifest
		if err := json.Unmarshal(data, &man); err != nil {
			t.Fatalf("decode manifest: %v", err)
		}
		man.Centers[0].StudentCount = 99
		man.Centers[0].LogCount = 1234
		out, err := json.Marshal(man)
		if err != nil {
			t.Fatalf("encode manifest: %v", err)
		}
		return out
	})

	res, err := Verify(context.Background(), path, nil)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if res.OK() {
		t.Fatal("a manifest disagreeing with its snapshot passed verification")
	}
	if !hasFailure(res, CheckStudentCount) {
		t.Errorf("the Student count check did not catch the mismatch: %v", res.Failures())
	}
	if !hasFailure(res, CheckLogCount) {
		t.Errorf("the Log count check did not catch the mismatch: %v", res.Failures())
	}
	// The failure names the numbers, so the user can see how far off it is.
	for _, f := range res.Failures() {
		if f.Check == CheckStudentCount && !strings.Contains(f.Detail, "99") {
			t.Errorf("detail = %q, want it to name the expected count", f.Detail)
		}
	}
}

// A Log row count is verified, not just the Student count: the log is the bulk
// of the data and the part a partial write would lose first.
func TestVerifyChecksLogRowsAreRecorded(t *testing.T) {
	// Every seeded student writes an "Added" log row, so a Center with
	// students has a non-zero log to check.
	path := makeBackup(t, map[string]int{"Alpha": 4})

	man, err := readManifest(path)
	if err != nil {
		t.Fatalf("readManifest: %v", err)
	}
	if man.Centers[0].LogCount != 4 {
		t.Errorf("manifest LogCount = %d, want 4 (one Added row per student)",
			man.Centers[0].LogCount)
	}
	if man.Centers[0].SHA256 == "" {
		t.Error("manifest has no SHA-256 for the snapshot")
	}
}

// A file that is not a backup is rejected with an explanation, rather than
// reported as a failed backup.
func TestVerifyRejectsAForeignZip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "not-a-backup.zip")

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create("hello.txt")
	w.Write([]byte("not a backup"))
	zw.Close()
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	if _, err := Verify(context.Background(), path, nil); err == nil {
		t.Fatal("a zip with no manifest verified successfully")
	} else if !strings.Contains(err.Error(), ManifestName) {
		t.Errorf("error = %q, want it to name the missing manifest", err)
	}
}

// An archive from before checksums were recorded says so, rather than being
// reported as corrupt.
func TestVerifyExplainsAMissingChecksum(t *testing.T) {
	path := makeBackup(t, map[string]int{"Alpha": 2})

	rewriteArchive(t, path, func(name string, data []byte) []byte {
		if name != ManifestName {
			return nil
		}
		var man Manifest
		json.Unmarshal(data, &man)
		man.Centers[0].SHA256 = "" // as an older archive would have
		out, _ := json.Marshal(man)
		return out
	})

	res, err := Verify(context.Background(), path, nil)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if res.OK() {
		t.Fatal("an archive with no checksum reported as fully verified")
	}
	for _, f := range res.Failures() {
		if f.Check == CheckIntegrity && !strings.Contains(f.Detail, "before checksums") {
			t.Errorf("detail = %q, want it to explain the checksum is missing", f.Detail)
		}
	}
}

// Progress is reported once per step, numbered 1..VerifySteps, naming each
// Center and check.
func TestVerifyReportsEveryStep(t *testing.T) {
	path := makeBackup(t, map[string]int{"Alpha": 1, "Beta": 1})

	var steps []int
	var descs []string
	var total int
	if _, err := Verify(context.Background(), path,
		func(step, tot int, desc string) {
			steps = append(steps, step)
			descs = append(descs, desc)
			total = tot
		}); err != nil {
		t.Fatalf("Verify: %v", err)
	}

	want := VerifySteps(2)
	if total != want {
		t.Errorf("total = %d, want %d", total, want)
	}
	if len(steps) != want {
		t.Fatalf("got %d progress calls, want %d: %v", len(steps), want, descs)
	}
	for i, s := range steps {
		if s != i+1 {
			t.Errorf("call %d reported step %d, want %d", i, s, i+1)
		}
	}
	if descs[0] != "Unzipping archive" {
		t.Errorf("first step = %q, want the unzip step", descs[0])
	}
}

// hasFailure reports whether a check of the given kind failed.
func hasFailure(res VerifyResult, check Check) bool {
	for _, f := range res.Failures() {
		if f.Check == check {
			return true
		}
	}
	return false
}
