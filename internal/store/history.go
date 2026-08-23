package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/pglass/checkin/db/gen"
)

// HistorySort selects the ordering of history results. Results are always
// ascending.
type HistorySort int

const (
	SortEventTime   HistorySort = iota // by event Timestamp
	SortStudentName                    // by StudentName, then Timestamp
)

// historyQueryTimeout bounds a history query so a pathological unbounded count
// or scan can't hang the caller. Queries run off the UI thread regardless.
const historyQueryTimeout = 30 * time.Second

// HistoryQuery describes a history lookup over the Log table.
type HistoryQuery struct {
	Start, End *time.Time // nil = unbounded (whole-day bounds applied by History)
	// StudentIDs selects which students to include; empty = all students.
	// Selection is by ID, not name: a name is two columns now, and the picker
	// already tracks its selection by ID.
	StudentIDs []int64
	Sort       HistorySort
}

// HistoryRow is one check-in/out event.
type HistoryRow struct {
	Name      Name
	Action    string // ActionCheckedIn | ActionCheckedOut
	Timestamp time.Time
	// AuthorizedAdult is the name the parent or authorized adult typed when
	// signing the student in or out. Empty for rows written before the field
	// existed, or where none was entered (stored as NULL).
	AuthorizedAdult string
}

// History returns up to limit matching rows plus the total match count
// (independent of limit). Date bounds are applied at whole-day granularity:
// Start at 00:00:00 local, End at 23:59:59 local. An empty StudentIDs matches
// all students. All access is through sqlc-generated queries.
func (s *Store) History(ctx context.Context, q HistoryQuery, limit int) ([]HistoryRow, int, error) {
	ctx, cancel := context.WithTimeout(ctx, historyQueryTimeout)
	defer cancel()

	// Optional bounds are toggled by a companion "has_*" flag (0/1) rather than a
	// nullable param. This keeps every placeholder referenced exactly once so the
	// generated positional binding lines up (nargs referenced twice produced
	// numbered placeholders whose positions no longer matched the bound args).
	hasStart, start := int64(0), int64(0)
	if q.Start != nil {
		hasStart, start = 1, startOfDay(*q.Start).Unix()
	}
	hasEnd, end := int64(0), int64(0)
	if q.End != nil {
		hasEnd, end = 1, endOfDay(*q.End).Unix()
	}

	// Students are matched via json_each over a JSON array (a single, fixed
	// placeholder) rather than a variable-length IN-list: mixing sqlc.slice with
	// params after it (LIMIT) broke the generated placeholder numbering. When no
	// students are chosen, all_students=1 bypasses the filter and the JSON is an
	// (unused) empty array.
	allStudents := int64(0)
	if len(q.StudentIDs) == 0 {
		allStudents = 1
	}
	idsJSON, err := json.Marshal(q.StudentIDs)
	if err != nil {
		return nil, 0, err
	}
	if len(q.StudentIDs) == 0 {
		idsJSON = []byte("[]")
	}

	total, err := s.q.CountHistory(ctx, gen.CountHistoryParams{
		HasStart: hasStart, Start: start, HasEnd: hasEnd, End: end,
		AllStudents: allStudents, IdsJson: string(idsJSON),
	})
	if err != nil {
		return nil, 0, err
	}

	var rows []HistoryRow
	switch q.Sort {
	case SortStudentName:
		got, err := s.q.HistoryByName(ctx, gen.HistoryByNameParams{
			HasStart: hasStart, Start: start, HasEnd: hasEnd, End: end,
			AllStudents: allStudents, IdsJson: string(idsJSON), Lim: int64(limit),
		})
		if err != nil {
			return nil, 0, err
		}
		rows = make([]HistoryRow, len(got))
		for i, r := range got {
			rows[i] = HistoryRow{
				Name:            Name{First: r.Firstname, Last: r.Lastname},
				Action:          r.Action,
				Timestamp:       time.Unix(r.Timestamp, 0),
				AuthorizedAdult: r.Authorizedadult.String,
			}
		}
	default: // SortEventTime
		got, err := s.q.HistoryByTime(ctx, gen.HistoryByTimeParams{
			HasStart: hasStart, Start: start, HasEnd: hasEnd, End: end,
			AllStudents: allStudents, IdsJson: string(idsJSON), Lim: int64(limit),
		})
		if err != nil {
			return nil, 0, err
		}
		rows = make([]HistoryRow, len(got))
		for i, r := range got {
			rows[i] = HistoryRow{
				Name:            Name{First: r.Firstname, Last: r.Lastname},
				Action:          r.Action,
				Timestamp:       time.Unix(r.Timestamp, 0),
				AuthorizedAdult: r.Authorizedadult.String,
			}
		}
	}

	return rows, int(total), nil
}

// startOfDay returns local midnight of t's calendar day.
func startOfDay(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}

// endOfDay returns the last second (23:59:59) of t's calendar day.
func endOfDay(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 23, 59, 59, 0, t.Location())
}
