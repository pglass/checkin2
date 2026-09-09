package logging

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
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

// readLog returns the contents of the log file in dir.
func readLog(t *testing.T, dir string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, LogFileName))
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	return string(data)
}

// SetCenter adds a center field to later lines, so one shared log file still
// says which Center each line came from.
func TestSetCenterTagsLaterLines(t *testing.T) {
	dir := t.TempDir()
	closer, err := Setup(dir, slog.LevelInfo)
	if err != nil {
		t.Fatal(err)
	}
	defer closer.Close()

	slog.Info("before-any-center")
	SetCenter("Maple St")
	slog.Info("after-center")

	out := readLog(t, dir)
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 2 {
		t.Fatalf("got %d log lines, want 2:\n%s", len(lines), out)
	}
	// Work done before a Center is chosen is still logged, without the field.
	if strings.Contains(lines[0], "center=") {
		t.Errorf("line before SetCenter has a center field: %s", lines[0])
	}
	if !strings.Contains(lines[1], `center="Maple St"`) {
		t.Errorf("line after SetCenter has no center field: %s", lines[1])
	}
}

// Switching Centers replaces the field rather than stacking a second one.
func TestSetCenterDoesNotStackFields(t *testing.T) {
	dir := t.TempDir()
	closer, err := Setup(dir, slog.LevelInfo)
	if err != nil {
		t.Fatal(err)
	}
	defer closer.Close()

	SetCenter("Alpha")
	SetCenter("Beta")
	slog.Info("switched")

	out := readLog(t, dir)
	if strings.Contains(out, "Alpha") {
		t.Errorf("the previous Center is still attached: %s", out)
	}
	if n := strings.Count(out, "center="); n != 1 {
		t.Errorf("got %d center fields, want 1: %s", n, out)
	}
}

// An empty name clears the field, so a log line is never wrongly attributed.
func TestSetCenterEmptyClearsTheField(t *testing.T) {
	dir := t.TempDir()
	closer, err := Setup(dir, slog.LevelInfo)
	if err != nil {
		t.Fatal(err)
	}
	defer closer.Close()

	SetCenter("Alpha")
	SetCenter("")
	slog.Info("cleared")

	if out := readLog(t, dir); strings.Contains(out, "center=") {
		t.Errorf("center field survived being cleared: %s", out)
	}
}

// Setup writes to the directory it is given -- the app directory, not a
// Center's -- so one log covers the whole run.
func TestSetupUsesTheGivenDirectory(t *testing.T) {
	appDir := t.TempDir()
	closer, err := Setup(appDir, slog.LevelInfo)
	if err != nil {
		t.Fatal(err)
	}
	defer closer.Close()

	slog.Info("startup")
	if _, err := os.Stat(filepath.Join(appDir, LogFileName)); err != nil {
		t.Fatalf("log not written to the given directory: %v", err)
	}
}
