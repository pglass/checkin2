package ui

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

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

// deactivateCenter hides the named Center in s's app dir and reloads.
func deactivateCenter(t *testing.T, s *startup, name string, at time.Time) {
	t.Helper()
	if _, err := center.Deactivate(s.appDir, name, at); err != nil {
		t.Fatalf("Deactivate %s: %v", name, err)
	}
	s.reload()
}

// A deactivated Center is off the list, and the checkbox appears to offer it.
func TestStartupHidesDeactivatedCenters(t *testing.T) {
	s, _ := newTestStartup(t, "Alpha", "Beta")
	if s.deactCheck.Visible() {
		t.Error("Show Deactivated is visible with nothing deactivated")
	}

	deactivateCenter(t, s, "Alpha", time.Now())

	if len(s.centers) != 1 || s.centers[0].Name != "Beta" {
		t.Errorf("centers = %+v, want only Beta", s.centers)
	}
	if !s.deactCheck.Visible() {
		t.Error("Show Deactivated is hidden with a deactivated Center present")
	}
}

// Ticking the box brings them back, labelled with when they were deactivated.
func TestStartupShowDeactivatedCheckbox(t *testing.T) {
	s, _ := newTestStartup(t, "Alpha", "Beta")
	at := time.Date(2026, 9, 10, 14, 30, 5, 0, time.Local)
	deactivateCenter(t, s, "Alpha", at)

	s.deactCheck.SetChecked(true)

	if len(s.centers) != 2 {
		t.Fatalf("centers = %d, want both shown", len(s.centers))
	}
	var alpha center.Center
	for _, c := range s.centers {
		if c.Name == "Alpha" {
			alpha = c
		}
	}
	if !alpha.Deactivated() {
		t.Fatal("Alpha should be listed as deactivated")
	}
	want := "Alpha (Deactivated at " + at.Format(archiveListTimeFormat) + ")"
	if got := centerRowLabel(alpha); got != want {
		t.Errorf("row label = %q, want %q", got, want)
	}

	// Unticking hides them again.
	s.deactCheck.SetChecked(false)
	if len(s.centers) != 1 || s.centers[0].Name != "Beta" {
		t.Errorf("centers = %+v, want only Beta again", s.centers)
	}
}

// A deactivated Center's row cannot be clicked to open it.
func TestStartupDeactivatedRowIsDisabled(t *testing.T) {
	s, opened := newTestStartup(t, "Alpha")
	deactivateCenter(t, s, "Alpha", time.Now())
	s.deactCheck.SetChecked(true)

	row := centerRowFor(t, s, 0)
	if !row.Disabled() {
		t.Error("deactivated row is enabled, want disabled")
	}
	if row.OnTapped != nil {
		t.Error("deactivated row has a tap action, want none")
	}

	// And the open path itself refuses, independently of the widget.
	s.open(s.centers[0])
	if opened.Name != "" {
		t.Errorf("opened %q, want a deactivated Center not to open", opened.Name)
	}
	if s.errLbl.Text == "" {
		t.Error("no message shown when a deactivated Center was opened")
	}
}

// The context menu offers Deactivate for an active Center and Re-activate for
// a deactivated one, never both.
func TestCenterContextMenuDeactivateToggles(t *testing.T) {
	s, _ := newTestStartup(t, "Alpha")

	labels := menuLabels(s.centerContextMenu(s.centers[0]))
	if !slices.Contains(labels, "Deactivate") {
		t.Errorf("menu = %q, want a Deactivate item", labels)
	}
	if slices.Contains(labels, "Re-activate") {
		t.Errorf("menu = %q, want no Re-activate item for an active Center", labels)
	}

	deactivateCenter(t, s, "Alpha", time.Now())
	s.deactCheck.SetChecked(true)

	labels = menuLabels(s.centerContextMenu(s.centers[0]))
	if !slices.Contains(labels, "Re-activate") {
		t.Errorf("menu = %q, want a Re-activate item", labels)
	}
	if slices.Contains(labels, "Deactivate") {
		t.Errorf("menu = %q, want no Deactivate item for a deactivated Center", labels)
	}
	// Renaming a deactivated Center is not offered; reactivate first.
	if slices.Contains(labels, "Rename") {
		t.Errorf("menu = %q, want no Rename item for a deactivated Center", labels)
	}
}

// menuLabels lists a menu's item labels.
func menuLabels(m *fyne.Menu) []string {
	var out []string
	for _, it := range m.Items {
		out = append(out, it.Label)
	}
	return out
}

// Deactivating renames the directory on disk and drops the Center from the list.
func TestStartupDeactivateHidesAndRenamesDirectory(t *testing.T) {
	s, _ := newTestStartup(t, "Alpha")

	s.deactivate(s.centers[0])

	if len(s.centers) != 0 {
		t.Errorf("centers = %+v, want the list empty", s.centers)
	}
	if _, err := os.Stat(filepath.Join(s.appDir, "Alpha")); !os.IsNotExist(err) {
		t.Error("the plain Alpha directory still exists")
	}
	all, err := center.ListAll(s.appDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 || !all[0].Deactivated() {
		t.Fatalf("ListAll = %+v, want one deactivated Center", all)
	}
	if !strings.Contains(filepath.Base(all[0].Dir), "_deactivated_") {
		t.Errorf("directory = %q, want the deactivated suffix", all[0].Dir)
	}
}

// Reactivating restores the name and puts the Center back on the active list.
func TestStartupReactivateRestoresCenter(t *testing.T) {
	s, _ := newTestStartup(t, "Alpha")
	deactivateCenter(t, s, "Alpha", time.Now())
	s.deactCheck.SetChecked(true)

	s.reactivate(s.centers[0])

	if len(s.centers) != 1 || s.centers[0].Name != "Alpha" || s.centers[0].Deactivated() {
		t.Fatalf("centers = %+v, want an active Alpha", s.centers)
	}
	if _, err := os.Stat(filepath.Join(s.appDir, "Alpha")); err != nil {
		t.Errorf("Alpha directory not restored: %v", err)
	}
	// That was the last deactivated Center, so the checkbox goes away.
	if s.deactCheck.Visible() {
		t.Error("Show Deactivated still visible with none deactivated")
	}
	if s.showDeactivated {
		t.Error("showDeactivated still set after the last one was reactivated")
	}
}

// With the name taken, reactivation is blocked and explained rather than done.
func TestStartupReactivateBlockedWhenNameTaken(t *testing.T) {
	s, _ := newTestStartup(t, "Alpha")
	deactivateCenter(t, s, "Alpha", time.Now())
	// A new Alpha takes the freed name, as restoring a backup would.
	if _, err := center.Create(s.appDir, "Alpha"); err != nil {
		t.Fatalf("recreate: %v", err)
	}
	s.deactCheck.SetChecked(true)

	var deact center.Center
	for _, c := range s.centers {
		if c.Deactivated() {
			deact = c
		}
	}
	if deact.Dir == "" {
		t.Fatal("no deactivated Center in the list")
	}

	if !s.nameTaken(deact) {
		t.Error("nameTaken = false with an active Center of that name")
	}

	s.showReactivateDialog(deact)

	texts := popupLabels(t, s.win)
	want := reactivateBlockedMessage(deact)
	if !slices.Contains(texts, want) {
		t.Errorf("dialog text = %q, want %q", texts, want)
	}
	// One way out and no Confirm: there is nothing to confirm here.
	btns := popupButtons(t, s.win)
	if !slices.Contains(btns, "Back") {
		t.Errorf("buttons = %q, want a Back button", btns)
	}
	if slices.Contains(btns, "Confirm") {
		t.Errorf("buttons = %q, want no Confirm button", btns)
	}

	// Nothing moved.
	if _, err := os.Stat(deact.Dir); err != nil {
		t.Errorf("deactivated directory should be untouched: %v", err)
	}
}

// The confirmation wording for both actions.
func TestDeactivateAndReactivateMessages(t *testing.T) {
	c := center.Center{Name: "Maple St"}
	if got, want := deactivateMessage(c),
		"This center Maple St will be deactivated. The center will be hidden. It can be re-activated later, if needed."; got != want {
		t.Errorf("deactivate message = %q, want %q", got, want)
	}
	if got, want := reactivateMessage(c), "Reactivate the Center Maple St?"; got != want {
		t.Errorf("reactivate message = %q, want %q", got, want)
	}
	if got, want := reactivateBlockedMessage(c),
		"Cannot reactivate this Center because there is already a Center named Maple St. "+
			"To re-activate this center, first rename or deactivate the existing Maple St Center."; got != want {
		t.Errorf("blocked message = %q, want %q", got, want)
	}
}

// The deactivate dialog offers Cancel and Deactivate, and only acts on the
// latter.
func TestDeactivateDialogButtons(t *testing.T) {
	s, _ := newTestStartup(t, "Alpha")

	s.showDeactivateDialog(s.centers[0])

	if texts := popupLabels(t, s.win); !slices.Contains(texts, deactivateMessage(s.centers[0])) {
		t.Errorf("dialog text = %q, want the deactivate message", texts)
	}
	btns := popupButtons(t, s.win)
	for _, want := range []string{"Cancel", "Deactivate"} {
		if !slices.Contains(btns, want) {
			t.Errorf("buttons = %q, want a %q button", btns, want)
		}
	}

	// Cancelling leaves the Center alone.
	tapPopupButton(t, s.win, "Cancel")
	if _, err := os.Stat(filepath.Join(s.appDir, "Alpha")); err != nil {
		t.Errorf("Alpha should be untouched after Cancel: %v", err)
	}
}

// popupLabels returns the label texts in whatever dialog is open.
func popupLabels(t *testing.T, win fyne.Window) []string {
	t.Helper()
	var out []string
	for _, top := range win.Canvas().Overlays().List() {
		for _, o := range test.LaidOutObjects(top) {
			if l, ok := o.(*widget.Label); ok {
				out = append(out, l.Text)
			}
		}
	}
	return out
}

// popupButtons returns the button texts in whatever dialog is open.
func popupButtons(t *testing.T, win fyne.Window) []string {
	t.Helper()
	var out []string
	for _, top := range win.Canvas().Overlays().List() {
		for _, o := range test.LaidOutObjects(top) {
			if b, ok := o.(*widget.Button); ok {
				out = append(out, b.Text)
			}
		}
	}
	return out
}

// tapPopupButton taps the named button in whatever dialog is open.
func tapPopupButton(t *testing.T, win fyne.Window, label string) {
	t.Helper()
	for _, top := range win.Canvas().Overlays().List() {
		for _, o := range test.LaidOutObjects(top) {
			if b, ok := o.(*widget.Button); ok && b.Text == label {
				b.OnTapped()
				return
			}
		}
	}
	t.Fatalf("no %q button in the open dialog", label)
}
