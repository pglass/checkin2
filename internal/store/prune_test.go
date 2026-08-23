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
		if err := s.insertLogForTest(ctx, testName("Old"), old); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 5; i++ {
		if err := s.insertLogForTest(ctx, testName("Recent"), recent); err != nil {
			t.Fatal(err)
		}
	}

	if got := countLog(t, s); got != 255 {
		t.Fatalf("seed count = %d, want 255", got)
	}

	// One pass deletes at most a single 100-row batch, never draining the whole
	// backlog at once.
	s.pruneOnce(ctx, 100)
	if got := countLog(t, s); got != 155 {
		t.Fatalf("after 1 pass count = %d, want 155 (255 - one 100-row batch)", got)
	}

	// Subsequent passes trickle the rest away, 100 at a time.
	s.pruneOnce(ctx, 100)
	if got := countLog(t, s); got != 55 {
		t.Fatalf("after 2 passes count = %d, want 55", got)
	}

	// Third pass clears the remaining 50 old rows (partial batch); the 5 recent
	// rows are within retention and must survive.
	s.pruneOnce(ctx, 100)
	if got := countLog(t, s); got != 5 {
		t.Fatalf("after 3 passes count = %d, want 5 (only recent rows remain)", got)
	}

	// A further pass with nothing old enough is a no-op.
	s.pruneOnce(ctx, 100)
	if got := countLog(t, s); got != 5 {
		t.Fatalf("extra pass changed count to %d, want 5", got)
	}
}
