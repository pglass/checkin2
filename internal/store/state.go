package store

import (
	"context"
	"sync"
	"time"

	"github.com/pglass/checkin/db/gen"
)

// InOut holds a student's check-in/out times for the current day.
type InOut struct {
	In  *time.Time
	Out *time.Time
}

// DayState is the in-memory "today" state, rebuilt from the Log at startup and
// on calendar-day rollover, and updated on every mutation.
type DayState struct {
	mu    sync.RWMutex
	day   time.Time // local midnight for the day this state represents
	inout map[int64]*InOut
}

func newDayState() *DayState {
	return &DayState{day: startOfToday(), inout: map[int64]*InOut{}}
}

// startOfToday returns local midnight for the current day.
func startOfToday() time.Time {
	now := time.Now()
	return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
}

// rebuild reloads today's check-in/out state from the Log.
func (d *DayState) rebuild(ctx context.Context, q *gen.Queries) error {
	day := startOfToday()
	logs, err := q.LogSince(ctx, day.Unix())
	if err != nil {
		return err
	}
	m := make(map[int64]*InOut, len(logs))
	for _, l := range logs {
		if !l.Studentid.Valid {
			continue
		}
		id := l.Studentid.Int64
		io := m[id]
		if io == nil {
			io = &InOut{}
			m[id] = io
		}
		t := time.Unix(l.Timestamp, 0)
		switch l.Action {
		case ActionCheckedIn:
			tt := t
			io.In = &tt
		case ActionCheckedOut:
			tt := t
			io.Out = &tt
		}
	}
	d.mu.Lock()
	d.day = day
	d.inout = m
	d.mu.Unlock()
	return nil
}

// maybeRollover rebuilds the state if the calendar day has changed since the
// state was last built (e.g. the app was left open overnight).
func (d *DayState) maybeRollover(ctx context.Context, q *gen.Queries) {
	d.mu.RLock()
	stale := !startOfToday().Equal(d.day)
	d.mu.RUnlock()
	if stale {
		_ = d.rebuild(ctx, q)
	}
}

func (d *DayState) get(id int64) (in, out *time.Time) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	if io := d.inout[id]; io != nil {
		return io.In, io.Out
	}
	return nil, nil
}

func (d *DayState) setIn(id int64, t time.Time) {
	d.mu.Lock()
	defer d.mu.Unlock()
	io := d.inout[id]
	if io == nil {
		io = &InOut{}
		d.inout[id] = io
	}
	tt := t
	io.In = &tt
}

func (d *DayState) setOut(id int64, t time.Time) {
	d.mu.Lock()
	defer d.mu.Unlock()
	io := d.inout[id]
	if io == nil {
		io = &InOut{}
		d.inout[id] = io
	}
	tt := t
	io.Out = &tt
}

func (d *DayState) clear(id int64) {
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.inout, id)
}
