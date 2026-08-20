package ui

import (
	"errors"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/pglass/checkin/internal/store"
)

// addNameLabelWidth is the width of the "Name:" column in the Add Student
// dialog, wide enough for the label so the entry starts clear of it.
const addNameLabelWidth = 52

// addStudentWidthFraction is how much of the main window's width the Add
// Student dialog spans. Fyne sizes a custom dialog to its content, which leaves
// it narrow and makes the "not found" notice wrap over several lines; widening
// it gives the notice and the name entry room. Dialog.Resize clamps to the
// content's MinSize, so this can only ever widen, never truncate.
const addStudentWidthFraction = 0.85

// showAddStudentDialog presents the Add Student popup. On duplicate name it
// shows a red message and keeps the dialog open (prefill optionally set for QR).
func (a *App) showAddStudentDialog() { a.showAddStudentDialogPrefill("", "") }

func (a *App) showAddStudentDialogPrefill(prefill, notice string) {
	a.popupOpen = true

	entry := widget.NewEntry()
	entry.SetText(prefill)

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
		_, err := a.store.AddStudent(a.ctx, entry.Text)
		if err != nil {
			if errors.Is(err, store.ErrDuplicateName) {
				errLabel.Text = "That name is already taken."
			} else {
				errLabel.Text = err.Error()
			}
			errLabel.Refresh()
			return
		}
		a.refresh()
		popup.Hide()
	}

	addBtn := widget.NewButton("Add", confirm)
	addBtn.Importance = widget.HighImportance
	cancelBtn := widget.NewButton("Cancel", func() { popup.Hide() })

	// Label and entry share a row: the fixed-width label column keeps the entry
	// aligned the same way the Settings window aligns its inputs.
	nameLbl := widget.NewLabel("Name:")
	nameRow := container.NewBorder(nil, nil,
		container.New(fixedWidthLayout{w: addNameLabelWidth}, nameLbl), nil, entry)

	body := container.NewVBox(
		noticeLbl,
		nameRow,
		errLabel,
		// Centred rather than left-aligned: the dialog is much wider than the
		// buttons, so an HBox alone would strand them at the left edge.
		container.NewCenter(container.NewHBox(cancelBtn, addBtn)),
	)
	popup = dialog.NewCustomWithoutButtons("Add Student", body, a.win)
	popup.SetOnClosed(func() { a.popupOpen = false })
	entry.OnSubmitted = func(string) { confirm() }
	popup.Show()
	// After Show: Resize needs the dialog's inner window, which does not exist
	// until then. Height comes from MinSize (Resize takes the max), so only the
	// width is really being set here.
	winSize := a.win.Canvas().Size()
	popup.Resize(fyne.NewSize(winSize.Width*addStudentWidthFraction, 0))
	a.win.Canvas().Focus(entry)
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
	name := canvas.NewText(row.Name, theme.Color(theme.ColorNameForeground))
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
// current status, without any confirmation. Shared by the confirmation dialog's
// buttons and by the no-confirmation scan path, so both apply exactly the same
// state transition.
//
// It reports whether anything was applied: a student who has already checked in
// and out today has no next action, and the caller decides what to do about it
// (the scan path falls back to showing the dialog, which explains the state).
func (a *App) applyCheckInOut(row store.StudentRow) bool {
	var err error
	switch a.store.Status(row.ID) {
	case store.StatusNotIn:
		err = a.store.CheckIn(a.ctx, row.ID, row.Name)
	case store.StatusIn:
		err = a.store.CheckOut(a.ctx, row.ID, row.Name)
	default: // StatusOut: nothing left to do today
		return false
	}
	if err != nil {
		dialog.ShowError(err, a.win)
		return true // handled: the error is on screen, do not fall back
	}
	a.refresh()
	return true
}

// showCheckInOutDialog presents the check-in/out popup (shared by double-click
// and QR scan). The middle button depends on the student's current status.
func (a *App) showCheckInOutDialog(row store.StudentRow) {
	a.popupOpen = true

	name := canvas.NewText(row.Name, theme.Color(theme.ColorNameForeground))
	name.TextSize = theme.TextSize() * 2
	name.Alignment = fyne.TextAlignCenter

	var popup dialog.Dialog

	now := time.Now().Format("3:04 PM")

	var action fyne.CanvasObject
	switch a.store.Status(row.ID) {
	case store.StatusNotIn:
		name.Text = "Check in " + row.Name + " at " + now
		b := widget.NewButton("Check In", func() {
			a.applyCheckInOut(row)
			popup.Hide()
		})
		b.Importance = widget.HighImportance
		action = b
	case store.StatusIn:
		name.Text = "Check out " + row.Name + " at " + now
		b := widget.NewButton("Check Out", func() {
			a.applyCheckInOut(row)
			popup.Hide()
		})
		b.Importance = widget.HighImportance
		action = b
	case store.StatusOut:
		action = widget.NewLabel("This student has checked out. Please close this window.")
	}

	cancel := widget.NewButton("Cancel", func() { popup.Hide() })

	body := container.NewVBox(name, action, widget.NewSeparator(), cancel)
	popup = dialog.NewCustomWithoutButtons("Check In / Out", body, a.win)
	popup.SetOnClosed(func() { a.popupOpen = false })
	popup.Show()
}

// showGenerateQRDialog is implemented in qr_dialog.go (Step 7).
