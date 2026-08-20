package ui

import (
	"errors"
	"log/slog"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/pglass/checkin/internal/center"
)

// startup is the Center selection window shown before the main window.
type startup struct {
	fyneApp fyne.App
	win     fyne.Window
	appDir  string

	// about supplies the About window, so version and license information is
	// reachable before any Center is open.
	about about

	centers []center.Center
	list    *widget.List
	errLbl  *canvas.Text

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
func ShowStartup(fa fyne.App, appDir string, onOpen func(center.Center, fyne.Window) error) {
	s := &startup{fyneApp: fa, appDir: appDir, onOpen: onOpen}
	s.win = fa.NewWindow("Check-In")
	s.about = about{fyneApp: fa, parent: s.win}
	// The selection window has no Center-specific actions yet, so its File menu
	// holds only About -- but the menu is always present, so the menubar does
	// not appear and disappear between the two windows.
	s.win.SetMainMenu(fyne.NewMainMenu(fyne.NewMenu("File", s.about.menuItem())))
	s.build()
	s.reload()
	s.win.Resize(fyne.NewSize(360, 320))
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
		func() fyne.CanvasObject { return widget.NewButton("", nil) },
		func(i widget.ListItemID, o fyne.CanvasObject) {
			btn := o.(*widget.Button)
			c := s.centers[i]
			btn.SetText(c.Name)
			btn.Alignment = widget.ButtonAlignLeading
			btn.Importance = widget.LowImportance
			btn.OnTapped = func() { s.open(c) }
		},
	)

	addBtn := widget.NewButton("Add Center…", s.showAddDialog)

	header := widget.NewLabel("Select a Center:")
	buttons := container.NewHBox(addBtn, layout.NewSpacer())
	bottom := container.NewVBox(s.errLbl, buttons)

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

// reload re-lists the Centers on disk and repaints the list.
func (s *startup) reload() {
	centers, err := center.List(s.appDir)
	if err != nil {
		s.showError(err)
		return
	}
	s.centers = centers
	s.list.Refresh()
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
