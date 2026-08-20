package ui

import (
	"context"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/pglass/checkin/internal/camera"
	"github.com/pglass/checkin/internal/config"
	"github.com/pglass/checkin/internal/qr"
	"github.com/pglass/checkin/internal/store"
)

// newScanApp builds an App backed by a real store, with confirmSc set as the
// confirm_scan setting.
func newScanApp(t *testing.T, confirmScan bool) *App {
	t.Helper()
	fa := test.NewApp()
	t.Cleanup(fa.Quit)

	s, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })

	cfg := config.Default()
	cfg.ConfirmScan = confirmScan

	a := &App{fyneApp: fa, store: s, ctx: context.Background(),
		cfg: cfg, camDevice: deviceNone}
	a.win = fa.NewWindow("main")
	a.table = newStudentTable(a)
	return a
}

// scan feeds a QR payload for name through the scan router.
func scan(t *testing.T, a *App, name string) {
	t.Helper()
	data, err := qr.NewPayload(name).Marshal()
	if err != nil {
		t.Fatal(err)
	}
	a.routeScan(camera.ScanEvent{Payload: string(data)})
}

// With confirmation off, a scan checks the student in immediately, with no
// pop-up, and a second scan checks them out.
func TestScanWithoutConfirmationAppliesImmediately(t *testing.T) {
	a := newScanApp(t, false)
	st, err := a.store.AddStudent(a.ctx, "Alice")
	if err != nil {
		t.Fatal(err)
	}

	scan(t, a, "Alice")
	if got := a.store.Status(st.ID); got != store.StatusIn {
		t.Fatalf("after first scan status = %v, want In", got)
	}
	if a.popupOpen {
		t.Error("a pop-up was shown despite confirm_scan being off")
	}

	scan(t, a, "Alice")
	if got := a.store.Status(st.ID); got != store.StatusOut {
		t.Fatalf("after second scan status = %v, want Out", got)
	}
	if a.popupOpen {
		t.Error("a pop-up was shown on the check-out scan")
	}
}

// With confirmation on, a scan opens the pop-up and changes nothing until the
// user acts.
func TestScanWithConfirmationShowsPopup(t *testing.T) {
	a := newScanApp(t, true)
	st, err := a.store.AddStudent(a.ctx, "Alice")
	if err != nil {
		t.Fatal(err)
	}

	scan(t, a, "Alice")

	if !a.popupOpen {
		t.Error("no pop-up shown despite confirm_scan being on")
	}
	if got := a.store.Status(st.ID); got != store.StatusNotIn {
		t.Fatalf("status = %v, want NotIn until the user confirms", got)
	}
}

// A student who has already checked in and out has no next action. With
// confirmation off this is reported on the feedback bar rather than by a
// pop-up, so an unattended kiosk is never left waiting for a click -- see
// TestScanAlreadyOutUsesFeedbackBarNotPopup. With confirmation on, the dialog
// still opens and explains the state.
func TestScanAlreadyOutWithConfirmationShowsPopup(t *testing.T) {
	a := newScanApp(t, true)
	st, err := a.store.AddStudent(a.ctx, "Alice")
	if err != nil {
		t.Fatal(err)
	}
	if err := a.store.CheckIn(a.ctx, st.ID, st.Name); err != nil {
		t.Fatal(err)
	}
	if err := a.store.CheckOut(a.ctx, st.ID, st.Name); err != nil {
		t.Fatal(err)
	}

	scan(t, a, "Alice")

	if !a.popupOpen {
		t.Error("no pop-up for an already-checked-out student with confirm_scan on")
	}
}

// An unknown code opens the Add Student pop-up whether or not confirmation is
// required: there is no student to check in either way.
func TestScanUnknownStudentAlwaysPrompts(t *testing.T) {
	for _, confirm := range []bool{true, false} {
		a := newScanApp(t, confirm)

		scan(t, a, "Nobody")

		if !a.popupOpen {
			t.Errorf("confirm_scan=%v: no Add Student pop-up for an unknown code", confirm)
		}
	}
}

// The one-pop-up-at-a-time guard still applies to the confirmation path.
func TestScanIgnoredWhilePopupOpen(t *testing.T) {
	a := newScanApp(t, true)
	if _, err := a.store.AddStudent(a.ctx, "Alice"); err != nil {
		t.Fatal(err)
	}

	a.popupOpen = true
	scan(t, a, "Alice")
	// Still exactly the state we set; nothing new was opened or applied.
	if !a.popupOpen {
		t.Error("popupOpen flag cleared by an ignored scan")
	}
}

// The unknown-code notice names the scanned student, and is a normal-size
// wrapping label rather than the small red caption used for validation errors.
func TestScanUnknownStudentNoticeText(t *testing.T) {
	a := newScanApp(t, false)

	scan(t, a, "Bobby Tables")

	want := `Scanned "Bobby Tables" but student is not found in this center. Add them?`
	found := false
	for _, lbl := range dialogLabels(a) {
		{
			if lbl.Text == want {
				found = true
				if lbl.Wrapping != fyne.TextWrapWord {
					t.Errorf("notice Wrapping = %v, want TextWrapWord", lbl.Wrapping)
				}
				if lbl.SizeName != "" && lbl.SizeName != theme.SizeNameText {
					t.Errorf("notice SizeName = %q, want normal text size", lbl.SizeName)
				}
			}
		}
	}
	if !found {
		t.Errorf("no label with the expected notice text %q", want)
	}
}

// The Add Student dialog puts "Name:" and its entry on one row.
func TestAddStudentDialogNameRow(t *testing.T) {
	a := newScanApp(t, false)

	a.showAddStudentDialogPrefill("Alice", "", "")

	var names []string
	for _, lbl := range dialogLabels(a) {
		names = append(names, lbl.Text)
	}
	for _, n := range names {
		if n == "Student name:" {
			t.Error(`label is still "Student name:", want "Name:"`)
		}
	}
	if !slices.Contains(names, "Name:") {
		t.Errorf(`no "Name:" label found; labels = %v`, names)
	}
}

// dialogLabels collects the labels of whatever dialog is currently showing.
// Fyne renders dialogs into the parent window's canvas overlay stack, and the
// overlay is a widget (not a plain container), so walking Objects alone finds
// nothing -- test.LaidOutObjects descends through widget renderers too.
func dialogLabels(a *App) []*widget.Label {
	var out []*widget.Label
	for _, top := range a.win.Canvas().Overlays().List() {
		for _, o := range test.LaidOutObjects(top) {
			if lbl, ok := o.(*widget.Label); ok {
				out = append(out, lbl)
			}
		}
	}
	return out
}

// The Add Student dialog spans most of the window's width. Fyne sizes a custom
// dialog to its content, which leaves it narrow enough that the "not found"
// notice wraps over several lines.
func TestAddStudentDialogIsWide(t *testing.T) {
	a := newScanApp(t, false)
	a.win.Resize(fyne.NewSize(640, 480))

	a.showAddStudentDialogPrefill("Alice",
		`Scanned "Bobby Tables" but student is not found in this center. Add them?`, "")

	canvasW := a.win.Canvas().Size().Width
	// The notice label stretches with the dialog, so its width stands in for
	// the dialog's. Allow for the dialog's own padding and borders.
	want := canvasW * addStudentWidthFraction
	var widest float32
	for _, lbl := range dialogLabels(a) {
		if lbl.Size().Width > widest {
			widest = lbl.Size().Width
		}
	}
	if widest < want*0.85 {
		t.Errorf("widest dialog label = %v, want near %v (%.0f%% of the %v canvas); "+
			"the dialog is not being widened", widest, want, addStudentWidthFraction*100, canvasW)
	}
}

// The Cancel and Add buttons are centred in the dialog. The dialog is far wider
// than the buttons, so a plain HBox would leave them at the left edge.
func TestAddStudentDialogButtonsCentred(t *testing.T) {
	a := newScanApp(t, false)
	a.win.Resize(fyne.NewSize(640, 480))
	a.showAddStudentDialogPrefill("Alice",
		`Scanned "Bobby Tables" but student is not found in this center. Add them?`, "")

	drv := fyne.CurrentApp().Driver()

	// The notice label stretches to the dialog's inner width, so it stands in
	// for the dialog's own bounds -- the canvas is the wrong reference, since
	// the dialog is inset within it.
	var innerX, innerW float32
	for _, lbl := range dialogLabels(a) {
		if lbl.Size().Width > innerW {
			innerW = lbl.Size().Width
			innerX = drv.AbsolutePositionForObject(lbl).X
		}
	}
	if innerW == 0 {
		t.Fatal("could not measure the dialog's inner width")
	}

	var minX, maxX float32
	maxX = -1
	for _, top := range a.win.Canvas().Overlays().List() {
		for _, o := range test.LaidOutObjects(top) {
			btn, ok := o.(*widget.Button)
			if !ok {
				continue
			}
			pos := drv.AbsolutePositionForObject(btn)
			if minX == 0 || pos.X < minX {
				minX = pos.X
			}
			if right := pos.X + btn.Size().Width; right > maxX {
				maxX = right
			}
		}
	}
	if maxX < 0 {
		t.Fatal("no buttons found in the dialog")
	}

	dialogCentre := innerX + innerW/2
	buttonCentre := (minX + maxX) / 2
	if diff := dialogCentre - buttonCentre; diff > 2 || diff < -2 {
		t.Errorf("buttons centred at %.1f, dialog centred at %.1f (off by %.1f)",
			buttonCentre, dialogCentre, diff)
	}
}

// Adding a student from a scanned code clears that code's cooldown, so the
// operator can scan again immediately to check the new student in rather than
// waiting out qr_scan_cooldown.
func TestAddFromScanClearsCooldown(t *testing.T) {
	a := newScanApp(t, false)
	a.cam = camera.New(6, 640, 480, time.Minute)

	payload, err := qr.NewPayload("Alice").Marshal()
	if err != nil {
		t.Fatal(err)
	}

	// Stand in for the scan that opened the dialog: it started the cooldown.
	a.cam.RecordScan(string(payload))
	if !a.cam.InCooldown(string(payload)) {
		t.Fatal("the opening scan should have started a cooldown")
	}

	a.showAddStudentDialogPrefill("Alice",
		`Scanned "Alice" but student is not found in this center. Add them?`,
		string(payload))

	// Confirm the add the way the Add button does.
	dialogConfirm(t, a)

	if _, err := a.store.StudentByName(a.ctx, "Alice"); err != nil {
		t.Fatalf("student was not added: %v", err)
	}
	if a.cam.InCooldown(string(payload)) {
		t.Error("cooldown was not cleared; the new student cannot be scanned in immediately")
	}
}

// Opening the dialog from the menu (no scan payload) must not disturb any
// cooldown.
func TestAddFromMenuLeavesCooldownAlone(t *testing.T) {
	a := newScanApp(t, false)
	a.cam = camera.New(6, 640, 480, time.Minute)

	payload, err := qr.NewPayload("Alice").Marshal()
	if err != nil {
		t.Fatal(err)
	}
	a.cam.RecordScan(string(payload))

	a.showAddStudentDialogPrefill("Alice", "", "")
	dialogConfirm(t, a)

	if !a.cam.InCooldown(string(payload)) {
		t.Error("a menu-opened add cleared a scan cooldown it should not know about")
	}
}

// dialogConfirm taps the "Add" button of the open dialog.
func dialogConfirm(t *testing.T, a *App) {
	t.Helper()
	for _, top := range a.win.Canvas().Overlays().List() {
		for _, o := range test.LaidOutObjects(top) {
			if btn, ok := o.(*widget.Button); ok && btn.Text == "Add" {
				btn.OnTapped()
				return
			}
		}
	}
	t.Fatal("no Add button found in the open dialog")
}
