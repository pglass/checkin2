package backup

import (
	"archive/zip"
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pglass/checkin/internal/center"
	"github.com/pglass/checkin/internal/store"
)

// newCenter creates a Center in appDir and adds n students to its database,
// leaving the Center closed so a backup can take its lock.
func newCenter(t *testing.T, appDir, name string, n int) center.Center {
	t.Helper()
	c, err := center.Create(appDir, name)
	if err != nil {
		t.Fatalf("Create %s: %v", name, err)
	}
	if n == 0 {
		return c
	}

	st, err := store.Open(c.DBPath())
	if err != nil {
		t.Fatalf("Open %s: %v", name, err)
	}
	for i := range n {
		nm := store.NewName("First"+string(rune('A'+i)), "Last"+name)
		if _, err := st.AddStudent(context.Background(), nm); err != nil {
			t.Fatalf("AddStudent: %v", err)
		}
	}
	if err := st.Close(); err != nil {
		t.Fatalf("Close %s: %v", name, err)
	}
	return c
}

// readArchive returns an archive's manifest and the names of its entries.
func readArchive(t *testing.T, path string) (Manifest, map[string][]byte) {
	t.Helper()
	zr, err := zip.OpenReader(path)
	if err != nil {
		t.Fatalf("open archive: %v", err)
	}
	defer zr.Close()

	files := map[string][]byte{}
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatalf("open %s: %v", f.Name, err)
		}
		b, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			t.Fatalf("read %s: %v", f.Name, err)
		}
		files[f.Name] = b
	}

	var man Manifest
	raw, ok := files[ManifestName]
	if !ok {
		t.Fatalf("archive has no %s (entries: %v)", ManifestName, keys(files))
	}
	if err := json.Unmarshal(raw, &man); err != nil {
		t.Fatalf("decode manifest: %v", err)
	}
	return man, files
}

func keys(m map[string][]byte) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// A backup archives every Center, and the manifest describes each one.
func TestRunArchivesAllCenters(t *testing.T) {
	appDir, destDir := t.TempDir(), t.TempDir()
	newCenter(t, appDir, "Alpha", 3)
	newCenter(t, appDir, "Beta", 1)

	centers, err := center.List(appDir)
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	res, err := Run(context.Background(), centers, destDir, "1.2.3", 7, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	man, files := readArchive(t, res.Path)
	if man.Version != "1.2.3" {
		t.Errorf("manifest version = %q, want %q", man.Version, "1.2.3")
	}
	if len(man.Centers) != 2 {
		t.Fatalf("manifest has %d centers, want 2", len(man.Centers))
	}

	want := map[string]int64{"Alpha": 3, "Beta": 1}
	for _, ci := range man.Centers {
		if got := ci.StudentCount; got != want[ci.Name] {
			t.Errorf("%s student count = %d, want %d", ci.Name, got, want[ci.Name])
		}
		if ci.SizeBytes <= 0 {
			t.Errorf("%s size = %d, want > 0", ci.Name, ci.SizeBytes)
		}
		if _, ok := files[ci.File]; !ok {
			t.Errorf("manifest names %q but the archive has %v", ci.File, keys(files))
		}
	}
}

// The snapshot in the archive is a usable database holding the same students,
// which is the whole point: an archive that cannot be opened is not a backup.
func TestSnapshotInArchiveIsAUsableDatabase(t *testing.T) {
	appDir, destDir := t.TempDir(), t.TempDir()
	newCenter(t, appDir, "Alpha", 4)
	centers, _ := center.List(appDir)

	res, err := Run(context.Background(), centers, destDir, "test", 7, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	_, files := readArchive(t, res.Path)

	extracted := filepath.Join(t.TempDir(), "restored.db")
	if err := os.WriteFile(extracted, files["centers/Alpha.db"], 0o644); err != nil {
		t.Fatalf("write extracted db: %v", err)
	}

	db, err := sql.Open("sqlite", snapshotDSN(extracted))
	if err != nil {
		t.Fatalf("open restored: %v", err)
	}
	defer db.Close()

	var n int64
	if err := db.QueryRow(`SELECT count(*) FROM Student`).Scan(&n); err != nil {
		t.Fatalf("query restored: %v", err)
	}
	if n != 4 {
		t.Errorf("restored student count = %d, want 4", n)
	}
}

// Data committed but not yet checkpointed into the main database file still
// makes it into the backup. This is the WAL hazard VACUUM INTO exists to avoid:
// copying checkin.db alone here would produce a stale but valid-looking
// snapshot.
func TestSnapshotIncludesUncheckpointedWALData(t *testing.T) {
	appDir := t.TempDir()
	c := newCenter(t, appDir, "Alpha", 2)

	// Reopen and add a student, then take the snapshot while the -wal file
	// still holds it. store.Close checkpoints, so the write is done through a
	// separate connection that is left unclosed until after the snapshot.
	db, err := sql.Open("sqlite", snapshotDSN(c.DBPath()))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := db.Exec(`PRAGMA journal_mode=WAL`); err != nil {
		t.Fatalf("wal: %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO Student (FirstName, LastName) VALUES ('Wal', 'Pending')`); err != nil {
		t.Fatalf("insert: %v", err)
	}

	dest := filepath.Join(t.TempDir(), "snap.db")
	info, err := snapshotCenter(context.Background(), c, dest)
	if err != nil {
		t.Fatalf("snapshotCenter: %v", err)
	}
	db.Close()

	if info.StudentCount != 3 {
		t.Errorf("snapshot student count = %d, want 3 (the pending WAL row is missing)",
			info.StudentCount)
	}
	// A VACUUM INTO output is self-contained: no -wal beside it to pair up.
	if _, err := os.Stat(dest + "-wal"); !os.IsNotExist(err) {
		t.Errorf("snapshot has a -wal sidecar; it should be self-contained")
	}
}

// A Center that was created but never opened has no database file. It should be
// archived as an empty entry rather than failing the whole backup.
func TestRunHandlesCenterWithNoDatabase(t *testing.T) {
	appDir, destDir := t.TempDir(), t.TempDir()
	newCenter(t, appDir, "Empty", 0)
	centers, _ := center.List(appDir)

	res, err := Run(context.Background(), centers, destDir, "test", 7, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	man, files := readArchive(t, res.Path)
	if len(man.Centers) != 1 || man.Centers[0].StudentCount != 0 {
		t.Fatalf("manifest = %+v, want one center with no students", man.Centers)
	}
	if _, ok := files["centers/Empty.db"]; !ok {
		t.Errorf("archive is missing the empty Center's entry: %v", keys(files))
	}
}

// Progress is reported once per Center plus once for the archive, numbered 1..N
// against the total Steps predicts.
func TestRunReportsProgressForEveryStep(t *testing.T) {
	appDir, destDir := t.TempDir(), t.TempDir()
	newCenter(t, appDir, "Alpha", 1)
	newCenter(t, appDir, "Beta", 1)
	centers, _ := center.List(appDir)

	var steps []int
	var lastTotal int
	_, err := Run(context.Background(), centers, destDir, "test", 7,
		func(step, total int, _ string) { steps = append(steps, step); lastTotal = total })
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	want := Steps(2)
	if lastTotal != want {
		t.Errorf("total = %d, want %d", lastTotal, want)
	}
	if len(steps) != want {
		t.Fatalf("got %d progress calls %v, want %d", len(steps), steps, want)
	}
	for i, s := range steps {
		if s != i+1 {
			t.Errorf("progress call %d reported step %d, want %d", i, s, i+1)
		}
	}
}

// Each step is announced as it begins, not once it has finished, so the message
// names what is happening rather than what already happened. The Center's
// snapshot must not exist yet at the moment its step is reported.
func TestRunReportsStepsAtTheirStart(t *testing.T) {
	appDir, destDir := t.TempDir(), t.TempDir()
	newCenter(t, appDir, "Alpha", 1)
	newCenter(t, appDir, "Beta", 1)
	centers, _ := center.List(appDir)

	var descs []string
	// Archives present when each step was announced: the archive step must be
	// reported before the file exists.
	var archivesAtStep []int
	_, err := Run(context.Background(), centers, destDir, "test", 7,
		func(_, _ int, desc string) {
			descs = append(descs, desc)
			found, _ := List(destDir)
			archivesAtStep = append(archivesAtStep, len(found))
		})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	want := []string{"Backing up Alpha", "Backing up Beta", "Creating archive"}
	if len(descs) != len(want) {
		t.Fatalf("descriptions = %v, want %v", descs, want)
	}
	for i, w := range want {
		if descs[i] != w {
			t.Errorf("step %d description = %q, want %q", i+1, descs[i], w)
		}
	}
	// Reported before the work: no archive exists at any announcement,
	// including the archive step's own.
	for i, n := range archivesAtStep {
		if n != 0 {
			t.Errorf("at step %d (%q) there were already %d archives; "+
				"the step was reported after its work, not before", i+1, descs[i], n)
		}
	}
}

// Only backup_count archives are kept, oldest first to go.
func TestRunPrunesToBackupCount(t *testing.T) {
	appDir, destDir := t.TempDir(), t.TempDir()
	newCenter(t, appDir, "Alpha", 1)
	centers, _ := center.List(appDir)

	// Archive names carry a whole-second timestamp, so pre-existing archives
	// are faked with explicit older names rather than by running backups in a
	// loop (which would collide within the same second).
	old := []string{
		ArchivePrefix + "2020-01-01-000000" + archiveExt,
		ArchivePrefix + "2020-01-02-000000" + archiveExt,
		ArchivePrefix + "2020-01-03-000000" + archiveExt,
	}
	for _, name := range old {
		if err := os.WriteFile(filepath.Join(destDir, name), []byte("x"), 0o644); err != nil {
			t.Fatalf("seed %s: %v", name, err)
		}
	}

	res, err := Run(context.Background(), centers, destDir, "test", 2, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	remaining, err := List(destDir)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(remaining) != 2 {
		t.Fatalf("kept %d archives, want 2", len(remaining))
	}
	// Newest-first: the run's own archive, then the newest seeded one.
	if got := filepath.Join(destDir, remaining[0].Name()); got != res.Path {
		t.Errorf("newest archive = %s, want the one just written (%s)", got, res.Path)
	}
	if remaining[1].Name() != old[2] {
		t.Errorf("second archive = %s, want %s", remaining[1].Name(), old[2])
	}
}

// Pruning must never touch files it did not write: a backup directory that is
// also a synced folder will hold other people's files.
func TestPruneLeavesUnrelatedFilesAlone(t *testing.T) {
	appDir, destDir := t.TempDir(), t.TempDir()
	newCenter(t, appDir, "Alpha", 1)
	centers, _ := center.List(appDir)

	bystanders := []string{"notes.txt", "some-other-archive.zip", "checkin-backup-nope.zip"}
	for _, name := range bystanders {
		if err := os.WriteFile(filepath.Join(destDir, name), []byte("keep me"), 0o644); err != nil {
			t.Fatalf("seed %s: %v", name, err)
		}
	}

	if _, err := Run(context.Background(), centers, destDir, "test", 1, nil); err != nil {
		t.Fatalf("Run: %v", err)
	}
	for _, name := range bystanders {
		if _, err := os.Stat(filepath.Join(destDir, name)); err != nil {
			t.Errorf("%s was removed or altered: %v", name, err)
		}
	}
}

// A Center held open by another instance is reported rather than snapshotted
// from under the live writer.
func TestRunFailsWhenACenterIsOpen(t *testing.T) {
	appDir, destDir := t.TempDir(), t.TempDir()
	c := newCenter(t, appDir, "Alpha", 1)

	st, err := store.Open(c.DBPath())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer st.Close()

	centers, _ := center.List(appDir)
	_, err = Run(context.Background(), centers, destDir, "test", 7, nil)
	if err == nil {
		t.Fatal("Run succeeded while a Center was open, want an error")
	}
	if !strings.Contains(err.Error(), "Alpha") {
		t.Errorf("error %q should name the Center that is open", err)
	}
	// A failed run leaves nothing half-written behind.
	entries, _ := os.ReadDir(destDir)
	for _, e := range entries {
		t.Errorf("failed run left %q in the backup directory", e.Name())
	}
}

// An empty backup directory is not an error, and reports no last backup.
func TestLastBackupTime(t *testing.T) {
	dir := t.TempDir()
	if _, ok := LastBackupTime(dir); ok {
		t.Error("LastBackupTime on an empty directory reported a backup")
	}
	if _, ok := LastBackupTime(filepath.Join(dir, "does-not-exist")); ok {
		t.Error("LastBackupTime on a missing directory reported a backup")
	}

	name := ArchivePrefix + "2024-03-04-050607" + archiveExt
	if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	got, ok := LastBackupTime(dir)
	if !ok {
		t.Fatal("LastBackupTime found no backup")
	}
	want := time.Date(2024, 3, 4, 5, 6, 7, 0, time.Local)
	if !got.Equal(want) {
		t.Errorf("LastBackupTime = %v, want %v", got, want)
	}
}

// An unconfigured backup directory is a clear error, not a panic or a backup
// written somewhere arbitrary.
func TestRunRequiresADestination(t *testing.T) {
	if _, err := Run(context.Background(), nil, "", "test", 7, nil); err == nil {
		t.Fatal("Run with no destination succeeded, want an error")
	}
}
