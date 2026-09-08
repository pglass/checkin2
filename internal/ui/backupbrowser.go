package ui

import (
	"fmt"
	"log/slog"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"

	"github.com/pglass/checkin/internal/backup"
)

// archiveListTimeFormat is how an archive's timestamp reads in the list. Wider
// than the startup screen's summary line because this is where backups are told
// apart from one another, so the time of day matters.
const archiveListTimeFormat = "Mon, Jan 2, 2006 at 3:04 PM"

// formatArchiveSize renders an archive's size for the list. Sizes are shown to
// one decimal place from KB up, which is enough to compare backups at a glance
// without implying byte-level precision.
func formatArchiveSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for v := n / unit; v >= unit; v /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}

// archiveLabel is the text shown for one archive: when it was taken, and how
// big it is.
func archiveLabel(a backup.Archive) string {
	return fmt.Sprintf("%s   (%s)", a.CreatedAt.Format(archiveListTimeFormat), formatArchiveSize(a.SizeBytes))
}

// browserHeaderText is the line at the top of the window naming where the
// listed backups came from, so the user can tell which directory they are
// looking at without opening Settings.
func browserHeaderText(backupDir string) string {
	if backupDir == "" {
		return "No backup directory is set. Set backup_dir in Settings to turn backups on."
	}
	return "Displaying backups from " + backupDir
}

// emptyListText explains an empty list, distinguishing "nowhere to look" from
// "looked and found nothing" -- the same distinction the startup screen's
// last-backup line makes.
func emptyListText(backupDir string) string {
	if backupDir == "" {
		return ""
	}
	return "No backups here yet. Use “Back Up Now” on the Center selection window to make one."
}

// backupBrowser is the Browse/Restore window: the archives in the backup
// directory, newest first, each with its own Verify and Restore buttons.
type backupBrowser struct {
	win fyne.Window
	// dir is the backup directory being listed, captured when the window opens.
	dir string
	// archives is what the list renders, newest first.
	archives []backup.Archive
	list     *widget.List
	// empty is shown in place of the list when there are no archives, so the
	// window explains itself rather than showing a blank panel.
	empty *widget.Label
}

// showBackupBrowser opens the Browse/Restore window over the given backup
// directory, calling onClosed when it goes away so the caller can drop its
// reference. The returned window is tracked by the caller to raise the existing
// window instead of opening a second one.
func showBackupBrowser(fa fyne.App, backupDir string, onClosed func()) fyne.Window {
	b := &backupBrowser{dir: backupDir}
	b.win = fa.NewWindow("Backups")
	b.build()
	b.reload()
	b.win.SetOnClosed(onClosed)
	b.win.Resize(fyne.NewSize(620, 420))
	b.win.CenterOnScreen()
	b.win.Show()
	return b.win
}

// build lays out the header, the archive list, and the Close button.
func (b *backupBrowser) build() {
	// A wrapping RichText, not a Label: a wrapping Label reserves height for
	// more lines than it draws, leaving a gap between the path and the
	// separator (settings.go uses RichText for the same reason).
	header := widget.NewRichText(&widget.TextSegment{Text: browserHeaderText(b.dir)})
	header.Wrapping = fyne.TextWrapWord

	b.empty = widget.NewLabel(emptyListText(b.dir))
	b.empty.Wrapping = fyne.TextWrapWord
	b.empty.Hide()

	// Each row is built once and rebound on update, since widget.List recycles
	// row objects across indices: every closure below must read the row's
	// current archive, not the one it was first built with.
	b.list = widget.NewList(
		func() int { return len(b.archives) },
		func() fyne.CanvasObject {
			label := widget.NewLabel("")
			verify := widget.NewButton("Verify", nil)
			restore := widget.NewButton("Restore", nil)
			return container.NewBorder(nil, nil, nil,
				container.NewHBox(verify, restore), label)
		},
		func(i widget.ListItemID, o fyne.CanvasObject) {
			row := o.(*fyne.Container)
			a := b.archives[i]

			label := row.Objects[0].(*widget.Label)
			label.SetText(archiveLabel(a))

			buttons := row.Objects[1].(*fyne.Container)
			verify := buttons.Objects[0].(*widget.Button)
			restore := buttons.Objects[1].(*widget.Button)

			// Rebound every update: widget.List recycles rows, so a closure
			// left over from another index would verify the wrong archive.
			verify.OnTapped = func() { b.verify(a) }
			verify.Enable()

			// Restore is not implemented yet. A button that looks available and
			// does nothing is worse than one that is plainly not ready.
			restore.OnTapped = nil
			restore.Disable()
		},
	)

	closeBtn := widget.NewButton("Close", func() { b.win.Close() })

	// The header is its own Border rather than a VBox entry: a VBox gives every
	// child its minimum height, and a wrapped multi-line path then leaves a gap
	// between the text and the separator.
	b.win.SetContent(withWindowMargin(container.NewBorder(
		container.NewBorder(nil, widget.NewSeparator(), nil, nil, header),
		container.NewVBox(widget.NewSeparator(), container.NewHBox(layout.NewSpacer(), closeBtn)),
		nil, nil,
		container.NewStack(b.list, b.empty),
	)))
}

// verify checks one archive, showing the progress window over this one.
func (b *backupBrowser) verify(a backup.Archive) {
	slog.Info("verifying backup", "path", a.Path)
	showVerifyProgress(b.win, a, nil)
}

// reload re-lists the backup directory and repaints. Listing is by file name
// only, so this is cheap enough to call whenever the window is shown.
func (b *backupBrowser) reload() {
	archives, err := backup.ListArchives(b.dir)
	if err != nil {
		slog.Error("could not list backups", "dir", b.dir, "err", err)
		b.archives = nil
		b.list.Refresh()
		b.showEmpty("Could not read the backup directory: " + err.Error())
		return
	}

	b.archives = archives
	b.list.Refresh()

	// Show the explanatory line instead of the list when there is nothing in it.
	if len(archives) == 0 {
		b.showEmpty(emptyListText(b.dir))
		return
	}
	b.empty.Hide()
	b.list.Show()
}

// showEmpty puts the explanatory message up in place of the list.
func (b *backupBrowser) showEmpty(msg string) {
	b.empty.SetText(msg)
	b.empty.Show()
	b.list.Hide()
}
