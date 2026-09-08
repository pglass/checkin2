package ui

import (
	"context"
	"log/slog"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"

	"github.com/pglass/checkin/internal/backup"
)

// verifyProgress is the modal window shown while an archive is verified.
type verifyProgress struct {
	dialog dialog.Dialog
	bar    *widget.ProgressBar
	status *widget.Label
	// statusScroll holds status, so a long list of problems scrolls instead of
	// pushing the progress bar and Close button out of the window.
	statusScroll *container.Scroll
	closeBtn     *widget.Button
	onClose      func()
}

// verifyStatusHeight is the height reserved for the status message: enough for
// a few lines of failures without the window resizing as the text changes.
const verifyStatusHeight = 110

// showVerifyProgress verifies archivePath in the background, showing a progress
// bar that advances one step per check. onClose is called once the user
// dismisses the window.
//
// The verification runs on its own goroutine; every widget update is marshalled
// back onto the Fyne thread with fyne.Do, since Fyne widgets may only be
// touched from the event loop.
func showVerifyProgress(parent fyne.Window, a backup.Archive, onClose func()) {
	p := &verifyProgress{onClose: onClose}

	p.bar = widget.NewProgressBar()
	// The real total arrives with the first progress call, once the archive's
	// manifest says how many Centers it holds. Until then the bar is a
	// placeholder rather than a wrong number.
	p.bar.Min, p.bar.Max = 0, 1
	p.bar.SetValue(0)

	p.status = widget.NewLabel("Starting…")
	p.status.Wrapping = fyne.TextWrapWord
	// A failed verification lists one line per problem, which can outgrow the
	// window. The message scrolls inside a fixed region so it can never push
	// the bar and the Close button off the bottom, or overlap them.
	p.statusScroll = container.NewVScroll(p.status)
	p.statusScroll.SetMinSize(fyne.NewSize(0, verifyStatusHeight))

	p.closeBtn = widget.NewButton("Close", func() {
		p.dialog.Hide()
		if p.onClose != nil {
			p.onClose()
		}
	})
	p.closeBtn.Importance = widget.HighImportance
	p.closeBtn.Disable()

	// Border, not VBox: the scrolling message takes the space left over after
	// the bar and buttons have theirs, rather than every child claiming its own
	// minimum and the total overflowing the window.
	body := container.NewBorder(
		nil,
		container.NewVBox(p.bar, container.NewHBox(layout.NewSpacer(), p.closeBtn)),
		nil, nil,
		p.statusScroll,
	)
	p.dialog = dialog.NewCustomWithoutButtons("Verify Backup", body, parent)
	p.dialog.Resize(fyne.NewSize(520, 260))
	p.dialog.Show()

	go func() {
		// Progress arrives as each step begins, so step-1 steps are complete
		// when step is announced; the bar only fills once Verify returns.
		onProgress, waitForLastMessage := paced(func(step, total int, desc string) {
			fyne.Do(func() {
				p.bar.Max = float64(total)
				p.bar.SetValue(float64(step - 1))
				p.status.SetText(desc + "…")
			})
		}, minVerifyStepDisplay)

		res, err := backup.Verify(context.Background(), a.Path, onProgress)
		waitForLastMessage()
		fyne.Do(func() { p.finish(res, err) })
	}()
}

// finish shows the outcome and re-enables the Close button.
//
// A failed verification is shown here in full rather than as a one-line error:
// the user's next decision is whether this backup can be relied on, and that
// needs to name what is wrong with it.
func (p *verifyProgress) finish(res backup.VerifyResult, err error) {
	switch {
	case err != nil:
		slog.Error("backup verification could not run", "path", res.Path, "err", err)
		p.status.SetText("Could not verify this backup: " + err.Error())
	case !res.OK():
		slog.Warn("backup failed verification", "path", res.Path,
			"failures", len(res.Failures()))
		p.status.SetText(backup.VerifySummary(res))
	default:
		slog.Info("backup verified", "path", res.Path, "checks", len(res.Results))
		p.status.SetText(backup.VerifySummary(res))
	}
	// The bar tracks work still running, and there is none now. Leaving a full
	// bar up would read as a result in itself -- and read as success even when
	// the checks failed, which is exactly backwards.
	p.bar.Hide()
	p.closeBtn.Enable()
}
