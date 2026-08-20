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

// showAddStudentDialog presents the Add Student popup. On duplicate name it
// shows a red message and keeps the dialog open (prefill optionally set for QR).
func (a *App) showAddStudentDialog() { a.showAddStudentDialogPrefill("", "") }

func (a *App) showAddStudentDialogPrefill(prefill, notice string) {
	a.popupOpen = true

	entry := widget.NewEntry()
	entry.SetText(prefill)

	errLabel := canvas.NewText(notice, theme.Color(theme.ColorNameError))
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

	body := container.NewVBox(
		widget.NewLabel("Student name:"),
		entry,
		errLabel,
		container.NewHBox(cancelBtn, addBtn),
	)
	popup = dialog.NewCustomWithoutButtons("Add Student", body, a.win)
	popup.SetOnClosed(func() { a.popupOpen = false })
	entry.OnSubmitted = func(string) { confirm() }
	popup.Show()
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
			if err := a.store.CheckIn(a.ctx, row.ID, row.Name); err != nil {
				dialog.ShowError(err, a.win)
				return
			}
			a.refresh()
			popup.Hide()
		})
		b.Importance = widget.HighImportance
		action = b
	case store.StatusIn:
		name.Text = "Check out " + row.Name + " at " + now
		b := widget.NewButton("Check Out", func() {
			if err := a.store.CheckOut(a.ctx, row.ID, row.Name); err != nil {
				dialog.ShowError(err, a.win)
				return
			}
			a.refresh()
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
