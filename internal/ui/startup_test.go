package ui

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
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
		onOpen: func(c center.Center, _ fyne.Window) error { opened = c; return nil }}
	s.win = fa.NewWindow("startup")
	s.build()
	s.reload()
	return s, &opened
}

// rowButton renders row i of the list the way widget.List does and returns the
// resulting row. Rows are centerRows (a Button that also handles right-click),
// so the button behaviour under test is reached through the embedded Button.
func rowButton(t *testing.T, s *startup, i int) *widget.Button {
	t.Helper()
	return &centerRowFor(t, s, i).Button
}

// centerRowFor renders row i and returns the whole row, for tests that need the
// right-click hook as well as the button.
func centerRowFor(t *testing.T, s *startup, i int) *centerRow {
	t.Helper()
	row := newCenterRow()
	var o fyne.CanvasObject = row
	s.list.UpdateItem(i, o)
	return row
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

	row := newCenterRow()
	var o fyne.CanvasObject = row

	s.list.UpdateItem(0, o) // row shows Alpha
	s.list.UpdateItem(1, o) // same object reused for Beta

	row.OnTapped()
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
		onOpen: func(center.Center, fyne.Window) error { return errAlreadyOpenStub{} }}
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

// The selection window becomes the main window in place: opening a Center must
// hand the existing window to onOpen and leave it open. Closing a window from
// inside the click that asked for it frees the GLFW handle while fyne's
// processMouseClicked is still using it, and the driver panics on the nil view
// -- reusing the window removes that path entirely.
func TestStartupOpenReusesWindowWithoutClosing(t *testing.T) {
	fa := test.NewApp()
	defer fa.Quit()

	dir := t.TempDir()
	if _, err := center.Create(dir, "Alpha"); err != nil {
		t.Fatal(err)
	}

	var gotWin fyne.Window
	s := &startup{fyneApp: fa, appDir: dir,
		onOpen: func(_ center.Center, win fyne.Window) error { gotWin = win; return nil }}
	s.win = fa.NewWindow("startup")
	s.build()
	s.reload()

	closed := false
	s.win.SetOnClosed(func() { closed = true })

	rowButton(t, s, 0).OnTapped()

	if gotWin != s.win {
		t.Error("onOpen was not handed the selection window to take over")
	}
	if closed {
		t.Error("selection window was closed; it must be reused as the main window")
	}
	if !s.opened {
		t.Error("opened flag not set after a successful open")
	}
}

// A failed open leaves the window showing the selection view with the error,
// and does not hand the window over.
func TestStartupFailedOpenKeepsSelectionView(t *testing.T) {
	fa := test.NewApp()
	defer fa.Quit()

	dir := t.TempDir()
	c, err := center.Create(dir, "Alpha")
	if err != nil {
		t.Fatal(err)
	}

	handedOver := false
	s := &startup{fyneApp: fa, appDir: dir,
		onOpen: func(center.Center, fyne.Window) error {
			handedOver = true
			return errAlreadyOpenStub{}
		}}
	s.win = fa.NewWindow("startup")
	s.build()
	s.reload()

	s.open(c)

	if !handedOver {
		t.Error("onOpen should still be called on the failing path")
	}
	if s.opened {
		t.Error("opened flag set despite the open failing")
	}
	if s.errLbl.Text == "" {
		t.Error("no error shown after a failed open")
	}
}

// renameProblem is the single check behind both the live validation and the
// confirm, so its behaviour is pinned directly.
func TestRenameProblem(t *testing.T) {
	s, _ := newTestStartup(t, "Alpha", "Beta")
	alpha := s.centers[0]
	if alpha.Name != "Alpha" {
		t.Fatalf("centers[0] = %q, want Alpha", alpha.Name)
	}

	cases := []struct {
		name, want string
	}{
		{"Gamma", ""},
		// Its own name is not a clash: confirming an unedited dialog is a no-op.
		{"Alpha", ""},
		// Nor is its own name in another case -- that is a real rename.
		{"alpha", ""},
		{"Beta", "Center already exists"},
		// Another Center's name in another case is still that Center on a
		// case-insensitive filesystem.
		{"beta", "Center already exists"},
		{"", "Enter a name"},
		{"   ", "Enter a name"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := s.renameProblem(alpha, tc.name); got != tc.want {
				t.Errorf("renameProblem(%q) = %q, want %q", tc.name, got, tc.want)
			}
		})
	}

	// An invalid name is reported, with whatever ValidateName says.
	if got := s.renameProblem(alpha, "a/b"); got == "" {
		t.Error("renameProblem on an invalid name = \"\", want a problem")
	}
}

// Right-clicking a Center row offers Rename.
func TestCenterRowContextMenuHasRename(t *testing.T) {
	s, _ := newTestStartup(t, "Alpha")

	row := centerRowFor(t, s, 0)
	if row.onSecondary == nil {
		t.Fatal("row has no right-click handler")
	}

	var items []string
	var rename *fyne.MenuItem
	for _, it := range s.centerContextMenu(s.centers[0]).Items {
		items = append(items, it.Label)
		if it.Label == "Rename" {
			rename = it
		}
	}
	if rename == nil {
		t.Fatalf("context menu items = %q, want a Rename item", items)
	}

	// The item opens the Rename dialog for the Center that was right-clicked.
	rename.Action()
	entry, _, _, texts := renameDialogParts(t, s)
	if entry.Text != "Alpha" {
		t.Errorf("dialog entry = %q, want Alpha", entry.Text)
	}
	if !slices.Contains(texts, "Rename center Alpha") {
		t.Errorf("dialog labels = %q, want the Alpha heading", texts)
	}

	// Right-clicking itself must not panic and must put the menu on screen.
	row.onSecondary(fyne.NewPos(0, 0))
}

// The dialog names the Center, prefills its name, and renames it on Confirm.
func TestRenameDialogRenamesCenter(t *testing.T) {
	s, _ := newTestStartup(t, "Alpha")
	alpha := s.centers[0]

	s.showRenameDialog(alpha)

	entry, confirm, _, texts := renameDialogParts(t, s)
	if entry.Text != "Alpha" {
		t.Errorf("entry = %q, want the current name prefilled", entry.Text)
	}
	if !slices.Contains(texts, "Rename center Alpha") {
		t.Errorf("dialog labels = %q, want %q", texts, "Rename center Alpha")
	}
	if confirm.Disabled() {
		t.Error("Confirm starts disabled on an unedited valid name")
	}

	entry.SetText("Gamma")
	confirm.OnTapped()

	if len(s.centers) != 1 || s.centers[0].Name != "Gamma" {
		t.Fatalf("centers after rename = %v, want just Gamma", s.centers)
	}
	if _, err := os.Stat(filepath.Join(s.appDir, "Gamma")); err != nil {
		t.Errorf("Gamma directory not on disk: %v", err)
	}
}

// Typing another Center's name shows the message and disables Confirm until it
// is fixed.
func TestRenameDialogRejectsExistingName(t *testing.T) {
	s, _ := newTestStartup(t, "Alpha", "Beta")

	s.showRenameDialog(s.centers[0])
	entry, confirm, errText, _ := renameDialogParts(t, s)

	entry.SetText("Beta")
	if !confirm.Disabled() {
		t.Error("Confirm is enabled for a name that already exists")
	}
	if errText.Text != "Center already exists" {
		t.Errorf("message = %q, want %q", errText.Text, "Center already exists")
	}

	// Fixing the name clears the message and re-enables Confirm.
	entry.SetText("Gamma")
	if confirm.Disabled() {
		t.Error("Confirm still disabled after the name was fixed")
	}
	if errText.Text != "" {
		t.Errorf("message = %q, want it cleared", errText.Text)
	}
}

// renameDialogParts digs the entry, Confirm button, red message and labels out
// of the open Rename dialog.
func renameDialogParts(t *testing.T, s *startup) (*widget.Entry, *widget.Button, *canvas.Text, []string) {
	t.Helper()

	var entry *widget.Entry
	var confirm *widget.Button
	var errText *canvas.Text
	var texts []string

	for _, top := range s.win.Canvas().Overlays().List() {
		for _, o := range test.LaidOutObjects(top) {
			switch v := o.(type) {
			case *widget.Entry:
				entry = v
			case *widget.Button:
				if v.Text == "Confirm" {
					confirm = v
				}
			case *widget.Label:
				texts = append(texts, v.Text)
			case *canvas.Text:
				// The red validation caption, not the dialog's own chrome.
				if v.TextSize == theme.CaptionTextSize() {
					errText = v
				}
			}
		}
	}
	if entry == nil || confirm == nil || errText == nil {
		t.Fatalf("rename dialog parts missing: entry=%v confirm=%v err=%v", entry != nil, confirm != nil, errText != nil)
	}
	return entry, confirm, errText, texts
}
