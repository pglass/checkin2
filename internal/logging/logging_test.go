package logging

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"
)

func TestParseLevel(t *testing.T) {
	cases := map[string]slog.Level{
		"":        slog.LevelInfo,
		"info":    slog.LevelInfo,
		"DEBUG":   slog.LevelDebug,
		"warn":    slog.LevelWarn,
		"WARNING": slog.LevelWarn,
		"error":   slog.LevelError,
	}
	for in, want := range cases {
		got, err := ParseLevel(in)
		if err != nil {
			t.Errorf("ParseLevel(%q) error: %v", in, err)
		}
		if got != want {
			t.Errorf("ParseLevel(%q) = %v, want %v", in, got, want)
		}
	}
	if _, err := ParseLevel("bogus"); err == nil {
		t.Error("expected error for unknown level")
	}
}

func TestSetupStdout(t *testing.T) {
	closer, err := Setup("-", slog.LevelInfo)
	if err != nil {
		t.Fatal(err)
	}
	if closer != nil {
		t.Error("stdout mode should return a nil closer")
	}
}

func TestSetupFileCreatesLog(t *testing.T) {
	dir := t.TempDir()
	closer, err := Setup(dir, slog.LevelInfo)
	if err != nil {
		t.Fatal(err)
	}
	if closer != nil {
		defer closer.Close()
	}
	slog.Info("hello")
	if _, err := os.Stat(filepath.Join(dir, LogFileName)); err != nil {
		t.Fatalf("log file not created: %v", err)
	}
}
