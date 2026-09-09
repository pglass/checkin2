package ui

import (
	"errors"
	"log/slog"
	"os"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/pglass/checkin/internal/center"
	"github.com/pglass/checkin/internal/config"
)

// startup is the Center selection window shown before the main window.
type startup struct {
	fyneApp fyne.App
	win     fyne.Window
	appDir  string

	// cfg is the loaded settings, replaced when the Settings window saves.
	// The selection window needs them for the backup row, which runs before any
	// Center is open. cfgPath is the settings.ini it is written back to.
	cfg     config.Config
	cfgPath string

	// settingsWin is the Settings window; tracked so reopening raises the
	// existing one instead of spawning a duplicate, as the main window does.
	settingsWin fyne.Window

	// browserWin is the Browse/Restore window, tracked like settingsWin.
	browserWin fyne.Window

	// about supplies the About window, so version and license information is
	// reachable before any Center is open.
	about about

	centers []center.Center
	list    *widget.List
	errLbl  *canvas.Text
	backups *backupBar

	// onOpen receives the chosen Center and this window, and replaces the
	// window's content with the main view. It returns an error (e.g.
	// store.ErrAlreadyOpen) when the Center cannot be opened, in which case the
	// selection view stays up and shows the message.
	onOpen func(center.Center, fyne.Window) error

	// opened records that a Center was chosen, so the window's close handler is
	// not the selection window's "user quit without choosing" one any more.
	opened bool
}

// ShowStartup displays the Center selection window. onOpen is called with the
// chosen Center and this window, and is expected to take the window over,
// replacing its content with the main view; on failure (e.g. the Center is
// already open) the selection view stays up and shows the message. If the user
// closes the window without choosing, onOpen is never called and the process
// should exit. The caller drives the Fyne event loop.
//
// Handing the window over rather than opening a second one and closing this one
// is deliberate: destroying a window from inside the click that asked for it
// frees the GLFW handle while fyne's processMouseClicked is still using it, and
// the driver then panics on a nil view. With one window for the lifetime of the
// process, that class of crash cannot occur here.
func ShowStartup(fa fyne.App, appDir string, cfg config.Config, cfgPath string, onOpen func(center.Center, fyne.Window) error) {
	s := &startup{fyneApp: fa, appDir: appDir, cfg: cfg, cfgPath: cfgPath, onOpen: onOpen}
	s.win = fa.NewWindow("Check-In")
	s.about = about{fyneApp: fa, parent: s.win}
	// Settings belongs here as well as in the main window: backup_dir is set
	// from Settings and used from this screen, so a user told to "set backup_dir
	// in Settings" must be able to get there without opening a Center first.
	// Backups get their own menu rather than sitting under File: they are the
	// one thing this window does that is not about choosing a Center, and the
	// menu is where they stay reachable once Verify and Restore are added.
	s.win.SetMainMenu(fyne.NewMainMenu(
		fyne.NewMenu("File",
			fyne.NewMenuItem("Settings…", s.showSettingsWindow),
			s.about.menuItem(),
		),
		fyne.NewMenu("Backups",
			fyne.NewMenuItem("Browse/Restore…", s.showBackupBrowser),
		),
	))
	s.build()
	s.reload()
	s.win.Resize(fyne.NewSize(400, 380))
	s.win.CenterOnScreen()
	s.win.Show()

	// Nothing to select on a fresh install: go straight to creating a Center.
	// Cancelling leaves the empty window, where "Add Center…" is still there.
	if len(s.centers) == 0 {
		s.showAddDialog()
	}
}

func (s *startup) build() {
	s.errLbl = canvas.NewText("", theme.Color(theme.ColorNameError))
	s.errLbl.TextSize = theme.CaptionTextSize()

	// Each row is a button that opens its own Center: one click instead of
	// select-then-Open. The row's OnTapped is rebound on every update, since
	// widget.List recycles row objects across indices.
	s.list = widget.NewList(
		func() int { return len(s.centers) },
		func() fyne.CanvasObject { return newCenterRow() },
		func(i widget.ListItemID, o fyne.CanvasObject) {
			row := o.(*centerRow)
			c := s.centers[i]
			row.SetText(c.Name)
			row.OnTapped = func() { s.open(c) }
			row.onSecondary = func(pos fyne.Position) { s.showCenterContextMenu(c, pos) }
		},
	)

	addBtn := widget.NewButton("Add Center…", s.showAddDialog)

	// Backups cover every Center at once and need none of them open, so they
	// belong here rather than inside a Center's own window.
	s.backups = newBackupBar(s)

	header := widget.NewLabel("Select a center to open:")
	buttons := container.NewHBox(addBtn, layout.NewSpacer())
	bottom := container.NewVBox(
		s.errLbl,
		buttons,
		widget.NewSeparator(),
		s.backups.view,
	)

	s.win.SetContent(withWindowMargin(
		container.NewBorder(header, bottom, nil, nil, s.list),
	))
	// No Center chosen: nothing else will run, so quit the app. Once a Center is
	// opened, NewAppInWindow replaces this handler with the main window's.
	s.win.SetOnClosed(func() {
		if !s.opened {
			s.fyneApp.Quit()
		}
	})
}

// settingsConfig, settingsPath, settingsApp, and applySettings implement
// settingsHost for the Center selection window.
func (s *startup) settingsConfig() config.Config { return s.cfg }
func (s *startup) settingsPath() string          { return s.cfgPath }
func (s *startup) settingsApp() fyne.App         { return s.fyneApp }

// settingsRestartNote is empty: nothing saved from this screen needs a restart.
// No Center is open, so no setting has been read into a running component yet
// -- the camera and the store read theirs when a Center is opened, which cannot
// have happened before this window closes -- and the backup settings are re-read
// on every run. There is nothing to warn about, so nothing is shown.
func (s *startup) settingsRestartNote() string { return "" }

// applySettings stores the saved settings and refreshes the backup row. Unlike
// the main window, this screen can honour a change immediately: the backup
// directory is read each time a backup runs, so a newly set backup_dir enables
// the button straight away rather than at the next startup.
func (s *startup) applySettings(cfg config.Config) {
	s.cfg = cfg
	s.backups.refresh()
}

// showSettingsWindow opens Settings, raising the existing window if it is
// already up.
func (s *startup) showSettingsWindow() {
	if s.settingsWin != nil {
		s.settingsWin.RequestFocus()
		return
	}
	s.settingsWin = showSettingsFor(s, func() { s.settingsWin = nil })
}

// showBackupBrowser opens the Browse/Restore window, raising the existing one
// if it is already up. The current backup directory is passed in, so a
// directory just changed in Settings is the one browsed.
func (s *startup) showBackupBrowser() {
	if s.browserWin != nil {
		s.browserWin.RequestFocus()
		return
	}
	s.browserWin = showBackupBrowser(s.fyneApp, s.cfg.BackupDir, s.appDir,
		func() { s.browserWin = nil },
		// A restored Center is a new directory in appDir, so the selection list
		// behind this window is stale until it is re-read.
		func() { fyne.Do(s.reload) })
}

// hasCenterData reports whether any Center has a database yet. A Center is a
// directory, created before it is ever opened, so its mere existence does not
// mean there is anything to back up; the database file is what says data
// exists. Used by the backup row to decide whether an overdue backup is worth
// warning about.
func (s *startup) hasCenterData() bool {
	for _, c := range s.centers {
		if _, err := os.Stat(c.DBPath()); err == nil {
			return true
		}
	}
	return false
}

// reload re-lists the Centers on disk and repaints the list.
func (s *startup) reload() {
	centers, err := center.List(s.appDir)
	if err != nil {
		s.showError(err)
		return
	}
	s.centers = centers
	s.list.Refresh()
	// The backup row's warning depends on which Centers have databases, so it
	// is re-evaluated with the list rather than only when a backup is taken.
	// build() runs before the first reload, so the row may not exist yet.
	if s.backups != nil {
		s.backups.refresh()
	}
}

// open opens c, keeping the selection view up with a message if it cannot be
// opened (e.g. it is already open in another instance).
//
// On success onOpen has replaced this window's content with the main view, so
// there is nothing to close here: the window continues as the main window. That
// is what keeps this path free of the destroy-during-click crash described on
// ShowStartup.
func (s *startup) open(c center.Center) {
	slog.Info("center selected", "name", c.Name, "dir", c.Dir)
	if err := s.onOpen(c, s.win); err != nil {
		// The Center could not be opened (already open in another instance):
		// keep the selection view up and explain, rather than replacing it.
		s.showError(err)
		return
	}
	s.opened = true
}

// showAddDialog prompts for a new Center name, creating its directory and
// selecting it in the list on success.
func (s *startup) showAddDialog() {
	entry := widget.NewEntry()
	entry.SetPlaceHolder("Center name")

	errLabel := canvas.NewText("", theme.Color(theme.ColorNameError))
	errLabel.TextSize = theme.CaptionTextSize()

	var popup dialog.Dialog
	confirm := func() {
		c, err := center.Create(s.appDir, entry.Text)
		if err != nil {
			if errors.Is(err, center.ErrExists) {
				errLabel.Text = "That Center already exists."
			} else {
				errLabel.Text = err.Error()
			}
			errLabel.Refresh()
			return
		}
		slog.Info("center created", "name", c.Name, "dir", c.Dir)
		popup.Hide()
		s.reload()
	}

	createBtn := widget.NewButton("Create", confirm)
	createBtn.Importance = widget.HighImportance
	cancelBtn := widget.NewButton("Cancel", func() { popup.Hide() })

	body := container.NewVBox(
		entry,
		widget.NewLabel("Name your student center."),
		errLabel,
		container.NewHBox(cancelBtn, createBtn),
	)
	popup = dialog.NewCustomWithoutButtons("Add Center", body, s.win)
	entry.OnSubmitted = func(string) { confirm() }
	popup.Show()
	s.win.Canvas().Focus(entry)
}

// centerContextMenu builds the right-click menu for one Center in the selection
// list. Separate from showing it so the items can be tested without a canvas.
func (s *startup) centerContextMenu(c center.Center) *fyne.Menu {
	return fyne.NewMenu("",
		fyne.NewMenuItem("Rename", func() { s.showRenameDialog(c) }),
	)
}

// showCenterContextMenu pops up the right-click menu under the pointer.
func (s *startup) showCenterContextMenu(c center.Center, pos fyne.Position) {
	widget.ShowPopUpMenuAtPosition(s.centerContextMenu(c), s.win.Canvas(), pos)
}

// showRenameDialog renames one Center. The name is checked as it is typed, so
// a clash with another Center disables Confirm and says why rather than letting
// the user press it and be refused.
//
// Validation is live but the rename itself is still checked on confirm: the
// directory is shared with anything else on this machine, so a name free when
// it was typed can be taken by the time it is used.
func (s *startup) showRenameDialog(c center.Center) {
	entry := widget.NewEntry()
	entry.SetText(c.Name)

	errLabel := canvas.NewText("", theme.Color(theme.ColorNameError))
	errLabel.TextSize = theme.CaptionTextSize()

	var popup dialog.Dialog
	confirmBtn := widget.NewButton("Confirm", nil)
	confirmBtn.Importance = widget.HighImportance

	// showProblem puts a message under the entry and disables Confirm; an empty
	// message clears it and enables Confirm again.
	showProblem := func(msg string) {
		errLabel.Text = msg
		errLabel.Refresh()
		if msg == "" {
			confirmBtn.Enable()
		} else {
			confirmBtn.Disable()
		}
	}

	entry.OnChanged = func(name string) { showProblem(s.renameProblem(c, name)) }
	// The entry starts at the Center's current name, which is valid, so Confirm
	// starts enabled. Stated rather than left to the order of the calls above.
	showProblem(s.renameProblem(c, entry.Text))

	confirm := func() {
		if problem := s.renameProblem(c, entry.Text); problem != "" {
			showProblem(problem)
			return
		}
		renamed, err := center.Rename(s.appDir, c.Name, entry.Text)
		if err != nil {
			if errors.Is(err, center.ErrExists) {
				showProblem("Center already exists")
			} else {
				showProblem(err.Error())
			}
			return
		}
		slog.Info("center renamed", "from", c.Name, "to", renamed.Name, "dir", renamed.Dir)
		popup.Hide()
		s.reload()
	}
	confirmBtn.OnTapped = confirm
	entry.OnSubmitted = func(string) { confirm() }

	cancelBtn := widget.NewButton("Cancel", func() { popup.Hide() })

	body := container.NewVBox(
		widget.NewLabel("Rename center "+c.Name),
		entry,
		errLabel,
		container.NewHBox(cancelBtn, confirmBtn),
	)
	popup = dialog.NewCustomWithoutButtons("Rename Center", body, s.win)
	popup.Show()
	s.win.Canvas().Focus(entry)
}

// renameProblem reports why c cannot be renamed to name, or "" if it can. It
// drives both the live check as the user types and the check on confirm, so the
// two can never disagree.
//
// The Center's own current name is not a clash: reopening the dialog and
// confirming without editing is a no-op, not an error. Comparison is
// case-insensitive because the app dir may sit on a case-insensitive
// filesystem, where "alpha" and "Alpha" are one directory -- except against the
// Center's own name, where a change of case is a legitimate rename.
func (s *startup) renameProblem(c center.Center, name string) string {
	if strings.TrimSpace(name) == "" {
		return "Enter a name"
	}
	if err := center.ValidateName(name); err != nil {
		return err.Error()
	}
	for _, other := range s.centers {
		if other.Name == c.Name {
			continue
		}
		if strings.EqualFold(other.Name, name) {
			return "Center already exists"
		}
	}
	return ""
}

func (s *startup) showError(err error) {
	s.errLbl.Text = err.Error()
	s.errLbl.Refresh()
}

// ShowFatalError displays a standalone message window with a Close button that
// quits the app. It is used when a Center chosen directly via -center or
// -db-path cannot be opened (e.g. it is already open in another instance) and
// there is no selection window to fall back to. The caller drives the Fyne
// event loop.
func ShowFatalError(fa fyne.App, message string) {
	w := fa.NewWindow("Check-In")

	msg := widget.NewLabel(message)
	msg.Wrapping = fyne.TextWrapWord

	closeBtn := widget.NewButton("Close", func() { fa.Quit() })
	closeBtn.Importance = widget.HighImportance

	w.SetContent(withWindowMargin(container.NewVBox(
		msg,
		container.NewHBox(layout.NewSpacer(), closeBtn),
	)))
	w.SetOnClosed(func() { fa.Quit() })
	w.Resize(fyne.NewSize(360, 160))
	w.CenterOnScreen()
	w.Show()
}
