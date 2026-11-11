package backup

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/pglass/checkin/internal/center"
)

// seedNamedArchives writes placeholder archives with the given timestamps into
// dir, each holding its own stamp so copies can be told apart.
func seedNamedArchives(t *testing.T, dir string, stamps ...string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, s := range stamps {
		if err := os.WriteFile(filepath.Join(dir, ArchivePrefix+s+archiveExt), []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// archiveNames lists the archive file names in dir, newest first.
func archiveNames(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := List(dir)
	if err != nil {
		t.Fatalf("List %s: %v", dir, err)
	}
	var out []string
	for _, e := range entries {
		out = append(out, e.Name())
	}
	return out
}

func TestInternalDirIsSiblingOfAppDir(t *testing.T) {
	appDir := filepath.Join("/Users", "x", "Library", "Application Support", "checkin")
	want := filepath.Join("/Users", "x", "Library", "Application Support", "checkin-backups")
	if got := InternalDir(appDir); got != want {
		t.Errorf("InternalDir = %q, want %q", got, want)
	}
	if got := InternalDir(appDir + string(filepath.Separator)); got != want {
		t.Errorf("InternalDir with trailing separator = %q, want %q", got, want)
	}
}

// A new directory receives every archive, each byte-for-byte, with no partial
// files left behind.
func TestCopyToDirsPopulatesNewDir(t *testing.T) {
	src := t.TempDir()
	seedNamedArchives(t, src, "2025-01-01-000000", "2025-02-01-000000", "2025-03-01-000000")
	dest := filepath.Join(t.TempDir(), "usb-backups") // does not exist yet

	var descs []string
	res := CopyToDirs(context.Background(), src, []string{dest}, func(step, total int, desc string) {
		descs = append(descs, desc)
		if total != 3 {
			t.Errorf("total = %d, want 3", total)
		}
	})
	if !res.OK() {
		t.Fatalf("failures: %+v", res.Failures)
	}
	if len(res.Copied) != 3 {
		t.Fatalf("copied %d, want 3", len(res.Copied))
	}
	if got, want := archiveNames(t, dest), archiveNames(t, src); !slices.Equal(got, want) {
		t.Errorf("dest archives = %q, want %q", got, want)
	}
	for _, name := range archiveNames(t, src) {
		a, _ := os.ReadFile(filepath.Join(src, name))
		b, _ := os.ReadFile(filepath.Join(dest, name))
		if !bytes.Equal(a, b) {
			t.Errorf("%s differs from the original", name)
		}
	}
	entries, _ := os.ReadDir(dest)
	if len(entries) != 3 {
		t.Errorf("dest holds %d files, want only the 3 archives", len(entries))
	}
	// Oldest first, named with the archive and destination.
	if len(descs) == 0 || descs[0] != "Copy backup "+ArchivePrefix+"2025-01-01-000000"+archiveExt+" to "+dest {
		t.Errorf("first progress = %q", descs)
	}
}

// External directories are add-only: archives already there, including ones
// the internal directory no longer has, are left exactly as they are.
func TestCopyToDirsIsAddOnly(t *testing.T) {
	src := t.TempDir()
	seedNamedArchives(t, src, "2025-02-01-000000", "2025-03-01-000000")
	dest := t.TempDir()
	// An old archive only the drive has, and one both have but with other bytes.
	seedNamedArchives(t, dest, "2024-01-01-000000")
	same := filepath.Join(dest, ArchivePrefix+"2025-02-01-000000"+archiveExt)
	if err := os.WriteFile(same, []byte("drive's own copy"), 0o644); err != nil {
		t.Fatal(err)
	}

	res := CopyToDirs(context.Background(), src, []string{dest}, nil)
	if !res.OK() {
		t.Fatalf("failures: %+v", res.Failures)
	}
	if len(res.Copied) != 1 || res.Copied[0].Archive.Name != ArchivePrefix+"2025-03-01-000000"+archiveExt {
		t.Errorf("copied %+v, want only the missing 2025-03 archive", res.Copied)
	}
	want := []string{
		ArchivePrefix + "2025-03-01-000000" + archiveExt,
		ArchivePrefix + "2025-02-01-000000" + archiveExt,
		ArchivePrefix + "2024-01-01-000000" + archiveExt,
	}
	if got := archiveNames(t, dest); !slices.Equal(got, want) {
		t.Errorf("dest archives = %q, want %q", got, want)
	}
	if b, _ := os.ReadFile(same); string(b) != "drive's own copy" {
		t.Errorf("existing archive was overwritten: %q", b)
	}
}

// A directory whose parent is missing -- an unplugged drive -- fails on its
// own, is not created, and does not stop the other directories.
func TestCopyToDirsSkipsUnavailableDir(t *testing.T) {
	src := t.TempDir()
	seedNamedArchives(t, src, "2025-01-01-000000")
	gone := filepath.Join(t.TempDir(), "NotMounted", "backups")
	ok := t.TempDir()

	res := CopyToDirs(context.Background(), src, []string{gone, ok}, nil)
	if len(res.Failures) != 1 || res.Failures[0].DestDir != gone || res.Failures[0].Err != errDirUnavailable {
		t.Errorf("failures = %+v, want one errDirUnavailable for %s", res.Failures, gone)
	}
	if _, err := os.Stat(filepath.Dir(gone)); !os.IsNotExist(err) {
		t.Errorf("unavailable drive path was created: %v", err)
	}
	if len(archiveNames(t, ok)) != 1 {
		t.Error("available directory did not receive the archive")
	}
	if !slices.Equal(res.Dirs, []string{gone, ok}) {
		t.Errorf("Dirs = %q, want both, in order", res.Dirs)
	}
}

// The internal directory itself and repeated directories are not copied to.
func TestCopyToDirsSkipsSourceAndRepeats(t *testing.T) {
	src := t.TempDir()
	seedNamedArchives(t, src, "2025-01-01-000000")
	dest := t.TempDir()

	res := CopyToDirs(context.Background(), src, []string{src, dest, dest + string(filepath.Separator)}, nil)
	if !res.OK() {
		t.Fatalf("failures: %+v", res.Failures)
	}
	if !slices.Equal(res.Dirs, []string{dest}) {
		t.Errorf("Dirs = %q, want only %s", res.Dirs, dest)
	}
	if len(res.Copied) != 1 {
		t.Errorf("copied %d, want 1", len(res.Copied))
	}
}

// RunAndCopy writes and verifies the backup in the internal directory, then
// copies it, along with anything else missing, to each backup directory.
// Progress runs through every step and ends on its own total.
func TestRunAndCopy(t *testing.T) {
	appDir := t.TempDir()
	newCenter(t, appDir, "Alpha", 2)
	centers, err := center.List(appDir)
	if err != nil {
		t.Fatal(err)
	}
	internal := InternalDir(appDir)
	seedNamedArchives(t, internal, "2020-01-01-000000") // an older backup
	dest := t.TempDir()

	var lastStep, lastTotal int
	var descs []string
	res, err := RunAndCopy(context.Background(), centers, internal, []string{dest}, "test",
		func(step, total int, desc string) {
			if step <= lastStep {
				t.Errorf("step %d after %d", step, lastStep)
			}
			lastStep, lastTotal = step, total
			descs = append(descs, desc)
		})
	if err != nil {
		t.Fatalf("RunAndCopy: %v", err)
	}
	if filepath.Dir(res.Path) != internal {
		t.Errorf("archive written to %s, want the internal dir %s", res.Path, internal)
	}
	if !res.Copies.OK() || len(res.Copies.Copied) != 2 {
		t.Errorf("copies = %+v, want the new and the old archive copied", res.Copies)
	}
	if got, want := archiveNames(t, dest), archiveNames(t, internal); !slices.Equal(got, want) {
		t.Errorf("dest archives = %q, want %q", got, want)
	}
	srcSum, _ := hashFile(res.Path)
	dstSum, _ := hashFile(filepath.Join(dest, filepath.Base(res.Path)))
	if !bytes.Equal(srcSum, dstSum) {
		t.Error("copied archive differs from the original")
	}
	if lastStep != lastTotal {
		t.Errorf("progress ended at step %d of %d", lastStep, lastTotal)
	}
	if want := Steps(1) + 2; lastTotal != want {
		t.Errorf("total = %d, want %d", lastTotal, want)
	}
	if d := descs[len(descs)-1]; d != "Copy backup "+filepath.Base(res.Path)+" to "+dest {
		t.Errorf("last progress = %q, want the new archive's copy", d)
	}
}

// With no backup directories, RunAndCopy is just a backup.
func TestRunAndCopyWithoutBackupDirs(t *testing.T) {
	appDir := t.TempDir()
	newCenter(t, appDir, "Alpha", 1)
	centers, _ := center.List(appDir)

	res, err := RunAndCopy(context.Background(), centers, InternalDir(appDir), nil, "test", nil)
	if err != nil {
		t.Fatalf("RunAndCopy: %v", err)
	}
	if len(res.Copies.Dirs) != 0 || len(res.Copies.Copied) != 0 || !res.Copies.OK() {
		t.Errorf("copies = %+v, want none", res.Copies)
	}
	if len(archiveNames(t, InternalDir(appDir))) != 1 {
		t.Error("backup not written to the internal dir")
	}
}
