package ui

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/pglass/checkin/internal/backup"
	"github.com/pglass/checkin/internal/center"
)

// restoreCenterLabel is the text for one Center row in the restore view: what
// it is called and how much is in it. The counts are what let a user tell two
// backups of the same Center apart.
//
// The backup's timestamp is not repeated here: every row in this view comes
// from the one archive named in the header, so per-row it is the same value
// over and over, crowding out the counts that actually differ.
func restoreCenterLabel(ci backup.CenterInfo) string {
	return fmt.Sprintf("%s   %d students, %d log entries",
		ci.Name,
		ci.StudentCount,
		ci.LogCount)
}

// restoreConfirmText is the confirmation shown before a Center is restored. It
// names the Center that will be created, since that name is derived rather than
// chosen and is the one thing the user cannot predict.
func restoreConfirmText(ci backup.CenterInfo, man backup.Manifest) string {
	return fmt.Sprintf("This will add a center named %q with the backup contents.",
		backup.RestoreName(ci.Name, man.CreatedAt))
}

// restoreView is the second view of the Backups window: the Centers inside one
// archive, each with its own Restore Center button.
type restoreView struct {
	browser *backupBrowser
	archive backup.Archive
	man     backup.Manifest
	view    fyne.CanvasObject
}

// newRestoreView builds the restore view for one archive.
func newRestoreView(b *backupBrowser, a backup.Archive, man backup.Manifest) *restoreView {
	r := &restoreView{browser: b, archive: a, man: man}

	header := widget.NewRichText(&widget.TextSegment{
		Text: "Displaying centers in backup from " + man.CreatedAt.Format(archiveListTimeFormat),
	})
	header.Wrapping = fyne.TextWrapWord

	// Restoring never overwrites, so this says what will happen instead --
	// otherwise "Restore" reads as though it will replace the live Center.
	note := widget.NewRichText(&widget.TextSegment{
		Text: "Restoring adds a new Center. Nothing already on this computer is changed or replaced.",
		Style: widget.RichTextStyle{
			ColorName: theme.ColorNamePlaceHolder,
			SizeName:  theme.SizeNameCaptionText,
		},
	})
	note.Wrapping = fyne.TextWrapWord

	// Rows are recycled across indices, so the button's action is rebound on
	// every update rather than captured when the row is created.
	list := widget.NewList(
		func() int { return len(man.Centers) },
		func() fyne.CanvasObject {
			return container.NewBorder(nil, nil, nil,
				widget.NewButton("Restore Center", nil), widget.NewLabel(""))
		},
		func(i widget.ListItemID, o fyne.CanvasObject) {
			row := o.(*fyne.Container)
			ci := man.Centers[i]

			row.Objects[0].(*widget.Label).SetText(restoreCenterLabel(ci))

			btn := row.Objects[1].(*widget.Button)
			btn.OnTapped = func() { r.confirm(ci) }
		},
	)

	backBtn := widget.NewButton("Back", b.showList)

	r.view = withWindowMargin(container.NewBorder(
		container.NewBorder(nil, widget.NewSeparator(), nil, nil,
			container.NewVBox(header, note)),
		container.NewVBox(widget.NewSeparator(), container.NewHBox(backBtn, layout.NewSpacer())),
		nil, nil,
		list,
	))
	return r
}

// confirm asks before restoring, naming the Center that will be created.
func (r *restoreView) confirm(ci backup.CenterInfo) {
	var popup dialog.Dialog

	msg := widget.NewLabel(restoreConfirmText(ci, r.man))
	msg.Wrapping = fyne.TextWrapWord

	confirmBtn := widget.NewButton("Confirm", func() {
		popup.Hide()
		r.doRestore(ci)
	})
	confirmBtn.Importance = widget.HighImportance
	cancelBtn := widget.NewButton("Cancel", func() { popup.Hide() })

	body := container.NewVBox(
		msg,
		container.NewHBox(layout.NewSpacer(), cancelBtn, confirmBtn),
	)
	popup = dialog.NewCustomWithoutButtons("Restore Center", body, r.browser.win)
	// Wide enough that a Center name with a space in it is not broken across
	// lines, which makes the name hard to read back.
	popup.Resize(fyne.NewSize(520, 190))
	popup.Show()
}

// doRestore performs the restore and reports the outcome.
//
// A Center that already exists is reported in its own terms rather than as a
// raw error: restoring the same backup twice is an ordinary thing to try, and
// the reason it is refused -- the Center is already there -- is the useful part.
func (r *restoreView) doRestore(ci backup.CenterInfo) {
	name := backup.RestoreName(ci.Name, r.man.CreatedAt)
	slog.Info("restoring center from backup",
		"archive", r.archive.Path, "center", ci.Name, "as", name)

	c, err := backup.Restore(context.Background(), r.archive.Path, ci.Name, r.browser.appDir)
	if err != nil {
		if errors.Is(err, center.ErrExists) {
			dialog.ShowInformation("Already Restored",
				fmt.Sprintf("A center named %q already exists, so nothing was changed. "+
					"Rename or remove it first if you want to restore this backup again.", name),
				r.browser.win)
			return
		}
		slog.Error("restore failed", "center", ci.Name, "err", err)
		dialog.ShowError(err, r.browser.win)
		return
	}

	slog.Info("center restored", "name", c.Name, "dir", c.Dir)
	if r.browser.onRestored != nil {
		r.browser.onRestored()
	}
	dialog.ShowInformation("Center Restored",
		fmt.Sprintf("Added the center %q. It is now in the list on the Center selection window.", c.Name),
		r.browser.win)
}
