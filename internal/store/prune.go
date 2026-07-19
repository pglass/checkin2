package store

import (
	"context"
	"database/sql"
	"time"

	"github.com/pglass/checkin/db/gen"
)

// Retention is how long Log entries are kept before pruning.
const Retention = 180 * 24 * time.Hour

// pruneBatchPause is the sleep between delete batches to keep pruning unnoticeable.
const pruneBatchPause = 200 * time.Millisecond

// StartPruner launches a background goroutine that periodically deletes Log
// rows older than Retention, in small batches. It does not run at startup;
// the first pass fires after interval. Returns when ctx is cancelled.
func (s *Store) StartPruner(ctx context.Context, interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.pruneOnce(ctx)
			}
		}
	}()
}

// pruneOnce deletes all rows older than the cutoff, 100 at a time.
func (s *Store) pruneOnce(ctx context.Context) {
	cutoff := time.Now().Add(-Retention).Unix()
	for {
		if ctx.Err() != nil {
			return
		}
		ids, err := s.q.OldestLogIDs(ctx, cutoff)
		if err != nil || len(ids) == 0 {
			return
		}
		if err := s.q.DeleteLogByIDs(ctx, ids); err != nil {
			return
		}
		if len(ids) < 100 {
			return // last (partial) batch
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(pruneBatchPause):
		}
	}
}

// insertLogForTest is a test helper to seed Log rows at arbitrary timestamps.
func (s *Store) insertLogForTest(ctx context.Context, name string, t time.Time) error {
	return s.q.AppendLog(ctx, gen.AppendLogParams{
		Studentid:   sql.NullInt64{},
		Studentname: name,
		Action:      ActionCheckedIn,
		Timestamp:   t.Unix(),
	})
}
