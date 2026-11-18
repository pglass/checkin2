package ui

import (
	"reflect"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"

	"github.com/pglass/checkin/internal/spreadsheet"
	"github.com/pglass/checkin/internal/store"
)

// The file picker is resized to follow the window, but FileDialog.Resize
// dereferences an internal window that only exists between Show and close.
// Resizing outside that window panics, so the importer must not hold a picker
// reference before Show or after close.
func TestImporterResizeOnlyWhileBrowserOpen(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()

	w := a.NewWindow("Import Students")
	im := &importer{win: w}

	notifier := newResizeNotifier(im.onWindowResize)
	w.SetContent(container.NewStack(notifier, widget.NewLabel("content")))
	w.Resize(fyne.NewSize(860, 560))
	w.Show()

	// No picker open: a resize must be a no-op rather than a nil deref.
	w.Resize(fyne.NewSize(900, 600))

	d := dialog.NewFileOpen(func(fyne.URIReadCloser, error) {}, w)
	d.SetOnClosed(func() { im.browser = nil })
	d.Show()
	im.browser = d
	d.Resize(w.Canvas().Size())

	// Open: resizes reach the picker.
	w.Resize(fyne.NewSize(1000, 700))
	if im.browser == nil {
		t.Fatal("browser cleared while still open")
	}

	// Closed: the reference is dropped, so later window resizes do no pointless
	// work on a picker the user can no longer see.
	d.Hide()
	if im.browser != nil {
		t.Fatal("browser not cleared on close")
	}
	w.Resize(fyne.NewSize(1100, 800))
}

// The preview lists new students first so the rows that will change something
// are visible without scrolling, and sorts each group by (last, first).
func TestRefreshPreviewOrdersNewFirst(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()

	// Within each group the spreadsheet order is deliberately not the name
	// order, so the assertion below can only pass if the rows are sorted.
	sheet, err := spreadsheet.FromRowsForTest([][]string{
		{"First Name", "Last Name"},
		{"zoe", "Ward"},   // already in the DB
		{"carl", "Young"}, // new
		{"beth", "Adams"}, // already in the DB
		{"adam", "Nash"},  // new
	})
	if err != nil {
		t.Fatalf("FromRowsForTest: %v", err)
	}

	im := &importer{
		win:   a.NewWindow("t"),
		sheet: sheet,
		existing: map[string]bool{
			normalizeName(store.Name{First: "zoe", Last: "Ward"}):   true,
			normalizeName(store.Name{First: "beth", Last: "Adams"}): true,
		},
	}
	im.build()
	im.resetColumnSelectors()

	var got []string
	for _, e := range im.entries {
		got = append(got, e.name.Display())
	}
	// New first (Nash before Young), then existing (Adams before Ward).
	want := []string{"Nash, adam", "Young, carl", "Adams, beth", "Ward, zoe"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("preview order = %q, want %q", got, want)
	}

	// The first two are new, the rest existing.
	for i, e := range im.entries {
		if wantNew := i < 2; e.isNew != wantNew {
			t.Errorf("entry %d (%q): isNew = %v, want %v", i, e.name.Display(), e.isNew, wantNew)
		}
	}
}

// Names with characters a student's name cannot have are left out of the
// preview, so the import cannot stop partway on them, and counted.
func TestRefreshPreviewSkipsInvalidNames(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	sheet, err := spreadsheet.FromRowsForTest([][]string{
		{"First Name", "Last Name"},
		{"Ann", "Lee"},
		{"Mary+Jo", "Smith"},
		{"Bo", "#1"},
	})
	if err != nil {
		t.Fatalf("FromRowsForTest: %v", err)
	}
	im := &importer{win: a.NewWindow("t"), sheet: sheet, existing: map[string]bool{}}
	im.build()
	im.resetColumnSelectors()

	if len(im.entries) != 1 || im.entries[0].name.Display() != "Lee, Ann" {
		t.Errorf("entries = %+v, want only Lee, Ann", im.entries)
	}
	if im.invalidLbl.Hidden || im.invalidLbl.Text != "Skipping 2 names with invalid characters" {
		t.Errorf("invalid label = %q (hidden %v)", im.invalidLbl.Text, im.invalidLbl.Hidden)
	}
}
