package config

import (
	"os"
	"path/filepath"
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
	if got != cfg {
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
