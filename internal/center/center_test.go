package center

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/pglass/checkin/internal/store"
)

func TestListSkipsFilesAndHiddenDirs(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"Beta", "alpha", ".hidden"} {
		if err := os.Mkdir(filepath.Join(dir, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, SettingsFileName), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	centers, err := List(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(centers) != 2 {
		t.Fatalf("got %d centers, want 2: %+v", len(centers), centers)
	}
	// Sorted case-insensitively.
	if centers[0].Name != "alpha" || centers[1].Name != "Beta" {
		t.Fatalf("unexpected order: %+v", centers)
	}
	if got, want := centers[0].Dir, filepath.Join(dir, "alpha"); got != want {
		t.Errorf("Dir = %q, want %q", got, want)
	}
	if got, want := centers[0].DBPath(), filepath.Join(dir, "alpha", DBFileName); got != want {
		t.Errorf("DBPath = %q, want %q", got, want)
	}
}

func TestCreate(t *testing.T) {
	dir := t.TempDir()

	c, err := Create(dir, "Fall 2026")
	if err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Stat(c.Dir); err != nil || !fi.IsDir() {
		t.Fatalf("center dir not created: %v", err)
	}

	if _, err := Create(dir, "Fall 2026"); !errors.Is(err, ErrExists) {
		t.Fatalf("second Create err = %v, want ErrExists", err)
	}
}

func TestCreateRejectsBadName(t *testing.T) {
	dir := t.TempDir()
	if _, err := Create(dir, "a/b"); err == nil {
		t.Fatal("expected error for name with a separator")
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("bad name created something: %+v", entries)
	}
}

func TestValidateName(t *testing.T) {
	valid := []string{"alpha", "Fall 2026", "north-side_1"}
	for _, name := range valid {
		if err := ValidateName(name); err != nil {
			t.Errorf("ValidateName(%q) = %v, want nil", name, err)
		}
	}

	invalid := []string{"", "   ", " lead", "trail ", ".", "..", ".hidden", "a/b", `a\b`, "a:b", "a*b", "a?b", `a"b`, "a<b", "a>b", "a|b", "a\x01b", "trail."}
	for _, name := range invalid {
		if err := ValidateName(name); err == nil {
			t.Errorf("ValidateName(%q) = nil, want error", name)
		}
	}
}

// Rename moves the Center's directory, taking the database with it.
func TestRename(t *testing.T) {
	dir := t.TempDir()
	if _, err := Create(dir, "Alpha"); err != nil {
		t.Fatalf("create: %v", err)
	}
	db := filepath.Join(dir, "Alpha", DBFileName)
	if err := os.WriteFile(db, []byte("data"), 0o644); err != nil {
		t.Fatalf("seed db: %v", err)
	}

	got, err := Rename(dir, "Alpha", "Beta")
	if err != nil {
		t.Fatalf("Rename: %v", err)
	}
	if got.Name != "Beta" {
		t.Errorf("renamed Name = %q, want Beta", got.Name)
	}
	if got.Dir != filepath.Join(dir, "Beta") {
		t.Errorf("renamed Dir = %q", got.Dir)
	}
	if _, err := os.Stat(filepath.Join(dir, "Alpha")); !os.IsNotExist(err) {
		t.Error("old Center directory still exists")
	}
	// The database went with the directory, contents intact.
	b, err := os.ReadFile(got.DBPath())
	if err != nil {
		t.Fatalf("read renamed db: %v", err)
	}
	if string(b) != "data" {
		t.Errorf("renamed db = %q, want the original contents", b)
	}
}

// Renaming onto another Center's name is refused, and changes nothing.
func TestRenameOntoExistingCenter(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"Alpha", "Beta"} {
		if _, err := Create(dir, n); err != nil {
			t.Fatalf("create %s: %v", n, err)
		}
	}

	if _, err := Rename(dir, "Alpha", "Beta"); !errors.Is(err, ErrExists) {
		t.Fatalf("Rename onto an existing Center = %v, want ErrExists", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "Alpha")); err != nil {
		t.Errorf("Alpha should be untouched after a refused rename: %v", err)
	}
}

// Renaming a Center to the name it already has succeeds and changes nothing,
// so confirming the dialog without editing is not an error.
func TestRenameToSameName(t *testing.T) {
	dir := t.TempDir()
	if _, err := Create(dir, "Alpha"); err != nil {
		t.Fatalf("create: %v", err)
	}

	got, err := Rename(dir, "Alpha", "Alpha")
	if err != nil {
		t.Fatalf("Rename to the same name = %v, want success", err)
	}
	if got.Name != "Alpha" {
		t.Errorf("Name = %q, want Alpha", got.Name)
	}
	if _, err := os.Stat(filepath.Join(dir, "Alpha")); err != nil {
		t.Errorf("Alpha should still exist: %v", err)
	}
}

// A change of case is a real rename, not a collision with itself -- including
// on a case-insensitive filesystem, where both names are one directory.
func TestRenameChangesCase(t *testing.T) {
	dir := t.TempDir()
	if _, err := Create(dir, "alpha"); err != nil {
		t.Fatalf("create: %v", err)
	}

	got, err := Rename(dir, "alpha", "Alpha")
	if err != nil {
		t.Fatalf("Rename changing case = %v, want success", err)
	}
	if got.Name != "Alpha" {
		t.Errorf("Name = %q, want Alpha", got.Name)
	}

	centers, err := List(dir)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(centers) != 1 {
		t.Fatalf("centers = %d, want 1", len(centers))
	}
	if centers[0].Name != "Alpha" {
		t.Errorf("listed name = %q, want Alpha", centers[0].Name)
	}
}

// An invalid new name is refused before anything is moved.
func TestRenameRejectsInvalidName(t *testing.T) {
	dir := t.TempDir()
	if _, err := Create(dir, "Alpha"); err != nil {
		t.Fatalf("create: %v", err)
	}

	for _, name := range []string{"", " Alpha", ".hidden", "a/b"} {
		if _, err := Rename(dir, "Alpha", name); err == nil {
			t.Errorf("Rename to %q succeeded, want a validation error", name)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "Alpha")); err != nil {
		t.Errorf("Alpha should be untouched: %v", err)
	}
}

// A Center open in another window is not renamed: moving the directory would
// leave that instance writing into a path that no longer exists.
func TestRenameRefusesOpenCenter(t *testing.T) {
	dir := t.TempDir()
	c, err := Create(dir, "Alpha")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// Holding the Center's lock is what "open in another window" means.
	st, err := store.Open(c.DBPath())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()

	if _, err := Rename(dir, "Alpha", "Beta"); !errors.Is(err, ErrOpen) {
		t.Fatalf("Rename of an open Center = %v, want ErrOpen", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "Alpha")); err != nil {
		t.Errorf("Alpha should be untouched after a refused rename: %v", err)
	}

	// Once it is closed the rename goes through.
	if err := st.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if _, err := Rename(dir, "Alpha", "Beta"); err != nil {
		t.Errorf("Rename after close = %v, want success", err)
	}
}

// A deactivated Center's directory name carries its name and the time, and
// parses back to exactly those.
func TestDirNameRoundTrip(t *testing.T) {
	at := time.Date(2026, 9, 10, 14, 30, 5, 0, time.Local)

	dir := DirName("Maple St", at)
	if dir != "Maple St_deactivated_2026-09-10-143005" {
		t.Fatalf("DirName = %q", dir)
	}

	name, got := parseDirName(dir)
	if name != "Maple St" {
		t.Errorf("name = %q, want Maple St", name)
	}
	if !got.Equal(at) {
		t.Errorf("time = %v, want %v", got, at)
	}

	// An active Center is just its name.
	if got := DirName("Maple St", time.Time{}); got != "Maple St" {
		t.Errorf("active DirName = %q, want Maple St", got)
	}
	if n, at := parseDirName("Maple St"); n != "Maple St" || !at.IsZero() {
		t.Errorf("parseDirName(active) = %q, %v", n, at)
	}
}

// A Center whose name merely contains the marker is not mistaken for a
// deactivated one: the name is user data, and the timestamp must parse.
func TestParseDirNameIgnoresUnparseableStamp(t *testing.T) {
	for _, dir := range []string{
		"Summer_deactivated_camp",
		"Alpha_deactivated_",
		"Alpha_deactivated_not-a-time",
	} {
		name, at := parseDirName(dir)
		if name != dir || !at.IsZero() {
			t.Errorf("parseDirName(%q) = %q, %v; want it treated as an active name", dir, name, at)
		}
	}
}

// Deactivating renames the directory and keeps the data; List hides it while
// ListAll still reports it.
func TestDeactivateHidesCenter(t *testing.T) {
	dir := t.TempDir()
	c, err := Create(dir, "Alpha")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := os.WriteFile(c.DBPath(), []byte("data"), 0o644); err != nil {
		t.Fatalf("seed db: %v", err)
	}

	at := time.Date(2026, 9, 10, 14, 30, 5, 0, time.Local)
	got, err := Deactivate(dir, "Alpha", at)
	if err != nil {
		t.Fatalf("Deactivate: %v", err)
	}
	if !got.Deactivated() {
		t.Error("returned Center is not marked deactivated")
	}
	if got.Name != "Alpha" {
		t.Errorf("Name = %q, want the original Alpha", got.Name)
	}

	// The data moved with the directory.
	b, err := os.ReadFile(got.DBPath())
	if err != nil {
		t.Fatalf("read db after deactivate: %v", err)
	}
	if string(b) != "data" {
		t.Errorf("db = %q, want the original contents", b)
	}

	active, err := List(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(active) != 0 {
		t.Errorf("List = %v, want no active Centers", active)
	}

	all, err := ListAll(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 || all[0].Name != "Alpha" || !all[0].DeactivatedAt.Equal(at) {
		t.Errorf("ListAll = %+v, want one deactivated Alpha at %v", all, at)
	}
}

// Reactivating puts the original name back and returns it to List.
func TestReactivateRestoresName(t *testing.T) {
	dir := t.TempDir()
	if _, err := Create(dir, "Alpha"); err != nil {
		t.Fatalf("create: %v", err)
	}
	at := time.Date(2026, 9, 10, 14, 30, 5, 0, time.Local)
	deact, err := Deactivate(dir, "Alpha", at)
	if err != nil {
		t.Fatalf("Deactivate: %v", err)
	}

	got, err := Reactivate(dir, deact)
	if err != nil {
		t.Fatalf("Reactivate: %v", err)
	}
	if got.Deactivated() {
		t.Error("reactivated Center still reports as deactivated")
	}
	if got.Dir != filepath.Join(dir, "Alpha") {
		t.Errorf("Dir = %q, want the plain name", got.Dir)
	}

	active, err := List(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(active) != 1 || active[0].Name != "Alpha" {
		t.Errorf("List = %+v, want the reactivated Alpha", active)
	}
}

// Reactivating onto a name an active Center already holds is refused, and
// changes nothing -- this is the case the UI explains to the user.
func TestReactivateRefusedWhenNameTaken(t *testing.T) {
	dir := t.TempDir()
	if _, err := Create(dir, "Alpha"); err != nil {
		t.Fatalf("create: %v", err)
	}
	deact, err := Deactivate(dir, "Alpha", time.Date(2026, 9, 10, 14, 30, 5, 0, time.Local))
	if err != nil {
		t.Fatalf("Deactivate: %v", err)
	}
	// A new Center takes the freed name, e.g. one restored from a backup.
	if _, err := Create(dir, "Alpha"); err != nil {
		t.Fatalf("recreate: %v", err)
	}

	if _, err := Reactivate(dir, deact); !errors.Is(err, ErrExists) {
		t.Fatalf("Reactivate with the name taken = %v, want ErrExists", err)
	}
	if _, err := os.Stat(deact.Dir); err != nil {
		t.Errorf("deactivated directory should be untouched: %v", err)
	}
}

// A Center open in another window is not deactivated, for the same reason it is
// not renamed.
func TestDeactivateRefusesOpenCenter(t *testing.T) {
	dir := t.TempDir()
	c, err := Create(dir, "Alpha")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	st, err := store.Open(c.DBPath())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()

	_, err = Deactivate(dir, "Alpha", time.Now())
	if !errors.Is(err, ErrOpen) {
		t.Fatalf("Deactivate of an open Center = %v, want ErrOpen", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "Alpha")); err != nil {
		t.Errorf("Alpha should be untouched: %v", err)
	}
}

// An active Center and a deactivated one can share a name: that is the state
// the restore-from-backup workflow leaves behind.
func TestListAllWithActiveAndDeactivatedSameName(t *testing.T) {
	dir := t.TempDir()
	if _, err := Create(dir, "Alpha"); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := Deactivate(dir, "Alpha", time.Date(2026, 9, 10, 14, 30, 5, 0, time.Local)); err != nil {
		t.Fatalf("deactivate: %v", err)
	}
	if _, err := Create(dir, "Alpha"); err != nil {
		t.Fatalf("recreate: %v", err)
	}

	active, err := List(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(active) != 1 {
		t.Errorf("List = %d Centers, want just the active Alpha", len(active))
	}

	all, err := ListAll(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Fatalf("ListAll = %d Centers, want 2", len(all))
	}
	var nActive, nDeact int
	for _, c := range all {
		if c.Name != "Alpha" {
			t.Errorf("name = %q, want both to be Alpha", c.Name)
		}
		if c.Deactivated() {
			nDeact++
		} else {
			nActive++
		}
	}
	if nActive != 1 || nDeact != 1 {
		t.Errorf("got %d active / %d deactivated, want 1 each", nActive, nDeact)
	}
}
