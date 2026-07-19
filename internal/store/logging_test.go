package store

import (
	"bytes"
	"context"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
)

// TestMutationDebugLogs verifies the four student mutations emit DEBUG logs.
func TestMutationDebugLogs(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	ctx := context.Background()
	s, err := Open(filepath.Join(t.TempDir(), "log.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	st, _ := s.AddStudent(ctx, "Grace")
	s.CheckIn(ctx, st.ID, st.Name)
	s.CheckOut(ctx, st.ID, st.Name)
	s.RemoveStudent(ctx, st.ID, st.Name)

	out := buf.String()
	for _, want := range []string{
		"student added",
		"student checked in",
		"student checked out",
		"student removed",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing debug log %q in:\n%s", want, out)
		}
	}
}

// TestInfoLevelHidesDebug confirms DEBUG lines are suppressed at INFO level.
func TestInfoLevelHidesDebug(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	ctx := context.Background()
	s, err := Open(filepath.Join(t.TempDir(), "log2.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	s.AddStudent(ctx, "Heidi")
	if strings.Contains(buf.String(), "student added") {
		t.Errorf("DEBUG log leaked at INFO level:\n%s", buf.String())
	}
}
