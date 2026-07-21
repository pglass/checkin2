// Command seed populates a SQLite database with synthetic students and a history
// of daily check-in/out log rows, for load-testing and development.
//
// For each of the past D days, every student gets a check-in and check-out time,
// except: a randomized ~1% of students never check in that day, and a separate
// randomized ~1% check in but never check out.
//
// Example:
//
//	go run ./cmd/seed -db-path /tmp/big.db -n 500 -d 180d
package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"time"

	"github.com/pglass/checkin/db/gen"
	"github.com/pglass/checkin/internal/store"
)

func main() {
	dbPath := flag.String("db-path", "", "SQLite database path (required)")
	n := flag.Int("n", 100, "number of students to create")
	dur := flag.Duration("d", 180*24*time.Hour, "history duration to backfill, e.g. 720h, 30m")
	seed := flag.Int64("seed", 0, "random seed (0 = time-based)")
	flag.Parse()

	if *dbPath == "" {
		log.Fatal("-db-path is required")
	}
	if *n <= 0 {
		log.Fatal("-n must be positive")
	}

	rng := rand.New(rand.NewSource(*seed))
	if *seed == 0 {
		rng = rand.New(rand.NewSource(time.Now().UnixNano()))
	}

	s, err := store.Open(*dbPath)
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer s.Close()
	q := s.Queries()
	ctx := context.Background()

	students, err := createStudents(ctx, q, *n)
	if err != nil {
		log.Fatalf("create students: %v", err)
	}
	log.Printf("created %d students", len(students))

	rows, err := backfill(ctx, s, q, students, *dur, rng)
	if err != nil {
		log.Fatalf("backfill: %v", err)
	}
	log.Printf("wrote %d check-in/out log rows across %s", rows, dur.String())
}

// createStudents inserts n students with unique names, each with an "Added" log
// row (mirroring store.AddStudent) so the history is complete.
func createStudents(ctx context.Context, q *gen.Queries, n int) ([]gen.Student, error) {
	out := make([]gen.Student, 0, n)
	now := time.Now()
	for i := 0; i < n; i++ {
		name := fmt.Sprintf("Student %05d", i+1)
		st, err := q.AddStudent(ctx, name)
		if err != nil {
			return nil, fmt.Errorf("add %q: %w", name, err)
		}
		if err := appendLog(ctx, q, st, store.ActionAdded, now); err != nil {
			return nil, fmt.Errorf("log added %q: %w", name, err)
		}
		out = append(out, st)
	}
	return out, nil
}

// backfill writes a check-in/out pair per student per past day, applying the
// 1%/1% no-show and no-checkout rules. It runs inside a single transaction for
// speed. Returns the number of log rows written.
func backfill(ctx context.Context, s *store.Store, q *gen.Queries, students []gen.Student, dur time.Duration, rng *rand.Rand) (int, error) {
	days := int(dur.Hours() / 24)
	if days < 1 {
		days = 1
	}

	tx, err := s.DB().BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	qtx := q.WithTx(tx)

	written := 0
	// Midnight today, local; walk back one day at a time.
	midnight := time.Now().Truncate(24 * time.Hour)
	for d := 1; d <= days; d++ {
		day := midnight.AddDate(0, 0, -d)
		for _, st := range students {
			roll := rng.Float64()
			switch {
			case roll < 0.01:
				// ~1%: never checks in this day. No rows.
				continue
			case roll < 0.02:
				// ~1%: checks in but never checks out. In row only.
				in := checkInTime(day, rng)
				if err := appendLog(ctx, qtx, st, store.ActionCheckedIn, in); err != nil {
					return written, err
				}
				written++
			default:
				in := checkInTime(day, rng)
				out := checkOutTime(in, rng)
				if err := appendLog(ctx, qtx, st, store.ActionCheckedIn, in); err != nil {
					return written, err
				}
				if err := appendLog(ctx, qtx, st, store.ActionCheckedOut, out); err != nil {
					return written, err
				}
				written += 2
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return written, err
	}
	return written, nil
}

// checkInTime returns a randomized morning arrival (07:00–09:59) on day.
func checkInTime(day time.Time, rng *rand.Rand) time.Time {
	hour := 7 + rng.Intn(3)
	min := rng.Intn(60)
	return time.Date(day.Year(), day.Month(), day.Day(), hour, min, rng.Intn(60), 0, day.Location())
}

// checkOutTime returns a randomized departure 6–9 hours after check-in.
func checkOutTime(in time.Time, rng *rand.Rand) time.Time {
	return in.Add(time.Duration(6*60+rng.Intn(3*60)) * time.Minute)
}

func appendLog(ctx context.Context, q *gen.Queries, st gen.Student, action string, t time.Time) error {
	return q.AppendLog(ctx, gen.AppendLogParams{
		Studentid:   sql.NullInt64{Int64: st.ID, Valid: true},
		Studentname: st.Name,
		Action:      action,
		Timestamp:   t.Unix(),
	})
}
