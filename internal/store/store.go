// Package store owns the SQLite database, the append-only Log, and the
// in-memory "today" check-in/out state.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/gofrs/flock"
	dbpkg "github.com/pglass/checkin/db"
	"github.com/pglass/checkin/db/gen"
	_ "modernc.org/sqlite"
)

// connPragmas are applied to every pooled connection (via the DSN), not just the
// first. busy_timeout and foreign_keys are per-connection settings, so setting
// them once after Open would leave any second connection the pool creates (e.g.
// the pruner writing while the UI reads) without them — a lock collision on that
// connection would fail immediately instead of waiting. journal_mode=WAL is
// stored in the database file, but is listed here so it is guaranteed set.
var connPragmas = []string{
	"busy_timeout(5000)",  // wait up to 5s for a lock instead of failing
	"journal_mode(WAL)",   // readers don't block the single writer
	"synchronous(NORMAL)", // durable enough for WAL, faster than FULL
	"foreign_keys(ON)",
}

// dsn builds a modernc.org/sqlite connection string that applies connPragmas on
// each new connection. url encoding keeps paths with spaces (e.g. macOS
// "Application Support") valid.
func dsn(path string) string {
	q := url.Values{}
	for _, p := range connPragmas {
		q.Add("_pragma", p)
	}
	return "file:" + path + "?" + q.Encode()
}

// Action values written to the Log table.
const (
	ActionAdded      = "Added"
	ActionCheckedIn  = "Checked In"
	ActionCheckedOut = "Checked Out"
	ActionDeleted    = "Deleted"
)

// ErrDuplicateName is returned by AddStudent when the name already exists.
var ErrDuplicateName = errors.New("a student with that name already exists")

// ErrAlreadyOpen is returned by Open when another process already holds this
// Center's lock, i.e. a second instance is trying to open the same database.
var ErrAlreadyOpen = errors.New("this Center is already open in another window")

// lockSuffix is appended to the database path to name the sidecar lock file
// that carries the exclusive advisory lock enforcing one process per Center.
const lockSuffix = ".lock"

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
	db   *sql.DB
	q    *gen.Queries
	day  *DayState
	lock *flock.Flock
}

// Open opens (or creates) the database at path, applies pragmas and schema,
// and rebuilds the in-memory "today" state from the Log.
//
// Open first takes an exclusive OS advisory lock on a sidecar "<path>.lock"
// file, enforcing one process per Center: if another instance already holds it,
// Open returns ErrAlreadyOpen without touching the database. The lock is an
// flock(2) lock on macOS/Linux and a LockFileEx lock on Windows; either way the
// OS releases it automatically if this process dies, so there is no stale lock
// to clean up (unlike a PID file). See Close for release on normal shutdown.
func Open(path string) (*Store, error) {
	lock := flock.New(path + lockSuffix)
	locked, err := lock.TryLock()
	if err != nil {
		return nil, fmt.Errorf("acquire center lock: %w", err)
	}
	if !locked {
		return nil, ErrAlreadyOpen
	}

	// Pragmas are carried in the DSN so they apply to every pooled connection,
	// not just the first (see connPragmas). The UI and the background pruner can
	// each hold their own connection, and both must have busy_timeout set.
	database, err := sql.Open("sqlite", dsn(path))
	if err != nil {
		_ = lock.Unlock()
		return nil, err
	}

	if _, err := database.Exec(dbpkg.Schema); err != nil {
		database.Close()
		_ = lock.Unlock()
		return nil, fmt.Errorf("apply schema: %w", err)
	}

	s := &Store{
		db:   database,
		q:    gen.New(database),
		day:  newDayState(),
		lock: lock,
	}
	if err := s.day.rebuild(context.Background(), s.q); err != nil {
		database.Close()
		_ = lock.Unlock()
		return nil, err
	}
	return s, nil
}

// Close closes the underlying database and releases the Center lock.
func (s *Store) Close() error {
	err := s.db.Close()
	if s.lock != nil {
		if uerr := s.lock.Unlock(); uerr != nil && err == nil {
			err = uerr
		}
	}
	return err
}

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
	slog.Debug("student added", "id", st.ID, "name", st.Name)
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
	slog.Debug("student removed", "id", id, "name", name)
	return nil
}

// CheckIn records a check-in for today. adult is the name typed by the parent
// or authorized adult signing the student in; "" is stored as NULL.
func (s *Store) CheckIn(ctx context.Context, id int64, name, adult string) error {
	now := time.Now()
	if err := s.appendLogAt(ctx, id, name, ActionCheckedIn, now, adult); err != nil {
		return err
	}
	s.day.setIn(id, now)
	slog.Debug("student checked in", "id", id, "name", name, "adult", adult, "at", now)
	return nil
}

// CheckOut records a check-out for today. adult is as in CheckIn.
func (s *Store) CheckOut(ctx context.Context, id int64, name, adult string) error {
	now := time.Now()
	if err := s.appendLogAt(ctx, id, name, ActionCheckedOut, now, adult); err != nil {
		return err
	}
	s.day.setOut(id, now)
	slog.Debug("student checked out", "id", id, "name", name, "adult", adult, "at", now)
	return nil
}

// adultScanRows is how many of a student's most recent check-in/out rows
// RecentAuthorizedAdults looks at. Bounded so the cost of the suggestion query
// does not grow with a student's history (two years of it, under the retention
// policy). Large enough that a family alternating two or three adults still
// surfaces all of them.
const adultScanRows = 20

// maxAdultSuggestions is how many distinct names the check-in/out dialog offers.
// Past three, scanning the buttons is slower than typing a short name.
const maxAdultSuggestions = 3

// RecentAuthorizedAdults returns the distinct adults who recently signed this
// student in or out, most recently used first, for the dialog's quick-pick
// buttons. Empty names are excluded, so students with no history (or only
// pre-existing rows) simply get no suggestions and the adult types one.
//
// The query seeks idx_log_student_ts and scans at most adultScanRows rows, so
// it stays sub-millisecond and is safe to run each time the pop-up opens.
func (s *Store) RecentAuthorizedAdults(ctx context.Context, studentID int64) ([]string, error) {
	got, err := s.q.RecentAuthorizedAdults(ctx, gen.RecentAuthorizedAdultsParams{
		StudentID: sql.NullInt64{Int64: studentID, Valid: true},
		Scan:      adultScanRows,
		Lim:       maxAdultSuggestions,
	})
	if err != nil {
		return nil, err
	}
	adults := make([]string, 0, len(got))
	for _, a := range got {
		if a.Valid && a.String != "" {
			adults = append(adults, a.String)
		}
	}
	return adults, nil
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

// lastActivity returns the row's most recent check-in/out time for today, or
// nil if the student has neither.
func lastActivity(r StudentRow) *time.Time {
	switch {
	case r.Out != nil && r.In != nil:
		if r.Out.After(*r.In) {
			return r.Out
		}
		return r.In
	case r.Out != nil:
		return r.Out
	default:
		return r.In
	}
}

// Students returns the main-list rows, merging the student table with today's state.
//
// Rows are ordered by today's most recent check-in/out (newest first), then by
// name for students with no activity today, so the list surfaces who just
// scanned while keeping the rest alphabetical.
//
// The UI calls this on every refresh (each check-in/out, add, remove, reset)
// rather than caching and mutating a row slice. That is deliberate: the only
// database work here is ListStudents, a name lookup against a local WAL-mode
// file, and the times come from the in-memory DayState, so a refresh costs
// well under a millisecond even at a thousand students and happens once per
// user action, never per camera frame. Keeping one path that builds the list
// means the view cannot drift from the data -- a cache would have to be
// invalidated correctly on add, remove, reset, check-in, check-out, bulk
// import, and day rollover, and a stale list is a worse bug than a slow one.
// If the list ever does feel slow, narrow ListStudents (it selects every
// column for a view that needs only ID and Name) before reaching for a cache.
// Sorting in SQL is not the cheaper option either: the sort key lives in
// DayState, so SQL would have to re-derive today's state from the Log.
//
// Caveat: DayState holds times at one-second resolution (the Log stores unix
// seconds), so two students scanned within the same second fall back to name
// order rather than scan order. Fine for a kiosk; preserving exact scan order
// would need millisecond timestamps or a sequence column, i.e. a schema change.
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
	// ListStudents already returns rows by name, so a stable sort keeps the
	// no-activity rows alphabetical.
	sort.SliceStable(rows, func(i, j int) bool {
		a, b := lastActivity(rows[i]), lastActivity(rows[j])
		if a == nil || b == nil {
			return a != nil // rows with activity today come first
		}
		return a.After(*b)
	})
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

// DB exposes the underlying connection for bulk operations (e.g. seeding) that
// need transaction control.
func (s *Store) DB() *sql.DB { return s.db }

// appendLog writes a row with no authorized adult (Added/Deleted).
func (s *Store) appendLog(ctx context.Context, id int64, name, action string) error {
	return s.appendLogAt(ctx, id, name, action, time.Now(), "")
}

// appendLogAt writes one Log row. An empty adult is stored as NULL, which is
// the case for actions where it does not apply or where none was entered.
func (s *Store) appendLogAt(ctx context.Context, id int64, name, action string, t time.Time, adult string) error {
	adult = strings.TrimSpace(adult)
	return s.q.AppendLog(ctx, gen.AppendLogParams{
		Studentid:       sql.NullInt64{Int64: id, Valid: true},
		Studentname:     name,
		Action:          action,
		Timestamp:       t.Unix(),
		Authorizedadult: sql.NullString{String: adult, Valid: adult != ""},
	})
}

func isUniqueViolation(err error) bool {
	// modernc.org/sqlite surfaces this in the error text.
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "unique")
}
