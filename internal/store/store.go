// Package store owns the SQLite database, the append-only Log, and the
// in-memory "today" check-in/out state.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	dbpkg "github.com/pglass/checkin/db"
	"github.com/pglass/checkin/db/gen"
	_ "modernc.org/sqlite"
)

// Action values written to the Log table.
const (
	ActionAdded      = "Added"
	ActionCheckedIn  = "Checked In"
	ActionCheckedOut = "Checked Out"
	ActionDeleted    = "Deleted"
)

// ErrDuplicateName is returned by AddStudent when the name already exists.
var ErrDuplicateName = errors.New("a student with that name already exists")

// Status describes a student's check-in/out state for the current day.
type Status int

const (
	StatusNotIn Status = iota // not yet checked in
	StatusIn                  // checked in, not checked out
	StatusOut                 // checked in and out
)

// StudentRow is a view-model row for the main list.
type StudentRow struct {
	ID   int64
	Name string
	In   *time.Time
	Out  *time.Time
}

// Store wraps the database connection, generated queries, and day state.
type Store struct {
	db  *sql.DB
	q   *gen.Queries
	day *DayState
}

// DefaultDBPath returns the durable per-OS location for the database file,
// creating the parent directory if needed.
func DefaultDBPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	appDir := filepath.Join(dir, "checkin")
	if err := os.MkdirAll(appDir, 0o755); err != nil {
		return "", err
	}
	return filepath.Join(appDir, "checkin.db"), nil
}

// Open opens (or creates) the database at path, applies pragmas and schema,
// and rebuilds the in-memory "today" state from the Log.
func Open(path string) (*Store, error) {
	database, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	// Single-writer desktop app: WAL keeps background pruning from blocking reads.
	pragmas := []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA synchronous=NORMAL",
		"PRAGMA busy_timeout=5000",
		"PRAGMA foreign_keys=ON",
	}
	for _, p := range pragmas {
		if _, err := database.Exec(p); err != nil {
			database.Close()
			return nil, fmt.Errorf("pragma %q: %w", p, err)
		}
	}

	if _, err := database.Exec(dbpkg.Schema); err != nil {
		database.Close()
		return nil, fmt.Errorf("apply schema: %w", err)
	}

	s := &Store{
		db:  database,
		q:   gen.New(database),
		day: newDayState(),
	}
	if err := s.day.rebuild(context.Background(), s.q); err != nil {
		database.Close()
		return nil, err
	}
	return s, nil
}

// Close closes the underlying database.
func (s *Store) Close() error { return s.db.Close() }

// AddStudent inserts a new student and appends an "Added" log entry.
// Returns ErrDuplicateName if the name is already taken.
func (s *Store) AddStudent(ctx context.Context, name string) (gen.Student, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return gen.Student{}, errors.New("name must not be empty")
	}
	st, err := s.q.AddStudent(ctx, name)
	if err != nil {
		if isUniqueViolation(err) {
			return gen.Student{}, ErrDuplicateName
		}
		return gen.Student{}, err
	}
	if err := s.appendLog(ctx, st.ID, st.Name, ActionAdded); err != nil {
		return gen.Student{}, err
	}
	return st, nil
}

// RemoveStudent deletes a student and appends a "Deleted" log entry.
func (s *Store) RemoveStudent(ctx context.Context, id int64, name string) error {
	if err := s.q.DeleteStudent(ctx, id); err != nil {
		return err
	}
	if err := s.appendLog(ctx, id, name, ActionDeleted); err != nil {
		return err
	}
	s.day.clear(id)
	return nil
}

// CheckIn records a check-in for today.
func (s *Store) CheckIn(ctx context.Context, id int64, name string) error {
	now := time.Now()
	if err := s.appendLogAt(ctx, id, name, ActionCheckedIn, now); err != nil {
		return err
	}
	s.day.setIn(id, now)
	return nil
}

// CheckOut records a check-out for today.
func (s *Store) CheckOut(ctx context.Context, id int64, name string) error {
	now := time.Now()
	if err := s.appendLogAt(ctx, id, name, ActionCheckedOut, now); err != nil {
		return err
	}
	s.day.setOut(id, now)
	return nil
}

// Reset clears today's check-in/out for a student, deleting today's rows.
func (s *Store) Reset(ctx context.Context, id int64) error {
	err := s.q.DeleteTodayCheckinsForStudent(ctx, gen.DeleteTodayCheckinsForStudentParams{
		Studentid: sql.NullInt64{Int64: id, Valid: true},
		Timestamp: startOfToday().Unix(),
	})
	if err != nil {
		return err
	}
	s.day.clear(id)
	return nil
}

// Students returns the main-list rows, merging the student table with today's state.
func (s *Store) Students(ctx context.Context) ([]StudentRow, error) {
	s.day.maybeRollover(ctx, s.q)
	students, err := s.q.ListStudents(ctx)
	if err != nil {
		return nil, err
	}
	rows := make([]StudentRow, 0, len(students))
	for _, st := range students {
		in, out := s.day.get(st.ID)
		rows = append(rows, StudentRow{ID: st.ID, Name: st.Name, In: in, Out: out})
	}
	return rows, nil
}

// Status returns the current check-in/out status for a student.
func (s *Store) Status(id int64) Status {
	in, out := s.day.get(id)
	switch {
	case in != nil && out != nil:
		return StatusOut
	case in != nil:
		return StatusIn
	default:
		return StatusNotIn
	}
}

// StudentByName looks up a student by exact name (used by QR scanning).
func (s *Store) StudentByName(ctx context.Context, name string) (gen.Student, error) {
	return s.q.GetStudentByName(ctx, name)
}

// Queries exposes the raw querier for the pruner and tests.
func (s *Store) Queries() *gen.Queries { return s.q }

func (s *Store) appendLog(ctx context.Context, id int64, name, action string) error {
	return s.appendLogAt(ctx, id, name, action, time.Now())
}

func (s *Store) appendLogAt(ctx context.Context, id int64, name, action string, t time.Time) error {
	return s.q.AppendLog(ctx, gen.AppendLogParams{
		Studentid:   sql.NullInt64{Int64: id, Valid: true},
		Studentname: name,
		Action:      action,
		Timestamp:   t.Unix(),
	})
}

func isUniqueViolation(err error) bool {
	// modernc.org/sqlite surfaces this in the error text.
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "unique")
}
