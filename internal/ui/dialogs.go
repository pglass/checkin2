package ui

import (
	"errors"
	"log/slog"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/pglass/checkin/internal/store"
)

// addNameLabelWidth is the width of the label column in the Add Student
// dialog, wide enough for "Last Name:" so both entries start clear of it.
const addNameLabelWidth = 80

// addStudentWidthFraction is how much of the main window's width the Add
// Student dialog spans. Fyne sizes a custom dialog to its content, which leaves
// it narrow and makes the "not found" notice wrap over several lines; widening
// it gives the notice and the name entry room. Dialog.Resize clamps to the
// content's MinSize, so this can only ever widen, never truncate.
const addStudentWidthFraction = 0.85

// adultLabelWidth is the width of the "Signed in by:" label column in the
// check-in/out dialog, wide enough for the label so the entry clears it.
const adultLabelWidth = 100

// showAddStudentDialog presents the Add Student popup. On duplicate name it
// shows a red message and keeps the dialog open (prefill optionally set for QR).
func (a *App) showAddStudentDialog() { a.showAddStudentDialogPrefill(store.Name{}, "", "") }

// showAddStudentDialogPrefill opens the Add Student dialog with the name box
// prefilled and an explanatory notice above it.
//
// scanPayload is the raw QR payload that triggered the dialog, or "" when it
// was opened from the menu. On a successful add it is cleared from the camera's
// cooldown, so the same code can immediately check the new student in rather
// than being ignored as a repeat scan.
func (a *App) showAddStudentDialogPrefill(prefill store.Name, notice, scanPayload string) {
	a.popupOpen = true

	lastEntry := widget.NewEntry()
	lastEntry.SetText(prefill.Last)
	firstEntry := widget.NewEntry()
	firstEntry.SetText(prefill.First)

	// The notice explains why the dialog opened (a scanned code with no matching
	// student). It is normal size and wraps, unlike the validation error below,
	// which stays a small red caption.
	noticeLbl := widget.NewLabel(notice)
	noticeLbl.Wrapping = fyne.TextWrapWord
	if notice == "" {
		noticeLbl.Hide() // no line taken when opened from the menu
	}

	errLabel := canvas.NewText("", theme.Color(theme.ColorNameError))
	errLabel.TextSize = theme.CaptionTextSize()

	var popup dialog.Dialog
	confirm := func() {
		_, err := a.store.AddStudent(a.ctx, store.NewName(firstEntry.Text, lastEntry.Text))
		if err != nil {
			if errors.Is(err, store.ErrDuplicateName) {
				errLabel.Text = "That name is already taken."
			} else {
				errLabel.Text = err.Error()
			}
			errLabel.Refresh()
			return
		}
		// The scan that opened this dialog already started the code's cooldown.
		// Clear it so the operator can scan again straight away to check the
		// newly added student in.
		if scanPayload != "" && a.cam != nil {
			a.cam.ForgetScan(scanPayload)
		}
		a.refresh()
		popup.Hide()
	}

	addBtn := widget.NewButton("Add", confirm)
	addBtn.Importance = widget.HighImportance
	cancelBtn := widget.NewButton("Cancel", func() { popup.Hide() })

	// Label and entry share a row: the fixed-width label column keeps the entries
	// aligned the same way the Settings window aligns its inputs. Last name
	// first, matching the column order of the main list.
	nameRow := func(label string, e *widget.Entry) fyne.CanvasObject {
		return container.NewBorder(nil, nil,
			container.New(fixedWidthLayout{w: addNameLabelWidth}, widget.NewLabel(label)), nil, e)
	}

	body := container.NewVBox(
		noticeLbl,
		nameRow("Last Name:", lastEntry),
		nameRow("First Name:", firstEntry),
		errLabel,
		// Centred rather than left-aligned: the dialog is much wider than the
		// buttons, so an HBox alone would strand them at the left edge.
		container.NewCenter(container.NewHBox(cancelBtn, addBtn)),
	)
	popup = dialog.NewCustomWithoutButtons("Add Student", body, a.win)
	popup.SetOnClosed(func() { a.popupOpen = false })
	lastEntry.OnSubmitted = func(string) { confirm() }
	firstEntry.OnSubmitted = func(string) { confirm() }
	popup.Show()
	// After Show: Resize needs the dialog's inner window, which does not exist
	// until then. Height comes from MinSize (Resize takes the max), so only the
	// width is really being set here.
	winSize := a.win.Canvas().Size()
	popup.Resize(fyne.NewSize(winSize.Width*addStudentWidthFraction, 0))
	a.win.Canvas().Focus(lastEntry)
}

// showRowContextMenu pops up the right-click context menu for a student row.
// Every item is an advanced action, revealed only while Option/Alt is held
// (macOS convention for hiding uncommon options). With no items to show, no
// menu is displayed at all.
func (a *App) showRowContextMenu(row store.StudentRow, pos fyne.Position, mod fyne.KeyModifier) {
	var items []*fyne.MenuItem
	if mod&fyne.KeyModifierAlt != 0 {
		items = append(items,
			fyne.NewMenuItem("Remove Student", func() { a.showRemoveDialog(row) }),
			fyne.NewMenuItem("Clear Check-in Times", func() {
				if err := a.store.Reset(a.ctx, row.ID); err != nil {
					dialog.ShowError(err, a.win)
					return
				}
				a.refresh()
			}),
		)
	}
	if len(items) == 0 {
		return
	}
	widget.ShowPopUpMenuAtPosition(fyne.NewMenu("", items...), a.win.Canvas(), pos)
}

// showRemoveDialog confirms removal of a student.
func (a *App) showRemoveDialog(row store.StudentRow) {
	name := canvas.NewText(row.Name.Display(), theme.Color(theme.ColorNameForeground))
	name.TextSize = theme.TextSize() * 2
	name.Alignment = fyne.TextAlignCenter

	var popup dialog.Dialog
	removeBtn := widget.NewButton("Remove", func() {
		if err := a.store.RemoveStudent(a.ctx, row.ID, row.Name); err != nil {
			dialog.ShowError(err, a.win)
			return
		}
		a.refresh()
		popup.Hide()
	})
	removeBtn.Importance = widget.DangerImportance
	cancelBtn := widget.NewButton("Cancel", func() { popup.Hide() })

	body := container.NewVBox(
		name,
		widget.NewLabel("Remove this student?"),
		container.NewHBox(cancelBtn, removeBtn),
	)
	popup = dialog.NewCustomWithoutButtons("Remove Student", body, a.win)
	popup.Show()
}

// applyCheckInOut performs the check-in or check-out implied by the student's
// current status. adult is the authorized adult's typed name, stored on the
// resulting Log row.
//
// It reports whether anything was applied: a student who has already checked in
// and out today has no next action, and the caller decides what to do about it
// (the scan path falls back to showing the dialog, which explains the state).
func (a *App) applyCheckInOut(row store.StudentRow, adult string) bool {
	var err error
	// checkedIn records which transition happened, so the feedback bar can be
	// told after the store call succeeds rather than guessing from the status
	// afterwards (which has changed by then).
	var checkedIn bool
	switch a.store.Status(row.ID) {
	case store.StatusNotIn:
		err = a.store.CheckIn(a.ctx, row.ID, row.Name, adult)
		checkedIn = true
	case store.StatusIn:
		err = a.store.CheckOut(a.ctx, row.ID, row.Name, adult)
	default: // StatusOut: nothing left to do today
		return false
	}
	if err != nil {
		dialog.ShowError(err, a.win)
		return true // handled: the error is on screen, do not fall back
	}
	a.refresh()

	// Every check-in/out funnels through here, so the bar reports them all.
	if a.feedback != nil {
		now := time.Now()
		if checkedIn {
			a.feedback.showCheckIn(row.Name.Full(), now)
		} else {
			a.feedback.showCheckOut(row.Name.Full(), now)
		}
	}
	return true
}

// showCheckInOutDialog presents the check-in/out popup (shared by double-click
// and QR scan). The middle button depends on the student's current status.
//
// The authorized adult entry is required: the check-in/out button stays
// disabled until it holds a non-empty (non-whitespace) name, which is then
// stored on the Log row. There is no validation beyond non-empty -- the name is
// a record of who signed, not a credential.
//
// Above the entry sit quick-pick buttons for the adults who recently signed
// this student in or out, since it is usually the same one or two people.
// Tapping one fills the entry (and so enables the action button) but does not
// submit: the confirmation tap stays, so a mis-tap can be corrected before it
// reaches the Log. A student with no such history simply gets no buttons.
func (a *App) showCheckInOutDialog(row store.StudentRow) {
	a.popupOpen = true

	name := canvas.NewText(row.Name.Full(), theme.Color(theme.ColorNameForeground))
	name.TextSize = theme.TextSize() * 2
	name.Alignment = fyne.TextAlignCenter

	var popup dialog.Dialog

	now := time.Now().Format("3:04 PM")

	adultEntry := widget.NewEntry()
	adultEntry.SetPlaceHolder("Parent or authorized adult name")
	adultLbl := widget.NewLabel("Signed in by:")
	adultRow := container.NewBorder(nil, nil,
		container.New(fixedWidthLayout{w: adultLabelWidth}, adultLbl), nil, adultEntry)

	// A failed lookup is not worth interrupting a check-in for: the suggestions
	// are a shortcut, and the entry still works without them.
	adults, err := a.store.RecentAuthorizedAdults(a.ctx, row.ID)
	if err != nil {
		slog.Warn("could not load recent authorized adults", "student", row.Name.Display(), "err", err)
	}
	var suggestions fyne.CanvasObject
	if len(adults) > 0 {
		btns := make([]fyne.CanvasObject, 0, len(adults))
		for _, adult := range adults {
			btns = append(btns, widget.NewButton(adult, func() {
				// SetText fires OnChanged, which enables the action button.
				adultEntry.SetText(adult)
			}))
		}
		// Indented to line up with the entry, under the "Signed in by:" label.
		suggestions = container.NewBorder(nil, nil,
			container.New(fixedWidthLayout{w: adultLabelWidth}, widget.NewLabel("")), nil,
			container.NewHBox(btns...))
	}

	// Set for the two actionable states; nil when the student is already out,
	// where the dialog shows no entry and no button at all.
	var action fyne.CanvasObject
	var actionBtn *widget.Button

	apply := func() {
		if strings.TrimSpace(adultEntry.Text) == "" {
			return // button is disabled in this state; guards Enter-to-submit
		}
		a.applyCheckInOut(row, adultEntry.Text)
		popup.Hide()
	}

	switch a.store.Status(row.ID) {
	case store.StatusNotIn:
		name.Text = "Check in " + row.Name.Full() + " at " + now
		actionBtn = widget.NewButton("Check In", apply)
	case store.StatusIn:
		name.Text = "Check out " + row.Name.Full() + " at " + now
		actionBtn = widget.NewButton("Check Out", apply)
	case store.StatusOut:
		action = widget.NewLabel("This student has checked out. Please close this window.")
	}

	if actionBtn != nil {
		actionBtn.Importance = widget.HighImportance
		actionBtn.Disable() // enabled once a name is typed
		adultEntry.OnChanged = func(s string) {
			if strings.TrimSpace(s) == "" {
				actionBtn.Disable()
			} else {
				actionBtn.Enable()
			}
		}
		adultEntry.OnSubmitted = func(string) { apply() }
		if suggestions != nil {
			action = container.NewVBox(adultRow, suggestions, actionBtn)
		} else {
			action = container.NewVBox(adultRow, actionBtn)
		}
	}

	cancel := widget.NewButton("Cancel", func() { popup.Hide() })

	body := container.NewVBox(name, action, widget.NewSeparator(), cancel)
	popup = dialog.NewCustomWithoutButtons("Check In / Out", body, a.win)
	popup.SetOnClosed(func() { a.popupOpen = false })
	popup.Show()
	if actionBtn != nil {
		// Widened for the same reason as the Add Student dialog: Fyne sizes a
		// custom dialog to its content, which leaves the name entry cramped.
		popup.Resize(fyne.NewSize(a.win.Canvas().Size().Width*addStudentWidthFraction, 0))
		a.win.Canvas().Focus(adultEntry)
	}
}

// showGenerateQRDialog is implemented in qr_dialog.go (Step 7).
