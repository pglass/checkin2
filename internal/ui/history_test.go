package ui

import (
	"testing"
	"time"

	"github.com/pglass/checkin/internal/store"
)

// A history line names the authorized adult who signed the student in or out,
// and omits the "by ..." clause when no adult is on record (rows written
// before the field existed, or where none was entered).
func TestFormatHistoryLine(t *testing.T) {
	at := time.Date(2026, 7, 19, 19, 31, 30, 0, time.UTC)
	stamp := at.Format("2006-01-02 03:04:05 PM MST")

	tests := []struct {
		name string
		row  store.HistoryRow
		want string
	}{
		{
			name: "check-in with adult",
			row: store.HistoryRow{StudentName: "John Smith", Action: store.ActionCheckedIn,
				Timestamp: at, AuthorizedAdult: "Jane Smith"},
			want: stamp + " -- John Smith checked in by Jane Smith",
		},
		{
			name: "check-out with adult",
			row: store.HistoryRow{StudentName: "John Smith", Action: store.ActionCheckedOut,
				Timestamp: at, AuthorizedAdult: "Jane Smith"},
			want: stamp + " -- John Smith checked out by Jane Smith",
		},
		{
			name: "no adult on record",
			row: store.HistoryRow{StudentName: "John Smith", Action: store.ActionCheckedIn,
				Timestamp: at},
			want: stamp + " -- John Smith checked in",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := formatHistoryLine(tc.row); got != tc.want {
				t.Errorf("formatHistoryLine() = %q, want %q", got, tc.want)
			}
		})
	}
}
