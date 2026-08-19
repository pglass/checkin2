package center

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
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
