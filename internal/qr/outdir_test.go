package qr

import (
	"os"
	"path/filepath"
	"testing"
)

// withHome points os.UserHomeDir at a temporary directory and returns it.
func withHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home) // Windows
	return home
}

func TestCenterDirIsUnderTheDesktop(t *testing.T) {
	home := withHome(t)
	desktop := filepath.Join(home, "Desktop")
	if err := os.Mkdir(desktop, 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := CenterDir("Main Street")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(desktop, RootDirName, "Main Street"); got != want {
		t.Errorf("CenterDir = %s, want %s", got, want)
	}
}

// Without a Desktop folder the images still go somewhere sensible rather than
// the call failing.
func TestCenterDirFallsBackToHomeWithoutADesktop(t *testing.T) {
	home := withHome(t)
	got, err := CenterDir("Main Street")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(home, RootDirName, "Main Street"); got != want {
		t.Errorf("CenterDir = %s, want %s", got, want)
	}
}

func TestCenterDirRejectsAnEmptyName(t *testing.T) {
	withHome(t)
	if _, err := CenterDir("  "); err == nil {
		t.Error("expected an error for an empty center name")
	}
}

func TestRenameCenterDirMovesGeneratedImages(t *testing.T) {
	withHome(t)
	oldDir, err := CenterDir("Old")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(oldDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(oldDir, "a.png"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := RenameCenterDir("Old", "New"); err != nil {
		t.Fatal(err)
	}
	newDir, err := CenterDir("New")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(newDir, "a.png")); err != nil {
		t.Errorf("image did not follow the rename: %v", err)
	}
	if _, err := os.Stat(oldDir); !os.IsNotExist(err) {
		t.Errorf("old directory still present: %v", err)
	}
}

// Renaming a Center that never generated codes is a no-op, not an error, and
// must not create an empty folder on the Desktop.
func TestRenameCenterDirWithNothingGenerated(t *testing.T) {
	withHome(t)
	if err := RenameCenterDir("Old", "New"); err != nil {
		t.Fatal(err)
	}
	newDir, err := CenterDir("New")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(newDir); !os.IsNotExist(err) {
		t.Errorf("rename created %s for a Center with no images", newDir)
	}
}

// An existing folder under the new name is left alone rather than being
// merged into or overwritten.
func TestRenameCenterDirLeavesAnOccupiedDestination(t *testing.T) {
	withHome(t)
	oldDir, _ := CenterDir("Old")
	newDir, _ := CenterDir("New")
	for _, d := range []string{oldDir, newDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(newDir, "keep.png"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := RenameCenterDir("Old", "New"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(newDir, "keep.png")); err != nil {
		t.Errorf("existing directory was disturbed: %v", err)
	}
	if _, err := os.Stat(oldDir); err != nil {
		t.Errorf("old directory should have been left in place: %v", err)
	}
}

func TestRenameCenterDirSameName(t *testing.T) {
	withHome(t)
	dir, _ := CenterDir("Same")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := RenameCenterDir("Same", "Same"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Errorf("directory disappeared: %v", err)
	}
}
