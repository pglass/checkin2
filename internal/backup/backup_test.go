package backup

import (
	"archive/zip"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sort"
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

	// The snapshot and archive steps, then the verification pass over what was
	// just written.
	want := []string{"Backing up Alpha", "Backing up Beta", "Creating archive", "Unzipping archive"}
	if len(descs) != Steps(2) {
		t.Fatalf("got %d descriptions %v, want %d", len(descs), descs, Steps(2))
	}
	for i, w := range want {
		if descs[i] != w {
			t.Errorf("step %d description = %q, want %q", i+1, descs[i], w)
		}
	}
	// Every check of every Center is named, so the user can see which one is
	// being verified.
	for _, name := range []string{"Alpha", "Beta"} {
		for _, check := range Checks {
			want := fmt.Sprintf("Verifying %s backup: %s", name, check.Label())
			if !slices.Contains(descs, want) {
				t.Errorf("descriptions %v are missing %q", descs, want)
			}
		}
	}

	// Reported before the work: no archive exists at any announcement up to and
	// including the archive step's own. Verification runs after the archive is
	// written, so its steps see one and are not part of this check.
	for i := 0; i <= 2; i++ {
		if archivesAtStep[i] != 0 {
			t.Errorf("at step %d (%q) there were already %d archives; "+
				"the step was reported after its work, not before", i+1, descs[i], archivesAtStep[i])
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

// Archives are listed newest first, with the timestamp taken from the file name
// rather than the filesystem: a synced or copied file keeps its name but not
// necessarily its modification time.
func TestListArchivesNewestFirst(t *testing.T) {
	dir := t.TempDir()
	names := []string{
		ArchivePrefix + "2024-01-02-030405" + archiveExt,
		ArchivePrefix + "2026-05-06-070809" + archiveExt,
		ArchivePrefix + "2025-03-04-050607" + archiveExt,
	}
	for _, n := range names {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("payload"), 0o644); err != nil {
			t.Fatalf("seed %s: %v", n, err)
		}
	}

	got, err := ListArchives(dir)
	if err != nil {
		t.Fatalf("ListArchives: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d archives, want 3", len(got))
	}

	wantYears := []int{2026, 2025, 2024}
	for i, want := range wantYears {
		if y := got[i].CreatedAt.Year(); y != want {
			t.Errorf("archive %d year = %d, want %d (newest first)", i, y, want)
		}
	}
	if got[0].SizeBytes != int64(len("payload")) {
		t.Errorf("size = %d, want %d", got[0].SizeBytes, len("payload"))
	}
	if got[0].Path != filepath.Join(dir, got[0].Name) {
		t.Errorf("Path = %q, want it under %q", got[0].Path, dir)
	}
}

// Non-archives are not listed, so a backup directory shared with other files
// shows only this app's backups.
func TestListArchivesIgnoresOtherFiles(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{
		"notes.txt",
		"some-other-archive.zip",
		ArchivePrefix + "nope" + archiveExt, // prefix right, timestamp unparseable
		ArchivePrefix + "2025-03-04-050607" + archiveExt + ".partial",
	} {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("x"), 0o644); err != nil {
			t.Fatalf("seed %s: %v", n, err)
		}
	}

	got, err := ListArchives(dir)
	if err != nil {
		t.Fatalf("ListArchives: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("listed %d archives, want none: %+v", len(got), got)
	}
}

// An unset or missing directory has no backups and is not an error: it is the
// state of a fresh install, not a fault.
func TestListArchivesToleratesMissingDirectory(t *testing.T) {
	for _, dir := range []string{"", filepath.Join(t.TempDir(), "not-created")} {
		got, err := ListArchives(dir)
		if err != nil {
			t.Errorf("ListArchives(%q) = error %v, want none", dir, err)
		}
		if len(got) != 0 {
			t.Errorf("ListArchives(%q) returned %d archives, want none", dir, len(got))
		}
	}
}

// A real backup is listed by the same call the browser window uses.
func TestListArchivesSeesARealBackup(t *testing.T) {
	appDir, destDir := t.TempDir(), t.TempDir()
	newCenter(t, appDir, "Alpha", 2)
	centers, _ := center.List(appDir)

	res, err := Run(context.Background(), centers, destDir, "test", 7, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	got, err := ListArchives(destDir)
	if err != nil {
		t.Fatalf("ListArchives: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d archives, want 1", len(got))
	}
	if got[0].Path != res.Path {
		t.Errorf("Path = %q, want %q", got[0].Path, res.Path)
	}
	if got[0].SizeBytes <= 0 {
		t.Errorf("SizeBytes = %d, want > 0", got[0].SizeBytes)
	}
}

// archivesAt builds a newest-first archive list from timestamp stamps, for
// testing the retention policy without touching the filesystem.
func archivesAt(t *testing.T, stamps ...string) []Archive {
	t.Helper()
	out := make([]Archive, 0, len(stamps))
	for _, stamp := range stamps {
		name := ArchivePrefix + stamp + archiveExt
		at, ok := timeFromName(name)
		if !ok {
			t.Fatalf("bad stamp %q", stamp)
		}
		out = append(out, Archive{Name: name, Path: "/backups/" + name, CreatedAt: at})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name > out[j].Name })
	return out
}

// kept renders the stamps of the archives keepers retained, newest first, so a
// test can state the expected outcome as a list.
func kept(archives []Archive, keep int) []string {
	survivors := keepers(archives, keep)
	var out []string
	for _, a := range archives {
		if survivors[a.Name] {
			out = append(out, strings.TrimSuffix(strings.TrimPrefix(a.Name, ArchivePrefix), archiveExt))
		}
	}
	return out
}

// Retention keeps the recent archives and, beyond those, the newest archive of
// each month -- so the history thins with age instead of stopping a few days
// back.
func TestKeepersKeepsRecentPlusOnePerMonth(t *testing.T) {
	archives := archivesAt(t,
		// March: several, of which only the newest survives past the recent window.
		"2026-03-10-090000",
		"2026-03-09-090000",
		"2026-03-08-090000",
		"2026-03-01-090000",
		// February: two, newest kept.
		"2026-02-20-090000",
		"2026-02-02-090000",
		// January: one, kept.
		"2026-01-15-090000",
	)

	got := kept(archives, 3)
	want := []string{
		"2026-03-10-090000", // recent (1)
		"2026-03-09-090000", // recent (2)
		"2026-03-08-090000", // recent (3)
		"2026-02-20-090000", // newest in February
		"2026-01-15-090000", // newest in January
	}
	if !slices.Equal(got, want) {
		t.Errorf("kept %v, want %v", got, want)
	}
}

// The month's keeper is its newest archive, not its oldest: that is the most
// complete picture of the month.
func TestKeepersKeepsNewestArchiveOfEachMonth(t *testing.T) {
	archives := archivesAt(t,
		"2026-05-01-090000",
		"2026-01-31-235959",
		"2026-01-15-120000",
		"2026-01-01-000000",
	)

	got := kept(archives, 1)
	want := []string{"2026-05-01-090000", "2026-01-31-235959"}
	if !slices.Equal(got, want) {
		t.Errorf("kept %v, want %v (the newest of January, not the oldest)", got, want)
	}
}

// The two rules overlap rather than compete: an archive kept by recency does
// not consume its month's slot, and vice versa.
func TestKeepersRulesOverlapWithoutDoubleCounting(t *testing.T) {
	// All in one month, and fewer than keep: everything survives.
	archives := archivesAt(t,
		"2026-03-03-090000",
		"2026-03-02-090000",
	)
	if got := kept(archives, 5); len(got) != 2 {
		t.Errorf("kept %v, want both archives", got)
	}

	// Same month, more than keep: recency decides, and the month's newest is
	// already among them, so exactly keep survive.
	archives = archivesAt(t,
		"2026-03-05-090000",
		"2026-03-04-090000",
		"2026-03-03-090000",
		"2026-03-02-090000",
	)
	got := kept(archives, 2)
	want := []string{"2026-03-05-090000", "2026-03-04-090000"}
	if !slices.Equal(got, want) {
		t.Errorf("kept %v, want %v", got, want)
	}
}

// Months are calendar months, so archives days apart across a boundary are
// different months and both are kept.
func TestKeepersTreatsMonthBoundaries(t *testing.T) {
	archives := archivesAt(t,
		"2026-04-01-000100",
		"2026-03-31-235900",
	)
	if got := kept(archives, 1); len(got) != 2 {
		t.Errorf("kept %v, want both: they are in different calendar months", got)
	}

	// Same month in different years is not the same month.
	archives = archivesAt(t,
		"2026-03-15-090000",
		"2025-03-15-090000",
	)
	if got := kept(archives, 1); len(got) != 2 {
		t.Errorf("kept %v, want both: same month, different years", got)
	}
}

// A retention count of zero or less disables pruning, so a misconfigured value
// never deletes anything.
func TestKeepersZeroKeepsEverything(t *testing.T) {
	archives := archivesAt(t,
		"2026-03-03-090000",
		"2026-02-02-090000",
		"2025-01-01-090000",
	)
	for _, keep := range []int{0, -1} {
		if got := kept(archives, keep); len(got) != len(archives) {
			t.Errorf("keep=%d kept %v, want everything", keep, got)
		}
	}
}

// The monthly rule is what a real prune applies on disk, not just the policy
// function: an old archive that is the only one from its month survives even
// though it is far outside the recent window.
func TestRunPruneKeepsOnePerMonthOnDisk(t *testing.T) {
	appDir, destDir := t.TempDir(), t.TempDir()
	newCenter(t, appDir, "Alpha", 1)
	centers, _ := center.List(appDir)

	seeded := []string{
		ArchivePrefix + "2020-01-15-090000" + archiveExt, // only one in Jan 2020
		ArchivePrefix + "2020-02-10-090000" + archiveExt, // newest in Feb 2020
		ArchivePrefix + "2020-02-09-090000" + archiveExt, // older in Feb 2020
		ArchivePrefix + "2020-02-08-090000" + archiveExt, // older in Feb 2020
	}
	for _, name := range seeded {
		if err := os.WriteFile(filepath.Join(destDir, name), []byte("x"), 0o644); err != nil {
			t.Fatalf("seed %s: %v", name, err)
		}
	}

	// keep=1: only the archive this run writes is "recent", so everything else
	// survives on the monthly rule alone.
	if _, err := Run(context.Background(), centers, destDir, "test", 1, nil); err != nil {
		t.Fatalf("Run: %v", err)
	}

	remaining, err := ListArchives(destDir)
	if err != nil {
		t.Fatalf("ListArchives: %v", err)
	}
	var names []string
	for _, a := range remaining {
		names = append(names, a.Name)
	}

	// The run's own archive, plus one per older month.
	if len(names) != 3 {
		t.Fatalf("kept %d archives %v, want 3 (this run, newest of Feb 2020, only one of Jan 2020)",
			len(names), names)
	}
	if !slices.Contains(names, seeded[0]) {
		t.Errorf("the only January 2020 archive was deleted: %v", names)
	}
	if !slices.Contains(names, seeded[1]) {
		t.Errorf("the newest February 2020 archive was deleted: %v", names)
	}
	for _, gone := range seeded[2:] {
		if slices.Contains(names, gone) {
			t.Errorf("%s should have been pruned: %v", gone, names)
		}
	}
}
