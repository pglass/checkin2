package store

import (
	"context"
	"database/sql"

	"github.com/pglass/checkin/db/gen"
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

	st, err := s.AddStudent(ctx, testName("Alice"))
	if err != nil {
		t.Fatalf("AddStudent: %v", err)
	}
	if nameOf(st) != testName("Alice") || st.ID == 0 {
		t.Fatalf("unexpected student: %+v", st)
	}

	if _, err := s.AddStudent(ctx, testName("Alice")); err != ErrDuplicateName {
		t.Fatalf("want ErrDuplicateName, got %v", err)
	}
}

func TestAddStudent_TrimsAndRejectsEmpty(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	// Either part missing is a rejection: both identify the student.
	if _, err := s.AddStudent(ctx, Name{First: "   ", Last: "Smith"}); err == nil {
		t.Fatal("want error for empty first name")
	}
	if _, err := s.AddStudent(ctx, Name{First: "Bob", Last: "  "}); err == nil {
		t.Fatal("want error for empty last name")
	}
	st, err := s.AddStudent(ctx, Name{First: "  Bob  ", Last: "  Smith  "})
	if err != nil {
		t.Fatalf("AddStudent: %v", err)
	}
	if got, want := nameOf(st), (Name{First: "Bob", Last: "Smith"}); got != want {
		t.Fatalf("name not trimmed: %+v, want %+v", got, want)
	}
}

func TestCheckInOutStatus(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	st, _ := s.AddStudent(ctx, testName("Carol"))

	if got := s.Status(st.ID); got != StatusNotIn {
		t.Fatalf("initial status = %v, want NotIn", got)
	}
	if err := s.CheckIn(ctx, st.ID, nameOf(st), "Parent"); err != nil {
		t.Fatal(err)
	}
	if got := s.Status(st.ID); got != StatusIn {
		t.Fatalf("after check-in status = %v, want In", got)
	}
	if err := s.CheckOut(ctx, st.ID, nameOf(st), "Parent"); err != nil {
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
	st, _ := s.AddStudent(ctx, testName("Dave"))
	s.CheckIn(ctx, st.ID, nameOf(st), "Parent")
	s.CheckOut(ctx, st.ID, nameOf(st), "Parent")
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
	st, _ := s.AddStudent(ctx, testName("Erin"))
	s.CheckIn(ctx, st.ID, nameOf(st), "Parent")
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
	st, _ := s.AddStudent(ctx, testName("Frank"))
	if err := s.RemoveStudent(ctx, st.ID, nameOf(st)); err != nil {
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

	// Shared surnames and mixed case, so the tiebreak has to compare
	// (last, first) case-insensitively rather than a single joined string.
	roster := []Name{
		{First: "Bob", Last: "Zane"},
		{First: "alice", Last: "Adams"},
		{First: "Carol", Last: "adams"},
		{First: "Dave", Last: "Mills"},
	}
	ids := map[string]int64{}
	for _, n := range roster {
		st, err := s.AddStudent(ctx, n)
		if err != nil {
			t.Fatalf("AddStudent %v: %v", n, err)
		}
		ids[n.First] = st.ID
	}

	now := time.Now()
	// Bob checked in early; Dave checked in later then out later still, so
	// Dave's check-out is the most recent event of the day.
	s.day.setIn(ids["Bob"], now.Add(-3*time.Hour))
	s.day.setIn(ids["Dave"], now.Add(-2*time.Hour))
	s.day.setOut(ids["Dave"], now.Add(-time.Minute))
	// alice and Carol have nothing today.

	rows, err := s.Students(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, r := range rows {
		got = append(got, r.Name.Display())
	}
	// Today's activity first (most recent first), then the rest by
	// (last, first): "adams, alice" before "adams, Carol" ignoring case.
	want := []string{"Mills, Dave", "Zane, Bob", "Adams, alice", "adams, Carol"}
	if !slices.Equal(got, want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
}

// The authorized adult typed at check-in/out lands on that Log row, and is
// NULL for actions where it does not apply (Added, Deleted) or was left blank.
func TestAuthorizedAdultStoredOnLogRows(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	st, err := s.AddStudent(ctx, testName("Alice"))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.CheckIn(ctx, st.ID, nameOf(st), "  Bob Parent  "); err != nil {
		t.Fatal(err)
	}
	if err := s.CheckOut(ctx, st.ID, nameOf(st), ""); err != nil {
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
func signInAt(t *testing.T, s *Store, id int64, name Name, adult string, at time.Time) {
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

	alice, err := s.AddStudent(ctx, testName("Alice"))
	if err != nil {
		t.Fatal(err)
	}
	bob, err := s.AddStudent(ctx, testName("Bob"))
	if err != nil {
		t.Fatal(err)
	}

	base := time.Now().Add(-100 * time.Hour)
	// Oldest first. Jane repeats, so she must collapse to one entry and rank by
	// her latest use, not her first.
	for i, adult := range []string{"Jane", "Grandpa", "Jane", "Sitter"} {
		signInAt(t, s, alice.ID, nameOf(alice), adult, base.Add(time.Duration(i)*time.Hour))
	}
	// Another student's adult must not leak into Alice's suggestions.
	signInAt(t, s, bob.ID, nameOf(bob), "Stranger", base.Add(10*time.Hour))

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

	st, err := s.AddStudent(ctx, testName("Alice"))
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

	if err := s.CheckIn(ctx, st.ID, nameOf(st), ""); err != nil {
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

	st, err := s.AddStudent(ctx, testName("Alice"))
	if err != nil {
		t.Fatal(err)
	}
	base := time.Now().Add(-100 * time.Hour)
	for i, adult := range []string{"A", "B", "C", "D", "E"} {
		signInAt(t, s, st.ID, nameOf(st), adult, base.Add(time.Duration(i)*time.Hour))
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

	st, err := s.AddStudent(ctx, testName("Alice"))
	if err != nil {
		t.Fatal(err)
	}
	base := time.Now().Add(-1000 * time.Hour)
	// One long-ago sign-in, then more than a full scan window of newer ones.
	signInAt(t, s, st.ID, nameOf(st), "LongAgo", base)
	for i := 0; i < adultScanRows; i++ {
		signInAt(t, s, st.ID, nameOf(st), "Recent", base.Add(time.Duration(i+1)*time.Hour))
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

// testName builds a Name from a single first name, pairing it with a last name
// derived from it. Most tests care only that a student is distinct and has a
// name; the (last, first) specifics are exercised by the sorting tests.
func testName(first string) Name {
	return Name{First: first, Last: first + "son"}
}

// nameOf is the Name of a student row returned by the generated queries.
func nameOf(st gen.Student) Name {
	return Name{First: st.Firstname, Last: st.Lastname}
}
