package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/pglass/checkin/db/gen"
)

// logAt appends a Log row with an explicit name, action, and time.
func logAt(t *testing.T, s *Store, name, action string, at time.Time) {
	t.Helper()
	err := s.q.AppendLog(context.Background(), gen.AppendLogParams{
		Studentid:   sql.NullInt64{},
		Studentname: name,
		Action:      action,
		Timestamp:   at.Unix(),
	})
	if err != nil {
		t.Fatal(err)
	}
}

func openHistoryStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "hist.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestHistoryFiltersAndSort(t *testing.T) {
	ctx := context.Background()
	s := openHistoryStore(t)

	day := func(y int, m time.Month, d, h int) time.Time {
		return time.Date(y, m, d, h, 0, 0, 0, time.Local)
	}

	// Alice: in on the 10th, out on the 12th. Bob: in on the 11th.
	logAt(t, s, "Alice", ActionCheckedIn, day(2026, 7, 10, 8))
	logAt(t, s, "Alice", ActionCheckedOut, day(2026, 7, 12, 15))
	logAt(t, s, "Bob", ActionCheckedIn, day(2026, 7, 11, 9))
	// Noise that must never appear in history results:
	logAt(t, s, "Alice", ActionAdded, day(2026, 7, 9, 8))
	logAt(t, s, "Bob", ActionDeleted, day(2026, 7, 13, 8))

	t.Run("all, sorted by time", func(t *testing.T) {
		rows, total, err := s.History(ctx, HistoryQuery{Sort: SortEventTime}, 100)
		if err != nil {
			t.Fatal(err)
		}
		if total != 3 {
			t.Fatalf("total = %d, want 3 (Added/Deleted excluded)", total)
		}
		if len(rows) != 3 {
			t.Fatalf("rows = %d, want 3", len(rows))
		}
		// Ascending by time: Alice-in(10th), Bob-in(11th), Alice-out(12th).
		wantNames := []string{"Alice", "Bob", "Alice"}
		wantActs := []string{ActionCheckedIn, ActionCheckedIn, ActionCheckedOut}
		for i := range rows {
			if rows[i].StudentName != wantNames[i] || rows[i].Action != wantActs[i] {
				t.Fatalf("row %d = %s/%s, want %s/%s", i,
					rows[i].StudentName, rows[i].Action, wantNames[i], wantActs[i])
			}
		}
	})

	t.Run("sorted by name then time", func(t *testing.T) {
		rows, _, err := s.History(ctx, HistoryQuery{Sort: SortStudentName}, 100)
		if err != nil {
			t.Fatal(err)
		}
		// Alice's two events (in before out), then Bob's.
		want := []struct{ name, act string }{
			{"Alice", ActionCheckedIn},
			{"Alice", ActionCheckedOut},
			{"Bob", ActionCheckedIn},
		}
		for i, w := range want {
			if rows[i].StudentName != w.name || rows[i].Action != w.act {
				t.Fatalf("row %d = %s/%s, want %s/%s", i, rows[i].StudentName, rows[i].Action, w.name, w.act)
			}
		}
	})

	t.Run("date range whole-day inclusive", func(t *testing.T) {
		// 11th only → just Bob's check-in. Whole-day bounds must include the 11th.
		start := day(2026, 7, 11, 0)
		end := day(2026, 7, 11, 0)
		rows, total, err := s.History(ctx, HistoryQuery{Start: &start, End: &end}, 100)
		if err != nil {
			t.Fatal(err)
		}
		if total != 1 || len(rows) != 1 || rows[0].StudentName != "Bob" {
			t.Fatalf("11th-only got total=%d rows=%d, want 1 Bob", total, len(rows))
		}
	})

	t.Run("student filter", func(t *testing.T) {
		rows, total, err := s.History(ctx, HistoryQuery{StudentNames: []string{"Alice"}}, 100)
		if err != nil {
			t.Fatal(err)
		}
		if total != 2 || len(rows) != 2 {
			t.Fatalf("Alice filter got total=%d rows=%d, want 2", total, len(rows))
		}
		for _, r := range rows {
			if r.StudentName != "Alice" {
				t.Fatalf("unexpected name %q", r.StudentName)
			}
		}
	})
}

func TestHistoryTotalVsLimit(t *testing.T) {
	ctx := context.Background()
	s := openHistoryStore(t)

	base := time.Date(2026, 7, 1, 8, 0, 0, 0, time.Local)
	for i := 0; i < 50; i++ {
		logAt(t, s, "Alice", ActionCheckedIn, base.Add(time.Duration(i)*time.Minute))
	}

	rows, total, err := s.History(ctx, HistoryQuery{Sort: SortEventTime}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if total != 50 {
		t.Fatalf("total = %d, want 50 (independent of limit)", total)
	}
	if len(rows) != 10 {
		t.Fatalf("rows = %d, want 10 (capped by limit)", len(rows))
	}
}
