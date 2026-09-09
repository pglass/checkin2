package center

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

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
