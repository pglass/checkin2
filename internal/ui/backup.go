package ui

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/pglass/checkin/internal/backup"
	"github.com/pglass/checkin/internal/center"
	"github.com/pglass/checkin/internal/version"
)

// autoBackupDue reports whether the automatic backup should run at startup:
// there is data to back up, and no backup younger than frequency. ok is false
// when there is no backup at all.
//
// hasData is whether any Center has a database yet: on a fresh install there is
// nothing to lose, and a backup of nothing would only add an empty archive.
func autoBackupDue(hasData bool, at time.Time, ok bool, now time.Time, frequency time.Duration) bool {
	if !hasData {
		return false
	}
	return !ok || now.Sub(at) >= frequency
}

// autoBackupNote is shown first in any dialog an automatic backup raises, so
// the user knows why a backup is asking for something they did not start.
func autoBackupNote() string {
	return "Automatic backup wants to begin, but needs your help."
}

// lastBackupText renders the "last backup" line for the startup screen. It is
// separate from the widget so it can be tested without a canvas.
func lastBackupText(at time.Time, ok bool) string {
	if !ok {
		return "Last backup: never"
	}
	return "Last backup: " + at.Format("Jan 2, 2006 3:04 PM")
}

// backupBar is the startup screen's backup row: when the last backup ran, and
// a button to run one now.
type backupBar struct {
	startup *startup
	label   *canvas.Text
	button  *widget.Button
	view    fyne.CanvasObject

	// confirm asks the user before a second backup on a day that already has
	// one, and calls onConfirm if they go ahead. start takes the backup. Both
	// are fields rather than direct calls so tests can drive either answer
	// without a real dialog and without a real backup running into a temp
	// directory that is about to be removed. newBackupBar wires them to the
	// real implementations.
	//
	// note is shown first in any dialog the backup raises; it is empty for a
	// backup the user started, and autoBackupNote for an automatic one.
	confirm func(note string, onConfirm func())
	start   func(note string)
}

// newBackupBar builds the row. Backups always have somewhere to go -- the
// internal backup directory -- so the button is only disabled while one runs.
func newBackupBar(s *startup) *backupBar {
	b := &backupBar{startup: s}

	b.label = canvas.NewText("", theme.Color(theme.ColorNameForeground))
	b.label.TextSize = theme.CaptionTextSize()

	b.button = widget.NewButton("Back Up Now", func() { b.run("") })

	b.confirm = b.confirmSecondBackup
	b.start = b.startBackup

	b.view = container.NewBorder(nil, nil, nil, b.button, container.NewVBox(layout.NewSpacer(), b.label))
	b.refresh()
	return b
}

// refresh re-reads the backup directory and updates the label and button. It is
// called when the row is built and after every backup.
func (b *backupBar) refresh() {
	at, ok := backup.LastBackupTime(b.startup.backupDir())
	b.label.Text = lastBackupText(at, ok)
	b.label.Refresh()
}

// sameDay reports whether a and b fall on the same calendar day in local time.
// Calendar day, not a 24-hour window: a backup at 11pm and another at 8am the
// next morning are two different days to the user, and asking "again today?"
// nine hours later would be wrong.
func sameDay(a, b time.Time) bool {
	ay, am, ad := a.Date()
	by, bm, bd := b.Date()
	return ay == by && am == bm && ad == bd
}

// backedUpToday reports whether the newest archive was taken today, which is
// what makes a second run worth confirming. ok is false when there is no
// archive at all, in which case there is nothing to confirm.
func backedUpToday(at time.Time, ok bool, now time.Time) bool {
	return ok && sameDay(at, now)
}

// run starts a backup and shows the progress window. Backups are taken with no
// Center open, so the button is disabled for the duration rather than allowing
// a second run to race the first.
//
// A backup already taken today is confirmed first: repeat runs are harmless, but
// a stray click should not quietly add a redundant archive.
//
// note is shown first in any dialog the backup raises; see backupBar.confirm.
func (b *backupBar) run(note string) {
	at, ok := backup.LastBackupTime(b.startup.backupDir())
	if backedUpToday(at, ok, time.Now()) {
		b.confirm(note, func() { b.start(note) })
		return
	}
	b.start(note)
}

// autoBackup runs the automatic backup if one is due: there is data, and no
// backup in the internal directory is younger than auto_backup_frequency_days. It
// is the same as pressing Back Up Now, except that any dialog it raises opens
// with autoBackupNote. Called once, when the startup window is shown.
func (b *backupBar) autoBackup() {
	days := b.startup.cfg.AutoBackupFrequencyDays
	at, ok := backup.LastBackupTime(b.startup.backupDir())
	if !autoBackupDue(b.startup.hasCenterData(), at, ok, time.Now(), time.Duration(days)*24*time.Hour) {
		return
	}
	slog.Info("automatic backup due", "frequency_days", days, "last", at, "found", ok)
	b.run(autoBackupNote())
}

// confirmSecondBackup asks before taking a second backup on a day that already
// has one, running onConfirm if the user goes ahead. A non-empty note is shown
// above the question.
func (b *backupBar) confirmSecondBackup(note string, onConfirm func()) {
	var popup dialog.Dialog

	// Hand-rolled buttons rather than dialog.NewConfirm, so the affirmative one
	// is named for what it does ("Back Up Now", the same words as the button
	// that opened it) instead of a bare Yes/No.
	backUpBtn := widget.NewButton("Back Up Now", func() {
		popup.Hide()
		onConfirm()
	})
	backUpBtn.Importance = widget.HighImportance
	cancelBtn := widget.NewButton("Cancel", func() { popup.Hide() })

	msg := widget.NewLabel("A backup was already created today. Make another backup?")
	msg.Wrapping = fyne.TextWrapWord

	body := container.NewVBox(
		msg,
		container.NewCenter(container.NewHBox(cancelBtn, backUpBtn)),
	)
	if note != "" {
		n := widget.NewLabel(note)
		n.Wrapping = fyne.TextWrapWord
		body.Objects = append([]fyne.CanvasObject{n}, body.Objects...)
	}
	popup = dialog.NewCustomWithoutButtons("Backup", body, b.startup.win)
	popup.Show()
}

// startBackup runs the backup, with no further confirmation -- unless a backup
// directory is unavailable, in which case checkBackupDirs warns first (opening
// with note, if any) and the user can connect the drive, go ahead without it,
// or cancel.
func (b *backupBar) startBackup(note string) {
	centers, err := center.List(b.startup.appDir)
	if err != nil {
		b.startup.showError(err)
		return
	}
	checkBackupDirs(b.startup.win, b.startup.backupDir(), b.startup.cfg.BackupDirs, note,
		func() { b.runBackup(centers) })
}

// runBackup takes the backup and shows its progress.
func (b *backupBar) runBackup(centers []center.Center) {
	b.button.Disable()
	showBackupProgress(b.startup.win, centers, b.startup.backupDir(), b.startup.cfg.BackupDirs, func() {
		b.refresh()
		b.button.Enable()
	})
}

// backupProgress is the modal progress window shown while a backup runs.
type backupProgress struct {
	win    fyne.Window
	dialog dialog.Dialog
	bar    *widget.ProgressBar
	status *widget.Label
	// summary replaces status once the run succeeds; see backupSummarySegments.
	summary *widget.RichText
	// done replaces the progress view's buttons once the run finishes, so the
	// window stays up with the outcome instead of vanishing.
	closeBtn *widget.Button
	onClose  func()
}

// minStepDisplay is how long each progress message is held before the next one
// replaces it. Snapshotting a small Center takes a few milliseconds, so without
// this the messages flicker past unread and the window looks like it did
// nothing. Pacing lives here rather than in the backup package because it is a
// property of showing progress to a person, not of taking a backup: an
// unattended backup must never slow itself down for a reader who is not there.
const minStepDisplay = time.Second

// minVerifyStepDisplay is the same for verification, which has more steps and
// shorter ones: a full second each would make checking a backup feel slower
// than taking one.
const minVerifyStepDisplay = 500 * time.Millisecond

// minCopyStepDisplay is the same for copies to the backup directories. A newly
// added directory receives every archive at once, possibly hundreds, so each
// message is held only briefly; the progress bar carries the rest.
const minCopyStepDisplay = 150 * time.Millisecond

// paced wraps a progress callback so each message stays up for at least the
// given minimum before the next is delivered. The returned wait function blocks
// until the final message has had its time, so the caller can hold off on
// replacing it with the summary.
//
// The minimum is read per message via minFor, so a run whose later steps are
// shorter -- a backup's verification tail -- can hold those for less time
// without a second wrapper.
//
// It sleeps on the backup's own goroutine, never the UI thread. The work itself
// is not delayed -- only the delivery of the *next* message is -- so a slow step
// (a large Center) costs nothing extra, and only steps faster than the minimum
// are padded.
func paced(onProgress func(step, total int, desc string), min time.Duration) (wrapped func(step, total int, desc string), wait func()) {
	return pacedBy(onProgress, func(int) time.Duration { return min })
}

// pacedBy is paced with a per-step minimum, chosen from the step number.
func pacedBy(onProgress func(step, total int, desc string), minFor func(step int) time.Duration) (wrapped func(step, total int, desc string), wait func()) {
	var last time.Time
	var lastMin time.Duration
	wrapped = func(step, total int, desc string) {
		if !last.IsZero() {
			if rest := lastMin - time.Since(last); rest > 0 {
				time.Sleep(rest)
			}
		}
		last, lastMin = time.Now(), minFor(step)
		if onProgress != nil {
			onProgress(step, total, desc)
		}
	}
	wait = func() {
		if last.IsZero() {
			return
		}
		if rest := lastMin - time.Since(last); rest > 0 {
			time.Sleep(rest)
		}
	}
	return wrapped, wait
}

// showBackupProgress runs a backup into internalDir in the background, then
// copies it (and any other archive they lack) to each of destDirs, showing a
// progress bar that advances one step per Center, archive, verification check,
// and copy. onClose is called after the user dismisses the window, so the caller
// can refresh.
//
// The backup runs on its own goroutine; every widget update is marshalled back
// onto the Fyne thread with fyne.Do, since Fyne widgets may only be touched
// from the event loop.
func showBackupProgress(parent fyne.Window, centers []center.Center, internalDir string, destDirs []string, onClose func()) {
	p := &backupProgress{win: parent, onClose: onClose}

	p.bar = widget.NewProgressBar()
	p.bar.Min, p.bar.Max = 0, float64(backup.Steps(len(centers)))
	p.bar.SetValue(0)

	p.status = widget.NewLabel("Starting…")
	p.status.Wrapping = fyne.TextWrapWord

	// The outcome replaces the status line once the run succeeds. A RichText,
	// not a Label, for its bulleted lists of monospace paths.
	p.summary = widget.NewRichText()
	p.summary.Wrapping = fyne.TextWrapWord
	p.summary.Hide()

	p.closeBtn = widget.NewButton("Close", func() {
		p.dialog.Hide()
		if p.onClose != nil {
			p.onClose()
		}
	})
	p.closeBtn.Importance = widget.HighImportance
	// Nothing to do until the run finishes: a backup is short and cancelling
	// midway would leave the user unsure whether an archive was written.
	p.closeBtn.Disable()

	body := container.NewVBox(
		p.status,
		p.summary,
		p.bar,
		container.NewHBox(layout.NewSpacer(), p.closeBtn),
	)
	p.dialog = dialog.NewCustomWithoutButtons("Backup", body, parent)
	p.dialog.Resize(fyne.NewSize(380, 180))
	p.dialog.Show()

	go func() {
		// Progress arrives as each step begins, so step-1 steps are complete
		// when step is announced; the bar only reaches full once Run returns.
		// The verification tail holds each message for the shorter verify
		// interval: those steps are numerous and quick, and a full second each
		// would make a backup feel far slower than the work it is doing.
		firstVerifyStep := backup.FirstVerifyStep(len(centers))
		firstCopyStep := backup.Steps(len(centers)) + 1
		onProgress, waitForLastMessage := pacedBy(func(step, total int, desc string) {
			fyne.Do(func() {
				// The total grows once the copies are known; see RunAndCopy.
				p.bar.Max = float64(total)
				p.bar.SetValue(float64(step - 1))
				p.status.SetText(desc + "…")
			})
		}, func(step int) time.Duration {
			switch {
			case step >= firstCopyStep:
				return minCopyStepDisplay
			case step >= firstVerifyStep:
				return minVerifyStepDisplay
			}
			return minStepDisplay
		})

		res, err := backup.RunAndCopy(context.Background(), centers, internalDir, destDirs, version.Resolve(), onProgress)
		// Let the last step's message be read before the summary replaces it.
		waitForLastMessage()
		fyne.Do(func() { p.finish(res, err) })
	}()
}

// finish shows the outcome and re-enables the Close button.
func (p *backupProgress) finish(res backup.Result, err error) {
	if err != nil {
		slog.Error("backup failed", "err", err)
		p.status.SetText("Backup failed: " + err.Error())
		p.bar.SetValue(p.bar.Min)
	} else {
		p.status.Hide()
		p.summary.Segments = backupSummarySegments(res)
		p.summary.Refresh()
		p.summary.Show()
		p.bar.SetValue(p.bar.Max)
		// Wider than the progress view, so the paths in the summary fit.
		p.dialog.Resize(fyne.NewSize(520, p.dialog.MinSize().Height))
	}
	p.closeBtn.Enable()
}

// backupSummarySegments is the message shown when a backup finishes: how many
// Centers were backed up and every location the backup now sits in -- the
// internal directory, then each backup directory reached -- followed by any
// that were skipped because they were unavailable, and any where the copy
// failed for another reason, with that reason.
func backupSummarySegments(res backup.Result) []widget.RichTextSegment {
	n := len(res.Manifest.Centers)
	noun := "centers"
	if n == 1 {
		noun = "center"
	}

	reached := []string{filepath.Dir(res.Path)}
	var skipped, failed []string
	reasons := map[string]string{}
	failedDirs := map[string]bool{}
	for _, f := range res.Copies.Failures {
		if failedDirs[f.DestDir] {
			continue // the first failure is the one reported
		}
		failedDirs[f.DestDir] = true
		if errors.Is(f.Err, backup.ErrDirUnavailable) {
			skipped = append(skipped, f.DestDir)
		} else {
			failed = append(failed, f.DestDir)
			reasons[f.DestDir] = f.Err.Error()
		}
	}
	for _, d := range res.Copies.Dirs {
		if !failedDirs[d] {
			reached = append(reached, d)
		}
	}

	segs := []widget.RichTextSegment{
		&widget.TextSegment{
			Text:  fmt.Sprintf("Backed up %d %s to %d location(s):", n, noun, len(reached)),
			Style: precheckPlainEnd,
		},
		pathList(reached, nil),
	}
	if len(skipped) > 0 {
		segs = append(segs, precheckBlankLine(),
			&widget.TextSegment{
				Text:  fmt.Sprintf("Skipped backup to %d unavailable location(s):", len(skipped)),
				Style: precheckPlainEnd,
			},
			pathList(skipped, nil))
	}
	if len(failed) > 0 {
		segs = append(segs, precheckBlankLine(),
			&widget.TextSegment{
				Text:  fmt.Sprintf("Failed backup to %d location(s):", len(failed)),
				Style: precheckPlainEnd,
			},
			pathList(failed, reasons))
	}
	return segs
}

// pathList lists paths one per line in small monospace, each marked "- " and
// followed by its entry in reasons, if any.
//
// Not a widget.ListSegment: RichText draws its bullet at the normal text size,
// top-aligned with the line, so beside small text the bullet sits visibly high.
// A dash in the path's own style lines up with it.
func pathList(paths []string, reasons map[string]string) widget.RichTextSegment {
	lines := make([]widget.RichTextSegment, 0, len(paths))
	for _, d := range paths {
		if r := reasons[d]; r != "" {
			path := precheckPathEnd
			path.Inline = true
			lines = append(lines,
				&widget.TextSegment{Text: "- " + d, Style: path},
				&widget.TextSegment{Text: ": " + r, Style: precheckPlainEnd})
			continue
		}
		lines = append(lines, &widget.TextSegment{Text: "- " + d, Style: precheckPathEnd})
	}
	return &widget.ParagraphSegment{Texts: lines}
}
