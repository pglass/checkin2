package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func countLog(t *testing.T, s *Store) int {
	t.Helper()
	var n int
	if err := s.db.QueryRow("SELECT COUNT(*) FROM Log").Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestPruneOnce(t *testing.T) {
	ctx := context.Background()
	s, err := Open(filepath.Join(t.TempDir(), "prune.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	now := time.Now()
	old := now.Add(-Retention - 24*time.Hour) // older than cutoff
	recent := now.Add(-24 * time.Hour)        // within retention

	// 250 old rows (spans >2 batches of 100) + 5 recent rows.
	for i := 0; i < 250; i++ {
		if err := s.insertLogForTest(ctx, "old", old); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 5; i++ {
		if err := s.insertLogForTest(ctx, "recent", recent); err != nil {
			t.Fatal(err)
		}
	}

	if got := countLog(t, s); got != 255 {
		t.Fatalf("seed count = %d, want 255", got)
	}

	s.pruneOnce(ctx)

	// Only the 5 recent rows should remain.
	if got := countLog(t, s); got != 5 {
		t.Fatalf("after prune count = %d, want 5", got)
	}
}
