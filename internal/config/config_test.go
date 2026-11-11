package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestLoadCreatesDefaultWhenMissing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.ini")

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.CameraFPS != Default().CameraFPS {
		t.Fatalf("CameraFPS = %d, want default %d", cfg.CameraFPS, Default().CameraFPS)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("file not created: %v", err)
	}

	// Reloading the just-written file yields the same values.
	got, err := Load(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if !reflect.DeepEqual(got, cfg) {
		t.Fatalf("reload = %+v, want %+v", got, cfg)
	}
}

func TestLoadParsesValues(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.ini")
	content := "# comment\n[camera]\ncamera_fps = 24\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.CameraFPS != 24 {
		t.Fatalf("CameraFPS = %d, want 24", cfg.CameraFPS)
	}
}

func TestLoadRejectsBadFPS(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.ini")
	if err := os.WriteFile(path, []byte("camera_fps = 0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("expected error for camera_fps = 0")
	}
}

// writeSettings writes content to a fresh settings.ini and returns its path.
func writeSettings(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "settings.ini")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// Numbered keys load in number order, gaps and all, and are written back
// renumbered 1..N.
func TestBackupDirsLoadAndWrite(t *testing.T) {
	path := writeSettings(t, "[backup]\nbackup_dirs3 = /c\nbackup_dirs1 = /a\nbackup_dirs7 = /d\n")
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if want := []string{"/a", "/c", "/d"}; !reflect.DeepEqual(cfg.BackupDirs, want) {
		t.Fatalf("BackupDirs = %q, want %q", cfg.BackupDirs, want)
	}

	// Remove the middle one, as the Settings window would, and save.
	cfg.BackupDirs = []string{"/a", "/d"}
	if err := Write(path, cfg); err != nil {
		t.Fatalf("Write: %v", err)
	}
	data, _ := os.ReadFile(path)
	text := string(data)
	for _, want := range []string{"backup_dirs1 = /a\n", "backup_dirs2 = /d\n"} {
		if !strings.Contains(text, want) {
			t.Errorf("written file lacks %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "backup_dirs3") {
		t.Errorf("written file still has backup_dirs3:\n%s", text)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if !reflect.DeepEqual(got, cfg) {
		t.Errorf("reload = %+v, want %+v", got, cfg)
	}
}

// With no directories the file carries a commented-out example, which loads
// as no directories.
func TestBackupDirsEmptyWritesExample(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.ini")
	if err := Write(path, Default()); err != nil {
		t.Fatalf("Write: %v", err)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "# backup_dirs1 = ") {
		t.Errorf("no commented example in:\n%s", data)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.BackupDirs) != 0 {
		t.Errorf("BackupDirs = %q, want none", cfg.BackupDirs)
	}
}

// The legacy backup_dir is moved into backup_dirs and removed from the file,
// so it is migrated once only.
func TestLegacyBackupDirMigratedOnce(t *testing.T) {
	path := writeSettings(t, "[backup]\nbackup_dir = /old\nbackup_dirs1 = /new\n")
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if want := []string{"/new", "/old"}; !reflect.DeepEqual(cfg.BackupDirs, want) {
		t.Fatalf("BackupDirs = %q, want %q", cfg.BackupDirs, want)
	}
	data, _ := os.ReadFile(path)
	if strings.Contains(string(data), "backup_dir =") {
		t.Fatalf("legacy key still in file after migration:\n%s", data)
	}

	// The user then removes the migrated directory; it must not come back.
	cfg.BackupDirs = []string{"/new"}
	if err := Write(path, cfg); err != nil {
		t.Fatalf("Write: %v", err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if want := []string{"/new"}; !reflect.DeepEqual(got.BackupDirs, want) {
		t.Errorf("after removal BackupDirs = %q, want %q", got.BackupDirs, want)
	}
}

// An empty legacy backup_dir (backups were off) adds nothing but is still
// removed from the file. A legacy directory already in the list is not doubled.
func TestLegacyBackupDirEmptyOrDuplicate(t *testing.T) {
	path := writeSettings(t, "backup_dir =\n")
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.BackupDirs) != 0 {
		t.Errorf("BackupDirs = %q, want none", cfg.BackupDirs)
	}
	if data, _ := os.ReadFile(path); strings.Contains(string(data), "backup_dir =") {
		t.Errorf("empty legacy key not removed:\n%s", data)
	}

	path = writeSettings(t, "backup_dirs1 = /same\nbackup_dir = /same\n")
	cfg, err = Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if want := []string{"/same"}; !reflect.DeepEqual(cfg.BackupDirs, want) {
		t.Errorf("BackupDirs = %q, want %q", cfg.BackupDirs, want)
	}
}
