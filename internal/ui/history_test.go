package ui

import (
	"slices"
	"testing"
	"time"

	"fyne.io/fyne/v2/test"

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
			row: store.HistoryRow{Name: store.Name{First: "John", Last: "Smith"},
				Action: store.ActionCheckedIn, Timestamp: at, AuthorizedAdult: "Jane Smith"},
			want: stamp + " -- John Smith checked in by Jane Smith",
		},
		{
			name: "check-out with adult",
			row: store.HistoryRow{Name: store.Name{First: "John", Last: "Smith"},
				Action: store.ActionCheckedOut, Timestamp: at, AuthorizedAdult: "Jane Smith"},
			want: stamp + " -- John Smith checked out by Jane Smith",
		},
		{
			name: "no adult on record",
			row: store.HistoryRow{Name: store.Name{First: "John", Last: "Smith"},
				Action: store.ActionCheckedIn, Timestamp: at},
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

// The student picker (used by the History window) lists students as
// "Last, First" and sorts by (last, first), case-insensitively.
func TestStudentSelectDisplaysAndSortsByLastFirst(t *testing.T) {
	fa := test.NewApp()
	defer fa.Quit()

	rows := []store.StudentRow{
		{ID: 1, Name: store.Name{First: "Bob", Last: "Zane"}},
		{ID: 2, Name: store.Name{First: "Carol", Last: "adams"}},
		{ID: 3, Name: store.Name{First: "alice", Last: "Adams"}},
	}
	sel := newStudentSelectWithOptions(rows, false, 0, sortRecent, nil)
	sel.header() // builds the sortable header widgets setSort refreshes
	sel.setSort(sortNameAsc)

	var got []string
	for _, r := range sel.rows {
		got = append(got, r.Name.Display())
	}
	want := []string{"Adams, alice", "adams, Carol", "Zane, Bob"}
	if !slices.Equal(got, want) {
		t.Errorf("ascending order = %v, want %v", got, want)
	}

	sel.setSort(sortNameDesc)
	got = nil
	for _, r := range sel.rows {
		got = append(got, r.Name.Display())
	}
	slices.Reverse(want)
	if !slices.Equal(got, want) {
		t.Errorf("descending order = %v, want %v", got, want)
	}
}

// Selecting students feeds the query by ID, and the summary names them in
// "Last, First" form.
func TestStudentSelectSelectionByID(t *testing.T) {
	fa := test.NewApp()
	defer fa.Quit()

	rows := []store.StudentRow{
		{ID: 7, Name: store.Name{First: "Bob", Last: "Zane"}},
		{ID: 9, Name: store.Name{First: "alice", Last: "Adams"}},
	}
	sel := newStudentSelectWithOptions(rows, false, 0, sortRecent, nil)
	sel.header()
	sel.setSort(sortNameAsc)
	sel.selected[9] = true

	if got := sel.selectedIDs(); !slices.Equal(got, []int64{9}) {
		t.Errorf("selectedIDs() = %v, want [9]", got)
	}
	if got := sel.selectedNames(); !slices.Equal(got, []string{"Adams, alice"}) {
		t.Errorf("selectedNames() = %v, want [\"Adams, alice\"]", got)
	}
}

// The History window's student list opens sorted A-Z by (last, first), not in
// the QR window's recent-first order: history is looked up for a student the
// user already knows by name.
func TestHistoryStudentListOpensSortedByName(t *testing.T) {
	fa := test.NewApp()
	defer fa.Quit()

	// IDs ascend in an order that is not the name order, so recent-first (ID
	// descending) and A-Z produce different lists.
	rows := []store.StudentRow{
		{ID: 1, Name: store.Name{First: "Bob", Last: "Mills"}},
		{ID: 2, Name: store.Name{First: "alice", Last: "Adams"}},
		{ID: 3, Name: store.Name{First: "Carol", Last: "Zane"}},
	}

	h := &history{}
	h.build(rows)

	var got []string
	for _, r := range h.students.rows {
		got = append(got, r.Name.Display())
	}
	want := []string{"Adams, alice", "Mills, Bob", "Zane, Carol"}
	if !slices.Equal(got, want) {
		t.Errorf("student list opened as %v, want %v", got, want)
	}
}

// The QR sheet's picker keeps its recent-first default: codes are printed
// right after a batch of students is added.
func TestQRStudentListOpensRecentFirst(t *testing.T) {
	fa := test.NewApp()
	defer fa.Quit()

	rows := []store.StudentRow{
		{ID: 1, Name: store.Name{First: "alice", Last: "Adams"}},
		{ID: 2, Name: store.Name{First: "Bob", Last: "Mills"}},
		{ID: 3, Name: store.Name{First: "Carol", Last: "Zane"}},
	}

	sel := newStudentSelect(rows)

	var got []string
	for _, r := range sel.rows {
		got = append(got, r.Name.Display())
	}
	want := []string{"Zane, Carol", "Mills, Bob", "Adams, alice"} // ID descending
	if !slices.Equal(got, want) {
		t.Errorf("QR list opened as %v, want %v (most recently added first)", got, want)
	}
}
