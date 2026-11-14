package ui

import (
	"fmt"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/pglass/checkin/internal/backup"
)

// backupCountdown is how long the pre-check waits, once a Refresh has made
// every backup directory available, before starting the backup on its own.
const backupCountdown = 4

// backupPrecheck is the dialog shown before a backup when some backup
// directories are unavailable -- usually an external drive left unplugged. It
// lets the user connect the drive and Refresh, Proceed without it, or Cancel.
// Once a Refresh finds everything available, it counts down and starts the
// backup by itself.
type backupPrecheck struct {
	parent      fyne.Window
	internalDir string
	dirs        []string
	// note, if not empty, opens the message; see backupBar.confirm.
	note string
	// proceed starts the backup; called at most once.
	proceed func()

	dialog  dialog.Dialog
	body    *widget.RichText
	buttons *fyne.Container

	refreshBtn *widget.Button
	cancelBtn  *widget.Button
	proceedBtn *widget.Button

	// unavailable is the result of the latest check; refreshes counts the
	// Refresh presses since this dialog opened.
	unavailable []string
	refreshes   int

	// remaining is the countdown's seconds left, and stopTick cancels its next
	// tick. after schedules a tick; it is time.AfterFunc in the app, and a
	// field so tests can step the countdown without waiting.
	remaining int
	stopTick  func()
	after     func(d time.Duration, f func()) (stop func())
	// done is set once the dialog has proceeded or been cancelled, so a tick
	// already queued does nothing.
	done bool
}

// afterFunc is the real scheduler for the countdown: f runs on the UI thread
// after d.
func afterFunc(d time.Duration, f func()) (stop func()) {
	t := time.AfterFunc(d, func() { fyne.Do(f) })
	return func() { t.Stop() }
}

// checkBackupDirs runs proceed straight away when every backup directory is
// available, and otherwise shows the pre-check dialog over parent, which calls
// proceed only if the user goes ahead (or a Refresh finds everything and the
// countdown runs out).
//
// A non-empty note is shown first in the dialog's message.
func checkBackupDirs(parent fyne.Window, internalDir string, dirs []string, note string, proceed func()) *backupPrecheck {
	unavailable := backup.UnavailableDirs(internalDir, dirs)
	if len(unavailable) == 0 {
		proceed()
		return nil
	}
	p := &backupPrecheck{
		parent: parent, internalDir: internalDir, dirs: dirs, note: note,
		proceed: proceed, unavailable: unavailable, after: afterFunc,
	}
	p.build()
	p.dialog.Show()
	return p
}

// build creates the dialog in its warning state.
func (p *backupPrecheck) build() {
	p.body = widget.NewRichText()
	p.body.Wrapping = fyne.TextWrapWord

	p.refreshBtn = widget.NewButtonWithIcon("Refresh", theme.ViewRefreshIcon(), p.refresh)
	p.cancelBtn = widget.NewButton("Cancel", p.cancel)
	p.proceedBtn = widget.NewButton("Proceed", p.finish)
	p.proceedBtn.Importance = widget.HighImportance
	p.buttons = container.NewHBox()

	p.showWarning()

	content := container.NewBorder(nil,
		container.NewVBox(widget.NewSeparator(), p.buttons), nil, nil,
		container.NewVScroll(p.body))
	p.dialog = dialog.NewCustomWithoutButtons("Backup", content, p.parent)
	p.dialog.Resize(fyne.NewSize(520, 380))
}

// showWarning lists the unavailable directories with Refresh, Cancel and
// Proceed.
func (p *backupPrecheck) showWarning() {
	available := 1 + backup.AvailableDirCount(p.internalDir, p.dirs) // the internal dir is always available
	p.body.Segments = withNote(p.note, precheckWarningSegments(p.unavailable, p.refreshes, available))
	p.body.Refresh()
	p.setButtons(p.refreshBtn, p.cancelBtn, p.proceedBtn)
}

// setButtons shows exactly the given buttons, right-aligned.
func (p *backupPrecheck) setButtons(bs ...fyne.CanvasObject) {
	p.buttons.Objects = append([]fyne.CanvasObject{layout.NewSpacer()}, bs...)
	p.buttons.Refresh()
}

// refresh re-checks every backup directory. With some still unavailable the
// warning is shown again, now with the refresh count so the user can see the
// check ran; with none, the countdown starts.
func (p *backupPrecheck) refresh() {
	p.refreshes++
	p.unavailable = backup.UnavailableDirs(p.internalDir, p.dirs)
	if len(p.unavailable) > 0 {
		p.showWarning()
		return
	}
	p.remaining = backupCountdown
	p.showCountdown()
	p.setButtons(p.cancelBtn)
	p.scheduleTick()
}

// showCountdown renders the all-available message with the seconds left.
func (p *backupPrecheck) showCountdown() {
	p.body.Segments = withNote(p.note, precheckCountdownSegments(p.remaining))
	p.body.Refresh()
}

// scheduleTick arranges the next second of the countdown.
func (p *backupPrecheck) scheduleTick() {
	p.stopTick = p.after(time.Second, p.tick)
}

// tick counts down one second, starting the backup when it reaches zero.
func (p *backupPrecheck) tick() {
	if p.done {
		return
	}
	p.remaining--
	if p.remaining <= 0 {
		p.finish()
		return
	}
	p.showCountdown()
	p.scheduleTick()
}

// finish closes the dialog and starts the backup.
func (p *backupPrecheck) finish() {
	if p.done {
		return
	}
	p.close()
	p.proceed()
}

// cancel closes the dialog without backing up.
func (p *backupPrecheck) cancel() {
	if p.done {
		return
	}
	p.close()
}

// close stops any countdown and hides the dialog.
func (p *backupPrecheck) close() {
	p.done = true
	if p.stopTick != nil {
		p.stopTick()
	}
	p.dialog.Hide()
}

// Text styles for the pre-check message: bold for the lead-in, and small
// monospace for paths.
//
// RichText ends a line only after a segment that is not Inline -- a paragraph
// or list breaks only if its own last segment does -- so each line's final
// segment uses an "End" style, and the inline ones are for everything before it.
var (
	precheckBold     = widget.RichTextStyleStrong
	precheckPlain    = widget.RichTextStyleInline
	precheckPlainEnd = widget.RichTextStyleParagraph
	precheckPathEnd  = widget.RichTextStyle{
		SizeName:  theme.SizeNameCaptionText,
		TextStyle: fyne.TextStyle{Monospace: true},
	}
)

// precheckBlankLine is an empty line between parts of the message.
func precheckBlankLine() widget.RichTextSegment {
	return &widget.TextSegment{Style: precheckPlainEnd}
}

// withNote puts note, on its own line followed by a blank one, ahead of segs.
// An empty note leaves segs as they are.
func withNote(note string, segs []widget.RichTextSegment) []widget.RichTextSegment {
	if note == "" {
		return segs
	}
	return append([]widget.RichTextSegment{
		&widget.TextSegment{Text: note, Style: precheckPlainEnd},
		precheckBlankLine(),
	}, segs...)
}

// precheckWarningSegments is the warning naming the unavailable directories.
// refreshes is shown once the user has pressed Refresh at least once, so a
// press that changed nothing still visibly did something. available counts the
// backup locations the backup will still go to, the internal one included.
func precheckWarningSegments(unavailable []string, refreshes, available int) []widget.RichTextSegment {
	lead := "The following backup location(s) are not available:"
	if refreshes > 0 {
		lead = fmt.Sprintf("The following backup location(s) are not available after %d refresh(es):", refreshes)
	}
	return []widget.RichTextSegment{
		&widget.ParagraphSegment{Texts: []widget.RichTextSegment{
			&widget.TextSegment{Text: "Warning", Style: precheckBold},
			&widget.TextSegment{Text: ": " + lead, Style: precheckPlainEnd},
		}},
		pathList(unavailable, nil),
		precheckBlankLine(),
		&widget.ParagraphSegment{Texts: []widget.RichTextSegment{
			&widget.TextSegment{Text: "If any are located on an external drive, ", Style: precheckPlain},
			&widget.TextSegment{Text: "connect the external drive and press Refresh", Style: precheckBold},
			&widget.TextSegment{Text: ".", Style: precheckPlainEnd},
		}},
		precheckBlankLine(),
		&widget.TextSegment{
			Text: fmt.Sprintf(
				"Press 'Proceed' to backup into %d other available backup location(s). "+
					"The unavailable backup location(s) will be skipped this time, and backfilled "+
					"next time they are available.", available),
			Style: widget.RichTextStyleParagraph,
		},
	}
}

// precheckCountdownSegments is the message shown while counting down to a
// backup once every directory is available.
func precheckCountdownSegments(seconds int) []widget.RichTextSegment {
	unit := "seconds"
	if seconds == 1 {
		unit = "second"
	}
	return []widget.RichTextSegment{
		&widget.ParagraphSegment{Texts: []widget.RichTextSegment{
			&widget.TextSegment{Text: "Refreshed", Style: precheckBold},
			&widget.TextSegment{Text: ": All backup locations are available.", Style: precheckPlainEnd},
		}},
		precheckBlankLine(),
		&widget.TextSegment{
			Text:  fmt.Sprintf("Starting backup in %d %s...", seconds, unit),
			Style: widget.RichTextStyleParagraph,
		},
	}
}

// segmentsText flattens rendered segments to plain text, for tests and logs.
func segmentsText(segs []widget.RichTextSegment) string {
	var out string
	for _, s := range segs {
		switch v := s.(type) {
		case *widget.TextSegment:
			out += v.Text
			if !v.Style.Inline {
				out += "\n"
			}
		case *widget.ParagraphSegment:
			out += segmentsText(v.Texts)
		case *widget.ListSegment:
			for _, it := range v.Items {
				out += "- " + segmentsText([]widget.RichTextSegment{it})
			}
		}
	}
	return out
}
