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
	// error), so an advanced row can be hidden and shown as a unit.
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

// settings holds the widgets for one Settings window. Rows are generated from
// config.Fields, so a setting added to that table appears here automatically —
// there is no per-setting code in this file.
type settings struct {
	app  *App
	win  fyne.Window
	rows []*settingsRow
	// restartNote warns that saved settings only take effect at the next
	// startup. Shown only once a value actually differs from what was loaded.
	restartNote *canvas.Text
	// showAdvanced reflects the "Show advanced settings" toggle, and advancedChk
	// is the toggle itself, so a hidden invalid value can force it open.
	showAdvanced bool
	advancedChk  *widget.Check
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
		// the gaps look large. The row keeps its own widgets so an advanced
		// setting can be hidden and shown as a unit.
		row.block = []fyne.CanvasObject{top, descBlock, row.errLb}
		blocks = append(blocks, row.block...)
		s.rows = append(s.rows, row)
	}

	form := container.NewVBox(blocks...)

	s.advancedChk = widget.NewCheck("Show advanced settings", func(on bool) {
		s.showAdvanced = on
		s.applyAdvancedVisibility()
	})

	saveBtn := widget.NewButton("Save", s.save)
	saveBtn.Importance = widget.HighImportance
	cancelBtn := widget.NewButton("Cancel", func() { s.win.Close() })
	buttons := container.NewHBox(layout.NewSpacer(), cancelBtn, saveBtn)

	// Saved settings are read at startup only, so say so where the user is
	// about to save rather than letting a changed value appear to do nothing.
	// Red and bold rather than a quiet caption: this is the one thing in the
	// window a user must not miss.
	s.restartNote = canvas.NewText(
		"You must close and re-open the application for changed settings to take effect",
		theme.Color(theme.ColorNameError))
	s.restartNote.TextSize = theme.CaptionTextSize()
	s.restartNote.TextStyle = fyne.TextStyle{Bold: true}
	// Nothing has been edited yet, so there is nothing to warn about.
	s.restartNote.Hide()

	bottom := container.NewVBox(widget.NewSeparator(), s.advancedChk, s.restartNote, buttons)

	// Advanced settings start hidden; the checkbox above reveals them.
	s.applyAdvancedVisibility()

	// The form scrolls so the window stays usable as settings are added.
	return container.NewBorder(nil, bottom, nil, nil, container.NewVScroll(form))
}

// applyAdvancedVisibility hides or shows every advanced setting's widgets to
// match the toggle. A hidden row's error stays hidden regardless, since it is
// only shown while it has text (see validateRow).
func (s *settings) applyAdvancedVisibility() {
	for _, row := range s.rows {
		if !row.field.Advanced {
			continue
		}
		for _, o := range row.block {
			switch {
			case !s.showAdvanced:
				o.Hide()
			case o == fyne.CanvasObject(row.errLb):
				// Restore only if it has something to say.
				if row.errLb.Text != "" {
					o.Show()
				}
			default:
				o.Show()
			}
		}
	}
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
	cfg := s.app.cfg
	ok := true
	badAdvanced := false
	for _, row := range s.rows {
		// Validate into the real config so valid rows carry their new value,
		// and re-check every row so all errors are shown at once, not just the
		// first.
		if err := row.field.Set(&cfg, row.value()); err != nil {
			ok = false
		}
		if !s.validateRow(row) {
			ok = false
			if row.field.Advanced {
				badAdvanced = true
			}
		}
	}
	if !ok {
		// An invalid advanced value would otherwise block the save with its
		// error hidden behind the toggle, making Save look like it did nothing.
		if badAdvanced && !s.showAdvanced {
			s.showAdvanced = true
			s.advancedChk.SetChecked(true)
			s.applyAdvancedVisibility()
		}
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
