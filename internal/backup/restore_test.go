package backup

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pglass/checkin/internal/center"
	"github.com/pglass/checkin/internal/store"
)

// The restored Center is named for the backup it came from, and the name is
// valid on every platform -- no colons or separators from the timestamp.
func TestRestoreName(t *testing.T) {
	at := time.Date(2026, 9, 8, 14, 5, 0, 0, time.Local)
	got := RestoreName("Maple St", at)

	if !strings.HasPrefix(got, "Maple St-") {
		t.Errorf("RestoreName = %q, want it to start with the Center's name", got)
	}
	if !strings.Contains(got, "2026-09-08") {
		t.Errorf("RestoreName = %q, want it to carry the backup's date", got)
	}
	if err := center.ValidateName(got); err != nil {
		t.Errorf("RestoreName produced an invalid Center name %q: %v", got, err)
	}
}

// A restore creates a new Center whose database holds the backed-up data.
func TestRestoreCreatesACenterWithTheBackupContents(t *testing.T) {
	archive := makeBackup(t, map[string]int{"Alpha": 3})
	appDir := t.TempDir()

	c, err := Restore(context.Background(), archive, "Alpha", appDir)
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}

	man, err := ReadManifest(archive)
	if err != nil {
		t.Fatalf("ReadManifest: %v", err)
	}
	if want := RestoreName("Alpha", man.CreatedAt); c.Name != want {
		t.Errorf("restored Center name = %q, want %q", c.Name, want)
	}

	// The restored database opens as a live Center and holds the students.
	st, err := store.Open(c.DBPath())
	if err != nil {
		t.Fatalf("open restored Center: %v", err)
	}
	defer st.Close()

	rows, err := st.Students(context.Background())
	if err != nil {
		t.Fatalf("read restored students: %v", err)
	}
	if len(rows) != 3 {
		t.Errorf("restored Center has %d students, want 3", len(rows))
	}
}

// Restoring never overwrites an existing Center: a second restore of the same
// backup is refused, and the Center already there is left untouched.
func TestRestoreDoesNotOverwriteAnExistingCenter(t *testing.T) {
	archive := makeBackup(t, map[string]int{"Alpha": 2})
	appDir := t.TempDir()

	first, err := Restore(context.Background(), archive, "Alpha", appDir)
	if err != nil {
		t.Fatalf("first Restore: %v", err)
	}

	// Write a marker into the restored Center, so an overwrite is detectable.
	marker := filepath.Join(first.Dir, "marker.txt")
	if err := os.WriteFile(marker, []byte("keep me"), 0o644); err != nil {
		t.Fatalf("marker: %v", err)
	}

	_, err = Restore(context.Background(), archive, "Alpha", appDir)
	if err == nil {
		t.Fatal("second Restore succeeded, want it refused")
	}
	if !errors.Is(err, center.ErrExists) {
		t.Errorf("second Restore error = %v, want center.ErrExists", err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Errorf("the existing Center was disturbed: %v", err)
	}
}

// Restoring one Center leaves the others in the archive alone: only the one
// asked for is created.
func TestRestoreCreatesOnlyTheRequestedCenter(t *testing.T) {
	archive := makeBackup(t, map[string]int{"Alpha": 1, "Beta": 1})
	appDir := t.TempDir()

	if _, err := Restore(context.Background(), archive, "Alpha", appDir); err != nil {
		t.Fatalf("Restore: %v", err)
	}

	centers, err := center.List(appDir)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(centers) != 1 {
		t.Fatalf("created %d Centers, want 1: %+v", len(centers), centers)
	}
	if !strings.HasPrefix(centers[0].Name, "Alpha-") {
		t.Errorf("created %q, want the Alpha restore", centers[0].Name)
	}
}

// A Center not in the archive is reported rather than silently doing nothing.
func TestRestoreRejectsAnUnknownCenter(t *testing.T) {
	archive := makeBackup(t, map[string]int{"Alpha": 1})
	appDir := t.TempDir()

	_, err := Restore(context.Background(), archive, "Nope", appDir)
	if err == nil {
		t.Fatal("restoring a Center not in the backup succeeded")
	}
	if !strings.Contains(err.Error(), "Nope") {
		t.Errorf("error = %q, want it to name the missing Center", err)
	}
	if centers, _ := center.List(appDir); len(centers) != 0 {
		t.Errorf("a failed restore created %d Centers, want none", len(centers))
	}
}

// A snapshot that fails its own checks is not restored: putting silently wrong
// data in front of the user is worse than refusing.
func TestRestoreRefusesACorruptSnapshot(t *testing.T) {
	archive := makeBackup(t, map[string]int{"Alpha": 3})
	appDir := t.TempDir()

	rewriteArchive(t, archive, func(name string, data []byte) []byte {
		if !strings.HasPrefix(name, "centers/") {
			return nil
		}
		altered := append([]byte(nil), data...)
		for i := len(altered) / 2; i < len(altered)/2+64 && i < len(altered); i++ {
			altered[i] ^= 0xFF
		}
		return altered
	})

	_, err := Restore(context.Background(), archive, "Alpha", appDir)
	if err == nil {
		t.Fatal("a corrupt snapshot was restored")
	}
	if !strings.Contains(err.Error(), "checks") {
		t.Errorf("error = %q, want it to say the backup failed its checks", err)
	}
	// Nothing half-made is left behind.
	if centers, _ := center.List(appDir); len(centers) != 0 {
		t.Errorf("a refused restore left %d Centers behind", len(centers))
	}
}

// A Center whose name would be invalid once the timestamp is appended is
// refused before anything is created.
func TestRestoreRejectsANameItCannotUse(t *testing.T) {
	archive := makeBackup(t, map[string]int{"Alpha": 1})
	appDir := t.TempDir()

	// A crafted manifest naming a Center that cannot become a directory.
	rewriteArchive(t, archive, func(name string, data []byte) []byte {
		if name != ManifestName {
			return nil
		}
		var man Manifest
		json.Unmarshal(data, &man)
		man.Centers[0].Name = "bad/name"
		out, _ := json.Marshal(man)
		return out
	})

	if _, err := Restore(context.Background(), archive, "bad/name", appDir); err == nil {
		t.Fatal("a Center with an unusable name was restored")
	}
	if centers, _ := center.List(appDir); len(centers) != 0 {
		t.Errorf("a refused restore created %d Centers", len(centers))
	}
}

// An empty Center -- created but never opened -- restores as an empty but
// usable Center rather than failing.
func TestRestoreHandlesAnEmptyCenter(t *testing.T) {
	appDir, destDir := t.TempDir(), t.TempDir()
	newCenter(t, appDir, "Empty", 0)
	centers, _ := center.List(appDir)
	res, err := Run(context.Background(), centers, destDir, "test", 7, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	target := t.TempDir()
	c, err := Restore(context.Background(), res.Path, "Empty", target)
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}

	st, err := store.Open(c.DBPath())
	if err != nil {
		t.Fatalf("open restored empty Center: %v", err)
	}
	defer st.Close()

	rows, err := st.Students(context.Background())
	if err != nil {
		t.Fatalf("read restored students: %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("restored empty Center has %d students, want 0", len(rows))
	}
}
