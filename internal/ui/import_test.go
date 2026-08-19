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

// Resizing a picker that was never shown panics: FileDialog.Resize calls
// MinSize, which dereferences the internal window that only Show creates. This
// is why onBrowse resizes after Show, and why onWindowResize is gated on a
// non-nil browser rather than resizing unconditionally.
func TestResizeBeforeShowPanics(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()

	w := a.NewWindow("host")
	w.Resize(fyne.NewSize(860, 560))
	w.Show()
	d := dialog.NewFileOpen(func(fyne.URIReadCloser, error) {}, w)

	defer func() {
		if recover() == nil {
			t.Fatal("expected panic resizing a file dialog before Show")
		}
	}()
	d.Resize(fyne.NewSize(900, 600))
}

// The preview lists new students first so the rows that will change something
// are visible without scrolling, while each group keeps spreadsheet order.
func TestRefreshPreviewOrdersNewFirst(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()

	sheet, err := spreadsheet.FromRowsForTest([][]string{
		{"First Name", "Last Name"},
		{"zoe", "existing"},  // already in the DB
		{"adam", "new"},      // new
		{"beth", "existing"}, // already in the DB
		{"carl", "new"},      // new
	})
	if err != nil {
		t.Fatalf("FromRowsForTest: %v", err)
	}

	im := &importer{
		win:   a.NewWindow("t"),
		sheet: sheet,
		existing: map[string]bool{
			"zoe existing":  true,
			"beth existing": true,
		},
	}
	im.build()
	im.resetColumnSelectors()

	var got []string
	for _, e := range im.entries {
		got = append(got, e.name)
	}
	want := []string{"adam new", "carl new", "zoe existing", "beth existing"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("preview order = %q, want %q", got, want)
	}

	// The first two are new, the rest existing.
	for i, e := range im.entries {
		if wantNew := i < 2; e.isNew != wantNew {
			t.Errorf("entry %d (%q): isNew = %v, want %v", i, e.name, e.isNew, wantNew)
		}
	}
}
