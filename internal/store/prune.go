package store

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	"github.com/pglass/checkin/db/gen"
)

// Retention is how long Log entries are kept before pruning.
const Retention = 180 * 24 * time.Hour

// defaultPruneBatchSize is the fallback max rows deleted per wake-up.
const defaultPruneBatchSize = 100

// remainingCountCap bounds the "rows left to prune" count so the scan stays
// cheap even with a huge backlog: the query stops after this many index rows, so
// counting is O(cap), not O(matches). A count that hits the cap is logged as
// "at least this many".
const remainingCountCap = 10000

// StartPruner launches a background goroutine that periodically deletes Log
// rows older than Retention, up to batchSize per wake-up, then sleeps for the
// full interval — pruning is a slow trickle, never a burst. It does not run at
// startup; the first pass fires after interval. Returns when ctx is cancelled.
func (s *Store) StartPruner(ctx context.Context, interval time.Duration, batchSize int) {
	if batchSize <= 0 {
		batchSize = defaultPruneBatchSize
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.pruneOnce(ctx, batchSize)
			}
		}
	}()
}

// pruneOnce deletes at most batchSize of the oldest rows past the retention
// cutoff, then returns. It does NOT loop to drain everything — a large backlog
// is worked off one batch per interval so the pruner never floods the database
// with back-to-back deletes.
func (s *Store) pruneOnce(ctx context.Context, batchSize int) {
	cutoff := time.Now().Add(-Retention).Unix()

	ids, err := s.q.OldestLogIDs(ctx, gen.OldestLogIDsParams{
		Timestamp: cutoff,
		Limit:     int64(batchSize),
	})
	if err != nil {
		slog.Warn("prune: failed to scan old log rows", "error", err)
		return
	}
	if len(ids) == 0 {
		return // nothing old enough
	}
	if err := s.q.DeleteLogByIDs(ctx, ids); err != nil {
		slog.Warn("prune: failed to delete log rows", "error", err, "batch", len(ids))
		return
	}

	// Report how many old rows remain, using a capped count so the scan is
	// bounded regardless of backlog size (see remainingCountCap). Beyond the cap
	// the exact total is unknown, so it is reported as "<cap>+".
	remaining, capped := s.remainingToPrune(ctx, cutoff)
	remainingStr := fmt.Sprintf("%d", remaining)
	if capped {
		remainingStr = fmt.Sprintf("%d+", remaining)
	}
	slog.Info("prune: removed old log rows", "count", len(ids), "remaining", remainingStr)
}

// remainingToPrune returns the number of rows still older than cutoff, counting
// at most remainingCountCap. The bool is true when the cap was hit, meaning at
// least that many remain (the true total is not computed, to bound the cost).
func (s *Store) remainingToPrune(ctx context.Context, cutoff int64) (int64, bool) {
	n, err := s.q.CountLogOlderThanCapped(ctx, gen.CountLogOlderThanCappedParams{
		Timestamp: cutoff,
		Limit:     remainingCountCap,
	})
	if err != nil {
		return 0, false
	}
	return n, n >= remainingCountCap
}

// insertLogForTest is a test helper to seed Log rows at arbitrary timestamps.
func (s *Store) insertLogForTest(ctx context.Context, name Name, t time.Time) error {
	return s.q.AppendLog(ctx, gen.AppendLogParams{
		Studentid: sql.NullInt64{},
		Firstname: name.First,
		Lastname:  name.Last,
		Action:    ActionCheckedIn,
		Timestamp: t.Unix(),
	})
}
