package ui

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
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

// browserHeaderText labels the directory selector at the top of the window.
const browserHeaderText = "Displaying backups from"

// browseOption is the selector's last entry, which opens a folder picker so
// backups can be restored from any directory -- a drive from another machine,
// say -- not only the configured ones.
const browseOption = "Browse…"

// internalSuffix marks the internal backup directory in the selector.
const internalSuffix = " (internal)"

// emptyListText explains an empty list.
func emptyListText() string {
	return "No backups here yet. Use “Back Up Now” on the Center selection window to make one."
}

// unavailableDirText explains a directory that does not exist, which for a
// backup directory usually means its drive is not connected.
func unavailableDirText(dir string) string {
	return "This directory is not available (is the drive connected?): " + dir
}

// browserDirs is the selector's directories: the internal one first, then each
// configured backup directory, with empties and repeats left out.
func browserDirs(internalDir string, backupDirs []string) []string {
	out := []string{internalDir}
	seen := map[string]bool{filepath.Clean(internalDir): true}
	for _, d := range backupDirs {
		if d == "" || seen[filepath.Clean(d)] {
			continue
		}
		seen[filepath.Clean(d)] = true
		out = append(out, d)
	}
	return out
}

// backupBrowser is the Browse/Restore window: the archives in one backup
// directory, newest first, each with its own Verify and Restore buttons. A
// selector at the top switches between the internal directory, the configured
// backup directories, and any other directory picked with Browse….
type backupBrowser struct {
	win fyne.Window
	// dir is the backup directory being listed.
	dir string
	// internalDir is the internal backup directory, marked as such in the
	// selector.
	internalDir string
	// dirs are the selector's directories, in order; a directory picked with
	// Browse… is added to the end.
	dirs []string
	// selector picks dir from dirs, or Browse….
	selector *widget.Select
	// appDir is where a restored Center is created.
	appDir string
	// onRestored is called after a Center is restored, so the Center selection
	// list behind this window picks it up without being reopened.
	onRestored func()
	// listView is the archive list view, kept so the restore view can put it
	// back when the user presses Back.
	listView fyne.CanvasObject
	// archives is what the list renders, newest first.
	archives []backup.Archive
	list     *widget.List
	// empty is shown in place of the list when there are no archives, so the
	// window explains itself rather than showing a blank panel.
	empty *widget.Label
}

// showBackupBrowser opens the Browse/Restore window over the internal backup
// directory, with the configured backup directories also selectable, calling
// onClosed when it goes away so the caller can drop its reference. The returned
// window is tracked by the caller to raise the existing window instead of
// opening a second one.
func showBackupBrowser(fa fyne.App, internalDir string, backupDirs []string, appDir string, onClosed, onRestored func()) fyne.Window {
	b := &backupBrowser{dir: internalDir, internalDir: internalDir,
		dirs: browserDirs(internalDir, backupDirs), appDir: appDir, onRestored: onRestored}
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
	if len(b.dirs) == 0 {
		b.dirs = []string{b.dir}
	}
	b.selector = widget.NewSelect(b.options(), nil)
	b.selector.SetSelected(b.optionFor(b.dir))
	// Attached after the initial selection, so building the window does not
	// count as the user choosing a directory.
	b.selector.OnChanged = b.onSelect
	header := container.NewBorder(nil, nil, widget.NewLabel(browserHeaderText), nil, b.selector)

	b.empty = widget.NewLabel(emptyListText())
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
			restore := widget.NewButton("Explore…", nil)
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

			restore.OnTapped = func() { b.restore(a) }
			restore.Enable()
		},
	)

	closeBtn := widget.NewButton("Close", func() { b.win.Close() })

	b.listView = withWindowMargin(container.NewBorder(
		container.NewBorder(nil, widget.NewSeparator(), nil, nil, header),
		container.NewVBox(widget.NewSeparator(), container.NewHBox(layout.NewSpacer(), closeBtn)),
		nil, nil,
		container.NewStack(b.list, b.empty),
	))
	b.win.SetContent(b.listView)
}

// options are the selector's entries: one per directory, then Browse….
func (b *backupBrowser) options() []string {
	out := make([]string, 0, len(b.dirs)+1)
	for _, d := range b.dirs {
		out = append(out, b.optionFor(d))
	}
	return append(out, browseOption)
}

// optionFor is the selector entry for dir.
func (b *backupBrowser) optionFor(dir string) string {
	if dir == b.internalDir && b.internalDir != "" {
		return dir + internalSuffix
	}
	return dir
}

// onSelect lists the chosen directory, or opens the folder picker for
// Browse….
func (b *backupBrowser) onSelect(opt string) {
	if opt == browseOption {
		b.browseForDir()
		return
	}
	for _, d := range b.dirs {
		if b.optionFor(d) == opt {
			b.setDir(d)
			return
		}
	}
}

// setDir lists dir, unless it is already the one listed.
func (b *backupBrowser) setDir(dir string) {
	if dir == b.dir {
		return
	}
	slog.Info("browsing backups", "dir", dir)
	b.dir = dir
	b.reload()
}

// browseForDir opens a folder picker, starting in the directory being listed.
// A chosen folder is added to the selector (once) and listed. Cancelling puts
// the selector back on the directory still being listed, rather than leaving it
// showing Browse….
func (b *backupBrowser) browseForDir() {
	d := dialog.NewFolderOpen(func(lu fyne.ListableURI, err error) {
		if err != nil {
			dialog.ShowError(err, b.win)
		}
		if err != nil || lu == nil {
			b.selector.SetSelected(b.optionFor(b.dir))
			return
		}
		b.addDir(lu.Path())
	}, b.win)
	if start := listableDir(b.dir); start != nil {
		d.SetLocation(start)
	}
	// Show before Resize: Resize reaches through the internal window that Show
	// creates (see settings.browseDir).
	d.Show()
	d.Resize(b.win.Canvas().Size())
}

// addDir adds dir to the selector, if it is not already there, and selects it,
// which lists it.
func (b *backupBrowser) addDir(dir string) {
	found := false
	for _, d := range b.dirs {
		if filepath.Clean(d) == filepath.Clean(dir) {
			dir, found = d, true
			break
		}
	}
	if !found {
		b.dirs = append(b.dirs, dir)
		b.selector.SetOptions(b.options())
	}
	b.selector.SetSelected(b.optionFor(dir))
}

// showList puts the archive list back, from the restore view's Back button.
// The list is re-read on the way back, so a Center restored in between is
// reflected without reopening the window.
func (b *backupBrowser) showList() {
	b.reload()
	b.win.SetContent(b.listView)
}

// restore switches this window to the restore view for one archive, listing the
// Centers inside it. The manifest is read here rather than in the view so a
// damaged archive is reported before the view is built.
func (b *backupBrowser) restore(a backup.Archive) {
	man, err := backup.ReadManifest(a.Path)
	if err != nil {
		slog.Error("could not read backup manifest", "path", a.Path, "err", err)
		dialog.ShowError(err, b.win)
		return
	}
	slog.Info("browsing backup for restore", "path", a.Path, "centers", len(man.Centers))
	b.win.SetContent(newRestoreView(b, a, man).view)
}

// verify checks one archive, showing the progress window over this one.
func (b *backupBrowser) verify(a backup.Archive) {
	slog.Info("verifying backup", "path", a.Path)
	showVerifyProgress(b.win, a, nil)
}

// reload re-lists the backup directory and repaints. Listing is by file name
// only, so this is cheap enough to call whenever the window is shown.
func (b *backupBrowser) reload() {
	if _, err := os.Stat(b.dir); os.IsNotExist(err) {
		b.archives = nil
		b.list.Refresh()
		b.showEmpty(unavailableDirText(b.dir))
		return
	}
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
		b.showEmpty(emptyListText())
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
