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

	centers  []center.Center
	list     *widget.List
	selected int // index into centers, or -1 when nothing is selected

	openBtn *widget.Button
	errLbl  *canvas.Text

	// onOpen receives the chosen Center. The startup window is closed first, so
	// the callback is free to open the main window.
	onOpen func(center.Center)

	// opened records that a Center was chosen, so closing the window afterwards
	// is not treated as the user quitting.
	opened bool
}

// ShowStartup displays the Center selection window. onOpen is called with the
// chosen Center after the window closes; if the user closes the window without
// choosing, onOpen is never called and the process should exit. The caller
// drives the Fyne event loop.
func ShowStartup(fa fyne.App, appDir string, onOpen func(center.Center)) {
	s := &startup{fyneApp: fa, appDir: appDir, selected: -1, onOpen: onOpen}
	s.win = fa.NewWindow("Check-In — Select Center")
	s.build()
	s.reload("")
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

	s.list = widget.NewList(
		func() int { return len(s.centers) },
		func() fyne.CanvasObject { return widget.NewLabel("") },
		func(i widget.ListItemID, o fyne.CanvasObject) {
			o.(*widget.Label).SetText(s.centers[i].Name)
		},
	)
	s.list.OnSelected = func(i widget.ListItemID) {
		s.selected = i
		s.openBtn.Enable()
	}
	s.list.OnUnselected = func(widget.ListItemID) {
		s.selected = -1
		s.openBtn.Disable()
	}

	s.openBtn = widget.NewButton("Open", s.open)
	s.openBtn.Importance = widget.HighImportance
	s.openBtn.Disable()
	addBtn := widget.NewButton("Add Center…", s.showAddDialog)

	header := widget.NewLabel("Select a Center:")
	buttons := container.NewHBox(addBtn, layout.NewSpacer(), s.openBtn)
	bottom := container.NewVBox(s.errLbl, buttons)

	s.win.SetContent(withWindowMargin(
		container.NewBorder(header, bottom, nil, nil, s.list),
	))
	s.win.SetOnClosed(func() {
		if !s.opened {
			// No Center chosen: nothing else will run, so quit the app.
			s.fyneApp.Quit()
		}
	})
}

// reload re-lists the Centers on disk and repaints the list, selecting the
// Center named selectName when it is present.
func (s *startup) reload(selectName string) {
	centers, err := center.List(s.appDir)
	if err != nil {
		s.showError(err)
		return
	}
	s.centers = centers
	s.selected = -1
	s.openBtn.Disable()
	s.list.UnselectAll()
	s.list.Refresh()

	if selectName != "" {
		for i, c := range centers {
			if c.Name == selectName {
				s.list.Select(i)
				break
			}
		}
	}
}

func (s *startup) open() {
	if s.selected < 0 || s.selected >= len(s.centers) {
		return
	}
	c := s.centers[s.selected]
	s.opened = true
	slog.Info("center selected", "name", c.Name, "dir", c.Dir)
	s.win.Close()
	s.onOpen(c)
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
		s.reload(c.Name)
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
