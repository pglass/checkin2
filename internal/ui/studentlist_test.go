package ui

import (
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/test"

	"github.com/pglass/checkin/internal/store"
)

// gridTexts returns the text of every canvas.Text in o, in layout order.
func gridTexts(o fyne.CanvasObject) []string {
	var out []string
	for _, obj := range test.LaidOutObjects(o) {
		if t, ok := obj.(*canvas.Text); ok {
			out = append(out, t.Text)
		}
	}
	return out
}

// The main list's columns are Last Name, First Name, Check In, Check Out --
// last name first, matching the roster sort order.
func TestStudentTableHeaderColumns(t *testing.T) {
	a := newScanApp(t)

	got := gridTexts(a.table.widget())
	want := []string{"Last Name", "First Name", "Check In", "Check Out"}
	if len(got) < len(want) {
		t.Fatalf("header columns = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("header columns = %v, want %v", got[:len(want)], want)
		}
	}
}

// A row puts the last name in the first column and the first name in the
// second, so the list reads in the same order as the header.
func TestStudentRowSplitsNameColumns(t *testing.T) {
	fa := test.NewApp()
	defer fa.Quit()

	r := newStudentRow()
	at := time.Date(2026, 7, 19, 8, 30, 0, 0, time.Local)
	r.update(store.StudentRow{
		Name: store.Name{First: "John", Last: "Smith"},
		In:   &at,
	})

	if r.last.Text != "Smith" {
		t.Errorf("last name cell = %q, want %q", r.last.Text, "Smith")
	}
	if r.first.Text != "John" {
		t.Errorf("first name cell = %q, want %q", r.first.Text, "John")
	}
	if r.in.Text == "" {
		t.Error("check-in cell is empty")
	}
	if r.out.Text != timeFmt(nil) {
		t.Errorf("check-out cell = %q, want the empty-time form %q", r.out.Text, timeFmt(nil))
	}
}
