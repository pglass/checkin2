package ui

import (
	"context"
	"fmt"
	"log/slog"
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

// backupInterval is how long a backup may go unrun before the startup screen
// starts asking for one. A week is short enough that a lost machine costs at
// most a few days of check-ins, and long enough that the warning still means
// something when it does appear.
const backupInterval = 7 * 24 * time.Hour

// backupNeeded reports whether the startup screen should be asking for a
// backup, and why. The reason is the parenthesised half of the warning, so the
// caller does not have to know which rule fired.
//
// hasData is whether any Center has a database yet: on a fresh install there is
// nothing to lose, and demanding a backup of nothing would train the user to
// ignore the warning before they ever have data worth keeping. With backups
// switched off there is no warning either -- that is a setting, not a lapse.
func backupNeeded(backupDir string, hasData bool, at time.Time, ok bool, now time.Time) (string, bool) {
	if backupDir == "" || !hasData {
		return "", false
	}
	if !ok {
		return "No backup found", true
	}
	if now.Sub(at) >= backupInterval {
		return "Over 7d since last backup", true
	}
	return "", false
}

// lastBackupText renders the "last backup" line for the startup screen. It is
// separate from the widget so it can be tested without a canvas.
//
// The four cases are distinct on purpose: backups switched off, switched on but
// never run, run at a known time, and overdue. "Never" against an unconfigured
// directory would read as a problem when it is a choice, and the overdue line
// replaces the date entirely rather than appending to it, since a warning read
// as a suffix to good news is a warning that gets skipped.
func lastBackupText(backupDir string, at time.Time, ok bool) string {
	if backupDir == "" {
		return "Backups are off. Set backup_dir in Settings to turn them on."
	}
	if !ok {
		return "Last backup: never"
	}
	return "Last backup: " + at.Format("Jan 2, 2006 3:04 PM")
}

// backupNeededText is the warning shown in place of the "last backup" line
// when a backup is due.
func backupNeededText(reason string) string {
	return "Backup is needed (" + reason + ")"
}

// backupBar is the startup screen's backup row: when the last backup ran, and
// a button to run one now.
type backupBar struct {
	startup *startup
	label   *canvas.Text
	button  *widget.Button
	view    fyne.CanvasObject

	// confirm asks the user before a second backup on a day that already has
	// one, and calls its argument if they go ahead. start takes the backup.
	// Both are fields rather than direct calls so tests can drive either answer
	// without a real dialog and without a real backup running into a temp
	// directory that is about to be removed. newBackupBar wires them to the
	// real implementations.
	confirm func(onConfirm func())
	start   func()
}

// newBackupBar builds the row. The button is disabled when no backup directory
// is configured, so the only way to reach a backup with nowhere to put it is
// blocked in the UI rather than reported as an error afterwards.
func newBackupBar(s *startup) *backupBar {
	b := &backupBar{startup: s}

	b.label = canvas.NewText("", theme.Color(theme.ColorNameForeground))
	b.label.TextSize = theme.CaptionTextSize()

	b.button = widget.NewButton("Back Up Now", b.run)

	b.confirm = b.confirmSecondBackup
	b.start = b.startBackup

	b.view = container.NewBorder(nil, nil, nil, b.button, container.NewVBox(layout.NewSpacer(), b.label))
	b.refresh()
	return b
}

// refresh re-reads the backup directory and updates the label and button. It is
// called when the row is built and after every backup.
func (b *backupBar) refresh() {
	dir := b.startup.cfg.BackupDir
	at, ok := backup.LastBackupTime(dir)

	// A due backup is shown at full size in the error colour, where the
	// reassuring "last backup" line is a caption: the whole point of the
	// warning is that it is not skimmed past like the line it replaces.
	if reason, need := backupNeeded(dir, b.startup.hasCenterData(), at, ok, time.Now()); need {
		b.label.Text = backupNeededText(reason)
		b.label.TextSize = theme.TextSize()
		b.label.Color = theme.Color(theme.ColorNameError)
	} else {
		b.label.Text = lastBackupText(dir, at, ok)
		b.label.TextSize = theme.CaptionTextSize()
		b.label.Color = theme.Color(theme.ColorNameForeground)
	}
	b.label.Refresh()

	if dir == "" {
		b.button.Disable()
	} else {
		b.button.Enable()
	}
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
// A backup already taken today is confirmed first: repeat runs are harmless but
// each one costs a slot in the retention window, so a stray click should not
// quietly push out an older archive.
func (b *backupBar) run() {
	at, ok := backup.LastBackupTime(b.startup.cfg.BackupDir)
	if backedUpToday(at, ok, time.Now()) {
		b.confirm(b.start)
		return
	}
	b.start()
}

// confirmSecondBackup asks before taking a second backup on a day that already
// has one, running onConfirm if the user goes ahead.
func (b *backupBar) confirmSecondBackup(onConfirm func()) {
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
	popup = dialog.NewCustomWithoutButtons("Backup", body, b.startup.win)
	popup.Show()
}

// startBackup runs the backup, with no further confirmation.
func (b *backupBar) startBackup() {
	centers, err := center.List(b.startup.appDir)
	if err != nil {
		b.startup.showError(err)
		return
	}
	b.button.Disable()
	showBackupProgress(b.startup.win, centers, b.startup.cfg.BackupDir, b.startup.cfg.BackupCount, func() {
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

// showBackupProgress runs a backup in the background, showing a progress bar
// that advances one step per Center plus one for the archive. onClose is called
// after the user dismisses the window, so the caller can refresh.
//
// The backup runs on its own goroutine; every widget update is marshalled back
// onto the Fyne thread with fyne.Do, since Fyne widgets may only be touched
// from the event loop.
func showBackupProgress(parent fyne.Window, centers []center.Center, destDir string, keep int, onClose func()) {
	p := &backupProgress{win: parent, onClose: onClose}

	p.bar = widget.NewProgressBar()
	p.bar.Min, p.bar.Max = 0, float64(backup.Steps(len(centers)))
	p.bar.SetValue(0)

	p.status = widget.NewLabel("Starting…")
	p.status.Wrapping = fyne.TextWrapWord

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
		firstVerifyStep := len(centers) + 2
		onProgress, waitForLastMessage := pacedBy(func(step, _ int, desc string) {
			fyne.Do(func() {
				p.bar.SetValue(float64(step - 1))
				p.status.SetText(desc + "…")
			})
		}, func(step int) time.Duration {
			if step >= firstVerifyStep {
				return minVerifyStepDisplay
			}
			return minStepDisplay
		})

		res, err := backup.Run(context.Background(), centers, destDir, version.Resolve(), keep, onProgress)
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
		p.status.SetText(backupSummary(res))
		p.bar.SetValue(p.bar.Max)
	}
	p.closeBtn.Enable()
}

// backupSummary is the message shown when a backup finishes, naming what was
// archived and where it went so the user can go and check.
func backupSummary(res backup.Result) string {
	n := len(res.Manifest.Centers)
	noun := "Centers"
	if n == 1 {
		noun = "Center"
	}
	return fmt.Sprintf("Backed up %d %s to\n%s", n, noun, res.Path)
}
