package ui

import (
	"log/slog"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/pglass/checkin/internal/config"
)

// settingsKeyWidth is the width of the key column, wide enough for the longest
// key so every input in the window starts at the same x position.
const settingsKeyWidth = 190

// settingsRow is one editable setting: the key label, its input, the small
// description below, and the red error shown while the input is invalid.
type settingsRow struct {
	field config.Field
	entry *widget.Entry
	errLb *canvas.Text
}

// settings holds the widgets for one Settings window. Rows are generated from
// config.Fields, so a setting added to that table appears here automatically —
// there is no per-setting code in this file.
type settings struct {
	app  *App
	win  fyne.Window
	rows []*settingsRow
}

// showSettingsWindow opens the Settings window, or raises the existing one.
func (a *App) showSettingsWindow() {
	if a.settingsWin != nil {
		a.settingsWin.RequestFocus()
		return
	}

	s := &settings{app: a}
	s.win = a.fyneApp.NewWindow("Settings")
	s.win.SetContent(withWindowMargin(s.build()))
	s.win.Resize(fyne.NewSize(560, 620))
	s.win.SetOnClosed(func() { a.settingsWin = nil })
	a.settingsWin = s.win
	s.win.Show()
}

// build lays out one block per setting above the Save/Cancel buttons.
func (s *settings) build() fyne.CanvasObject {
	cfg := s.app.cfg

	blocks := []fyne.CanvasObject{}
	for _, f := range config.Fields {
		row := &settingsRow{field: f}

		row.entry = widget.NewEntry()
		row.entry.SetText(f.Get(cfg))
		// Validate as the user types so the error appears next to the offending
		// value rather than only after pressing Save.
		row.entry.OnChanged = func(string) { s.validateRow(row) }

		key := canvas.NewText(f.Key, theme.Color(theme.ColorNameForeground))
		key.TextSize = theme.TextSize()

		desc := canvas.NewText(f.Desc, theme.Color(theme.ColorNamePlaceHolder))
		desc.TextSize = theme.CaptionTextSize()

		row.errLb = canvas.NewText("", theme.Color(theme.ColorNameError))
		row.errLb.TextSize = theme.CaptionTextSize()

		// Fixed-width key column keeps every input left-aligned with the others.
		keyCol := container.New(fixedWidthLayout{w: settingsKeyWidth}, key)
		top := container.NewBorder(nil, nil, keyCol, nil, row.entry)

		blocks = append(blocks, container.NewVBox(top, desc, row.errLb))
		s.rows = append(s.rows, row)
	}

	form := container.NewVBox(blocks...)

	saveBtn := widget.NewButton("Save", s.save)
	saveBtn.Importance = widget.HighImportance
	cancelBtn := widget.NewButton("Cancel", func() { s.win.Close() })
	buttons := container.NewHBox(layout.NewSpacer(), cancelBtn, saveBtn)

	// Saved settings are read at startup only, so say so where the user is
	// about to save rather than letting a changed value appear to do nothing.
	// Red and bold rather than a quiet caption: this is the one thing in the
	// window a user must not miss.
	restartNote := canvas.NewText(
		"You must close and re-open the application for changed settings to take effect",
		theme.Color(theme.ColorNameError))
	restartNote.TextSize = theme.CaptionTextSize()
	restartNote.TextStyle = fyne.TextStyle{Bold: true}

	bottom := container.NewVBox(widget.NewSeparator(), restartNote, buttons)

	// The form scrolls so the window stays usable as settings are added.
	return container.NewBorder(nil, bottom, nil, nil, container.NewVScroll(form))
}

// validateRow parses one row's input, showing or clearing its error message.
// It reports whether the value is valid.
func (s *settings) validateRow(row *settingsRow) bool {
	var probe config.Config
	if err := row.field.Set(&probe, row.entry.Text); err != nil {
		row.errLb.Text = err.Error()
		row.errLb.Refresh()
		return false
	}
	row.errLb.Text = ""
	row.errLb.Refresh()
	return true
}

// save validates every row and, on success, writes settings.ini. Nothing is
// written unless all rows parse, so the file never holds a half-applied edit.
// The new values take effect at the next startup, not immediately.
func (s *settings) save() {
	cfg := s.app.cfg
	ok := true
	for _, row := range s.rows {
		// Validate into the real config so valid rows carry their new value,
		// and re-check every row so all errors are shown at once, not just the
		// first.
		if err := row.field.Set(&cfg, row.entry.Text); err != nil {
			ok = false
		}
		if !s.validateRow(row) {
			ok = false
		}
	}
	if !ok {
		return // errors are already displayed under the offending inputs
	}

	if err := config.Write(s.app.cfgPath, cfg); err != nil {
		dialog.ShowError(err, s.win)
		return
	}
	slog.Info("settings saved", "path", s.app.cfgPath)

	// Saved values are not applied to the running app: the camera, QR scanning,
	// and the pruner keep using the settings read at startup. Restarting those
	// in place proved fragile (a camera cannot be reopened until its driver has
	// released the device), so the window tells the user to restart instead.
	s.app.cfg = cfg
	s.win.Close()
}
