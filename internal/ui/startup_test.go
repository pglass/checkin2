package ui

import (
	"path/filepath"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"

	"github.com/pglass/checkin/internal/center"
)

// newTestStartup builds a selection window over an app dir holding the given
// Centers, recording which Center onOpen was called with.
func newTestStartup(t *testing.T, names ...string) (*startup, *center.Center) {
	t.Helper()
	fa := test.NewApp()
	t.Cleanup(fa.Quit)

	dir := t.TempDir()
	for _, n := range names {
		if _, err := center.Create(dir, n); err != nil {
			t.Fatalf("Create %s: %v", n, err)
		}
	}

	var opened center.Center
	s := &startup{fyneApp: fa, appDir: dir,
		onOpen: func(c center.Center) error { opened = c; return nil }}
	s.win = fa.NewWindow("startup")
	s.build()
	s.reload()
	return s, &opened
}

// rowButton renders row i of the list the way widget.List does and returns the
// resulting button.
func rowButton(t *testing.T, s *startup, i int) *widget.Button {
	t.Helper()
	obj := widget.NewButton("", nil)
	var o fyne.CanvasObject = obj
	s.list.UpdateItem(i, o)
	return obj
}

// Tapping a Center's row button opens that Center -- no separate Open button.
func TestStartupRowButtonOpensItsCenter(t *testing.T) {
	s, opened := newTestStartup(t, "Alpha", "Beta")

	if len(s.centers) != 2 {
		t.Fatalf("centers = %d, want 2", len(s.centers))
	}

	btn := rowButton(t, s, 1)
	if btn.Text != s.centers[1].Name {
		t.Fatalf("row 1 text = %q, want %q", btn.Text, s.centers[1].Name)
	}
	btn.OnTapped()

	if opened.Name != s.centers[1].Name {
		t.Fatalf("opened %q, want %q", opened.Name, s.centers[1].Name)
	}
	if !s.opened {
		t.Error("opened flag not set after a successful open")
	}
}

// widget.List recycles row objects, so a button reused for a different index
// must open the Center it currently shows, not the one it showed before.
func TestStartupRecycledRowOpensCurrentCenter(t *testing.T) {
	s, opened := newTestStartup(t, "Alpha", "Beta")

	obj := widget.NewButton("", nil)
	var o fyne.CanvasObject = obj

	s.list.UpdateItem(0, o) // row shows Alpha
	s.list.UpdateItem(1, o) // same object reused for Beta

	obj.OnTapped()
	if opened.Name != s.centers[1].Name {
		t.Fatalf("recycled row opened %q, want %q", opened.Name, s.centers[1].Name)
	}
}

// A Center that cannot be opened leaves the window up with the error shown.
func TestStartupOpenFailureKeepsWindow(t *testing.T) {
	fa := test.NewApp()
	defer fa.Quit()

	dir := t.TempDir()
	c, err := center.Create(dir, "Alpha")
	if err != nil {
		t.Fatal(err)
	}

	s := &startup{fyneApp: fa, appDir: dir,
		onOpen: func(center.Center) error { return errAlreadyOpenStub{} }}
	s.win = fa.NewWindow("startup")
	s.build()
	s.reload()

	s.open(c)

	if s.opened {
		t.Error("opened flag set despite the open failing")
	}
	if s.errLbl.Text == "" {
		t.Error("no error shown after a failed open")
	}
}

type errAlreadyOpenStub struct{}

func (errAlreadyOpenStub) Error() string { return "already open" }

// Centers created after the window is built appear in the list.
func TestStartupReloadPicksUpNewCenter(t *testing.T) {
	s, _ := newTestStartup(t, "Alpha")

	if _, err := center.Create(s.appDir, "Beta"); err != nil {
		t.Fatal(err)
	}
	s.reload()

	if len(s.centers) != 2 {
		t.Fatalf("centers = %d, want 2 after reload", len(s.centers))
	}
	if _, err := filepath.Abs(s.centers[1].Dir); err != nil {
		t.Fatal(err)
	}
}
