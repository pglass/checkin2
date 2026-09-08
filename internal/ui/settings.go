package ui

import (
	"log/slog"
	"strconv"

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

// Vertical margins around a setting's description: tight above so it reads as
// belonging to the input it follows, looser below to separate one setting from
// the next. RichText carries its own internal padding, so the top margin is
// negative to pull it back up under the input.
const (
	settingsDescTopMargin    = -6
	settingsDescBottomMargin = 8
)

// settingsRow is one editable setting: the key label, its input, the small
// description below, and the red error shown while the input is invalid.
//
// The input is either an entry or a checkbox, never both -- boolean settings
// get a checkbox, everything else a text entry. Read the current value through
// value() rather than touching the widgets, so the rest of the window does not
// care which kind it is.
type settingsRow struct {
	field config.Field
	entry *widget.Entry // nil for boolean settings
	check *widget.Check // nil for everything else
	errLb *canvas.Text
	// orig is the value the input was built with, so an edit that is undone
	// counts as unchanged again.
	orig string
	// block holds every widget of this setting (key + input, description, and
	// error), kept together so the row can be handled as a unit.
	block []fyne.CanvasObject
}

// value returns the row's current value in the same string form config.Field's
// Get produces and Set parses.
func (r *settingsRow) value() string {
	if r.check != nil {
		return strconv.FormatBool(r.check.Checked)
	}
	return r.entry.Text
}

// isBoolField reports whether f holds a boolean, which is shown as a checkbox
// rather than a text entry. Determined by parsing the field's own default
// rather than by naming specific keys, so a new boolean setting added to
// config.Fields is picked up with no change here.
func isBoolField(f config.Field) bool {
	_, err := strconv.ParseBool(f.Get(config.Default()))
	return err == nil
}

// settingsHost is what a Settings window needs from whatever opened it: the
// current settings, where to write them, and somewhere to hand the saved values
// back to. Both the main window (*App) and the Center selection window
// (*startup) implement it, so one Settings window serves both rather than the
// selection screen growing a second copy.
type settingsHost interface {
	// settingsConfig is the configuration the window is populated from.
	settingsConfig() config.Config
	// settingsPath is the settings.ini that Save writes to.
	settingsPath() string
	// applySettings receives the saved configuration. Implementations decide
	// how much of it can take effect without a restart.
	applySettings(config.Config)
	// window supplies the parent for error dialogs and the Fyne app the
	// Settings window is created from.
	settingsApp() fyne.App
	// settingsRestartNote is the warning shown once a value has been edited.
	// It is per-host because how much of a saved change takes effect right away
	// depends on where the window was opened from.
	settingsRestartNote() string
}

// settings holds the widgets for one Settings window. Rows are generated from
// config.Fields, so a setting added to that table appears here automatically —
// there is no per-setting code in this file.
type settings struct {
	host settingsHost
	win  fyne.Window
	rows []*settingsRow
	// restartNote warns that saved settings only take effect at the next
	// startup. Shown only once a value actually differs from what was loaded.
	restartNote *canvas.Text
}

// showSettingsWindow opens the Settings window, or raises the existing one.
// settingsConfig, settingsPath, settingsApp, and applySettings implement
// settingsHost for the main window.
func (a *App) settingsConfig() config.Config { return a.cfg }
func (a *App) settingsPath() string          { return a.cfgPath }
func (a *App) settingsApp() fyne.App         { return a.fyneApp }

// settingsRestartNote: a Center is open, and the camera and QR scanning keep
// the values they started with, so every change here waits for a restart.
func (a *App) settingsRestartNote() string {
	return "You must close and re-open the application for changed settings to take effect"
}

// applySettings stores the saved settings. They are not applied to the running
// app: the camera and QR scanning keep using the values read at startup, which
// is what the window's restart warning is about.
func (a *App) applySettings(cfg config.Config) { a.cfg = cfg }

func (a *App) showSettingsWindow() {
	if a.settingsWin != nil {
		a.settingsWin.RequestFocus()
		return
	}
	a.settingsWin = showSettingsFor(a, func() { a.settingsWin = nil })
}

// showSettingsFor opens a Settings window over host and returns it, calling
// onClosed when it goes away so the caller can drop its reference and let the
// next request open a fresh one. Callers track the returned window to raise the
// existing window instead of spawning a duplicate.
func showSettingsFor(host settingsHost, onClosed func()) fyne.Window {
	s := &settings{host: host}
	s.win = host.settingsApp().NewWindow("Settings")
	s.win.SetContent(withWindowMargin(s.build()))
	s.win.Resize(fyne.NewSize(560, 620))
	s.win.SetOnClosed(onClosed)
	s.win.Show()
	return s.win
}

// build lays out one block per setting above the Save/Cancel buttons.
func (s *settings) build() fyne.CanvasObject {
	cfg := s.host.settingsConfig()

	blocks := []fyne.CanvasObject{}
	for _, f := range config.Fields {
		row := &settingsRow{field: f}

		row.orig = f.Get(cfg)
		// Validate as the input changes so an error appears next to the
		// offending value rather than only after pressing Save, and re-check
		// whether the restart warning still applies.
		onChange := func() {
			s.validateRow(row)
			s.updateRestartNote()
		}

		key := canvas.NewText(f.Key, theme.Color(theme.ColorNameForeground))
		key.TextSize = theme.TextSize()

		// A wrapping RichText, not canvas.Text: canvas.Text is single-line and
		// reports its full string width as its minimum size, so a long
		// description forced the whole window wide. RichText wraps, and unlike
		// widget.Label it takes an exact theme colour, so the description keeps
		// the placeholder grey and caption size it has always had.
		desc := widget.NewRichText(&widget.TextSegment{
			Text: f.Desc,
			Style: widget.RichTextStyle{
				ColorName: theme.ColorNamePlaceHolder,
				SizeName:  theme.SizeNameCaptionText,
			},
		})
		desc.Wrapping = fyne.TextWrapWord

		// Sits tight under the input it describes, with clear space before the
		// next setting.
		descBlock := container.New(
			marginLayout{top: settingsDescTopMargin, bottom: settingsDescBottomMargin}, desc)

		row.errLb = canvas.NewText("", theme.Color(theme.ColorNameError))
		row.errLb.TextSize = theme.CaptionTextSize()

		// Built only now, and the initial value set BEFORE the callback is
		// attached: Check.SetChecked fires OnChanged, and onChange reads
		// row.errLb, which must already exist (same ordering rule as
		// history.build). Attaching after the initial value also stops the
		// restart note appearing before the user has touched anything.
		var input fyne.CanvasObject
		if isBoolField(f) {
			// A checkbox cannot hold an invalid value, so it needs no
			// validation of its own -- but it still drives the restart note.
			row.check = widget.NewCheck("", nil)
			row.check.SetChecked(row.orig == "true")
			row.check.OnChanged = func(bool) { onChange() }
			input = row.check
		} else {
			row.entry = widget.NewEntry()
			row.entry.SetText(row.orig)
			row.entry.OnChanged = func(string) { onChange() }
			input = row.entry
		}

		// Fixed-width key column keeps every input left-aligned with the others.
		keyCol := container.New(fixedWidthLayout{w: settingsKeyWidth}, key)
		top := container.NewBorder(nil, nil, keyCol, nil, input)

		// The error is hidden until it has text: a shown-but-empty canvas.Text
		// still occupies a line, which would space every setting apart even when
		// nothing is wrong.
		row.errLb.Hide()

		// Appended flat rather than wrapped in a per-setting VBox: nesting VBoxes
		// pays the container's padding twice between settings, which is what made
		// the gaps look large.
		row.block = []fyne.CanvasObject{top, descBlock, row.errLb}
		blocks = append(blocks, row.block...)
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
	// window a user must not miss. The selection window applies what it reads
	// per-backup, so it supplies its own, milder wording rather than telling a
	// user to restart for a setting that already took effect.
	s.restartNote = canvas.NewText(
		s.host.settingsRestartNote(),
		theme.Color(theme.ColorNameError))
	s.restartNote.TextSize = theme.CaptionTextSize()
	s.restartNote.TextStyle = fyne.TextStyle{Bold: true}
	// Nothing has been edited yet, so there is nothing to warn about.
	s.restartNote.Hide()

	bottom := container.NewVBox(widget.NewSeparator(), s.restartNote, buttons)

	// The form scrolls so the window stays usable as settings are added.
	return container.NewBorder(nil, bottom, nil, nil, container.NewVScroll(form))
}

// validateRow parses one row's input, showing or clearing its error message.
// It reports whether the value is valid.
func (s *settings) validateRow(row *settingsRow) bool {
	var probe config.Config
	if err := row.field.Set(&probe, row.value()); err != nil {
		row.errLb.Text = err.Error()
		row.errLb.Refresh()
		row.errLb.Show() // takes a line only while there is something to say
		return false
	}
	row.errLb.Text = ""
	row.errLb.Refresh()
	row.errLb.Hide()
	return true
}

// updateRestartNote shows the restart warning only while some value differs
// from the one the window was opened with, so a user who opens Settings and
// changes nothing (or undoes an edit) is not told to restart.
func (s *settings) updateRestartNote() {
	if s.restartNote == nil {
		return
	}
	if s.dirty() {
		s.restartNote.Show()
	} else {
		s.restartNote.Hide()
	}
}

// dirty reports whether any input differs from the value it was built with.
func (s *settings) dirty() bool {
	for _, row := range s.rows {
		if row.value() != row.orig {
			return true
		}
	}
	return false
}

// save validates every row and, on success, writes settings.ini. Nothing is
// written unless all rows parse, so the file never holds a half-applied edit.
// The new values take effect at the next startup, not immediately.
func (s *settings) save() {
	cfg := s.host.settingsConfig()
	ok := true
	for _, row := range s.rows {
		// Validate into the real config so valid rows carry their new value,
		// and re-check every row so all errors are shown at once, not just the
		// first.
		if err := row.field.Set(&cfg, row.value()); err != nil {
			ok = false
		}
		if !s.validateRow(row) {
			ok = false
		}
	}
	if !ok {
		return // errors are already displayed under the offending inputs
	}

	if err := config.Write(s.host.settingsPath(), cfg); err != nil {
		dialog.ShowError(err, s.win)
		return
	}
	slog.Info("settings saved", "path", s.host.settingsPath())

	// Saved values are not applied to the running app: the camera and QR
	// scanning keep using the settings read at startup. Restarting those in
	// place proved fragile (a camera cannot be reopened until its driver has
	// released the device), so the window tells the user to restart instead.
	s.host.applySettings(cfg)
	s.win.Close()
}
