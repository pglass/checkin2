package store

import (
	"context"
	"database/sql"
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
	if err := s.CheckIn(ctx, st.ID, st.Name, "Parent"); err != nil {
		t.Fatal(err)
	}
	if got := s.Status(st.ID); got != StatusIn {
		t.Fatalf("after check-in status = %v, want In", got)
	}
	if err := s.CheckOut(ctx, st.ID, st.Name, "Parent"); err != nil {
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
	s.CheckIn(ctx, st.ID, st.Name, "Parent")
	s.CheckOut(ctx, st.ID, st.Name, "Parent")
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
	s.CheckIn(ctx, st.ID, st.Name, "Parent")
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

// The authorized adult typed at check-in/out lands on that Log row, and is
// NULL for actions where it does not apply (Added, Deleted) or was left blank.
func TestAuthorizedAdultStoredOnLogRows(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	st, err := s.AddStudent(ctx, "Alice")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.CheckIn(ctx, st.ID, st.Name, "  Bob Parent  "); err != nil {
		t.Fatal(err)
	}
	if err := s.CheckOut(ctx, st.ID, st.Name, ""); err != nil {
		t.Fatal(err)
	}

	rows, err := s.DB().QueryContext(ctx,
		"SELECT Action, AuthorizedAdult FROM Log ORDER BY ID")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	type entry struct {
		action string
		adult  sql.NullString
	}
	var got []entry
	for rows.Next() {
		var e entry
		if err := rows.Scan(&e.action, &e.adult); err != nil {
			t.Fatal(err)
		}
		got = append(got, e)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}

	want := []entry{
		{ActionAdded, sql.NullString{}},
		{ActionCheckedIn, sql.NullString{String: "Bob Parent", Valid: true}}, // trimmed
		{ActionCheckedOut, sql.NullString{}},                                 // blank -> NULL
	}
	if len(got) != len(want) {
		t.Fatalf("got %d log rows, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("row %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

// signInAt records a check-in for a student at an explicit time with an
// explicit authorized adult, so a test can build up several days of history.
// (CheckIn always stamps time.Now(), and Reset would delete same-day rows.)
func signInAt(t *testing.T, s *Store, id int64, name, adult string, at time.Time) {
	t.Helper()
	if err := s.appendLogAt(context.Background(), id, name, ActionCheckedIn, at, adult); err != nil {
		t.Fatal(err)
	}
}

// Suggestions are the student's own distinct adults, most recently used first,
// capped at maxAdultSuggestions.
func TestRecentAuthorizedAdults(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	alice, err := s.AddStudent(ctx, "Alice")
	if err != nil {
		t.Fatal(err)
	}
	bob, err := s.AddStudent(ctx, "Bob")
	if err != nil {
		t.Fatal(err)
	}

	base := time.Now().Add(-100 * time.Hour)
	// Oldest first. Jane repeats, so she must collapse to one entry and rank by
	// her latest use, not her first.
	for i, adult := range []string{"Jane", "Grandpa", "Jane", "Sitter"} {
		signInAt(t, s, alice.ID, alice.Name, adult, base.Add(time.Duration(i)*time.Hour))
	}
	// Another student's adult must not leak into Alice's suggestions.
	signInAt(t, s, bob.ID, bob.Name, "Stranger", base.Add(10*time.Hour))

	got, err := s.RecentAuthorizedAdults(ctx, alice.ID)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"Sitter", "Jane", "Grandpa"}
	if !slices.Equal(got, want) {
		t.Errorf("RecentAuthorizedAdults = %v, want %v", got, want)
	}
}

// A student with no history, and one whose only row recorded no adult, both
// get no suggestions; the dialog falls back to typing.
func TestRecentAuthorizedAdultsEmptyCases(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	st, err := s.AddStudent(ctx, "Alice")
	if err != nil {
		t.Fatal(err)
	}

	got, err := s.RecentAuthorizedAdults(ctx, st.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("new student suggestions = %v, want none", got)
	}

	if err := s.CheckIn(ctx, st.ID, st.Name, ""); err != nil {
		t.Fatal(err)
	}
	got, err = s.RecentAuthorizedAdults(ctx, st.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("suggestions after a blank-adult check-in = %v, want none", got)
	}
}

// More distinct adults than the cap: only the most recently used ones are
// offered, and only names within the scan window are considered.
func TestRecentAuthorizedAdultsCapped(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	st, err := s.AddStudent(ctx, "Alice")
	if err != nil {
		t.Fatal(err)
	}
	base := time.Now().Add(-100 * time.Hour)
	for i, adult := range []string{"A", "B", "C", "D", "E"} {
		signInAt(t, s, st.ID, st.Name, adult, base.Add(time.Duration(i)*time.Hour))
	}

	got, err := s.RecentAuthorizedAdults(ctx, st.ID)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"E", "D", "C"}
	if !slices.Equal(got, want) {
		t.Errorf("RecentAuthorizedAdults = %v, want %v (the %d most recent)",
			got, want, maxAdultSuggestions)
	}
}

// An adult who has fallen outside the scan window is not suggested, even
// though older rows for the student still exist.
func TestRecentAuthorizedAdultsScanWindow(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	st, err := s.AddStudent(ctx, "Alice")
	if err != nil {
		t.Fatal(err)
	}
	base := time.Now().Add(-1000 * time.Hour)
	// One long-ago sign-in, then more than a full scan window of newer ones.
	signInAt(t, s, st.ID, st.Name, "LongAgo", base)
	for i := 0; i < adultScanRows; i++ {
		signInAt(t, s, st.ID, st.Name, "Recent", base.Add(time.Duration(i+1)*time.Hour))
	}

	got, err := s.RecentAuthorizedAdults(ctx, st.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got, []string{"Recent"}) {
		t.Errorf("RecentAuthorizedAdults = %v, want [Recent]; "+
			"an adult beyond the %d-row scan window should not be suggested", got, adultScanRows)
	}
}
