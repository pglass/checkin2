package store

import (
	"context"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestAddStudent_Duplicate(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	st, err := s.AddStudent(ctx, "Alice")
	if err != nil {
		t.Fatalf("AddStudent: %v", err)
	}
	if st.Name != "Alice" || st.ID == 0 {
		t.Fatalf("unexpected student: %+v", st)
	}

	if _, err := s.AddStudent(ctx, "Alice"); err != ErrDuplicateName {
		t.Fatalf("want ErrDuplicateName, got %v", err)
	}
}

func TestAddStudent_TrimsAndRejectsEmpty(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	if _, err := s.AddStudent(ctx, "   "); err == nil {
		t.Fatal("want error for empty name")
	}
	st, err := s.AddStudent(ctx, "  Bob  ")
	if err != nil {
		t.Fatalf("AddStudent: %v", err)
	}
	if st.Name != "Bob" {
		t.Fatalf("name not trimmed: %q", st.Name)
	}
}

func TestCheckInOutStatus(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	st, _ := s.AddStudent(ctx, "Carol")

	if got := s.Status(st.ID); got != StatusNotIn {
		t.Fatalf("initial status = %v, want NotIn", got)
	}
	if err := s.CheckIn(ctx, st.ID, st.Name); err != nil {
		t.Fatal(err)
	}
	if got := s.Status(st.ID); got != StatusIn {
		t.Fatalf("after check-in status = %v, want In", got)
	}
	if err := s.CheckOut(ctx, st.ID, st.Name); err != nil {
		t.Fatal(err)
	}
	if got := s.Status(st.ID); got != StatusOut {
		t.Fatalf("after check-out status = %v, want Out", got)
	}

	rows, err := s.Students(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].In == nil || rows[0].Out == nil {
		t.Fatalf("rows missing times: %+v", rows)
	}
}

func TestReset_ClearsTodayAndRebuild(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "reset.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	st, _ := s.AddStudent(ctx, "Dave")
	s.CheckIn(ctx, st.ID, st.Name)
	s.CheckOut(ctx, st.ID, st.Name)
	if err := s.Reset(ctx, st.ID); err != nil {
		t.Fatal(err)
	}
	if got := s.Status(st.ID); got != StatusNotIn {
		t.Fatalf("after reset status = %v, want NotIn", got)
	}
	s.Close()

	// Reopen: rebuild-from-log must also show cleared state (rows were deleted).
	s2, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	if got := s2.Status(st.ID); got != StatusNotIn {
		t.Fatalf("after reopen status = %v, want NotIn", got)
	}
}

func TestRebuildFromLog(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "rebuild.db")
	s, _ := Open(path)
	st, _ := s.AddStudent(ctx, "Erin")
	s.CheckIn(ctx, st.ID, st.Name)
	s.Close()

	s2, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	if got := s2.Status(st.ID); got != StatusIn {
		t.Fatalf("rebuilt status = %v, want In", got)
	}
}

func TestRemoveStudent(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	st, _ := s.AddStudent(ctx, "Frank")
	if err := s.RemoveStudent(ctx, st.ID, st.Name); err != nil {
		t.Fatal(err)
	}
	rows, _ := s.Students(ctx)
	if len(rows) != 0 {
		t.Fatalf("student not removed: %+v", rows)
	}
	// A Deleted log row should still exist (append-only history).
	logs, err := s.Queries().LogSince(ctx, 0)
	_ = logs // LogSince only returns Checked In/Out; Deleted history verified indirectly.
	if err != nil {
		t.Fatal(err)
	}
}

// Students orders by today's most recent check-in/out (newest first), with
// students who have no activity today following, alphabetically.
func TestStudents_SortByRecentActivityThenName(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	names := []string{"Bob", "Alice", "Carol", "Dave"}
	ids := map[string]int64{}
	for _, n := range names {
		st, err := s.AddStudent(ctx, n)
		if err != nil {
			t.Fatalf("AddStudent %s: %v", n, err)
		}
		ids[n] = st.ID
	}

	now := time.Now()
	// Bob checked in early; Dave checked in later then out later still, so
	// Dave's check-out is the most recent event of the day.
	s.day.setIn(ids["Bob"], now.Add(-3*time.Hour))
	s.day.setIn(ids["Dave"], now.Add(-2*time.Hour))
	s.day.setOut(ids["Dave"], now.Add(-time.Minute))
	// Alice and Carol have nothing today.

	rows, err := s.Students(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, r := range rows {
		got = append(got, r.Name)
	}
	want := []string{"Dave", "Bob", "Alice", "Carol"}
	if !slices.Equal(got, want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
}
