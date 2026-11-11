package ui

import (
	"fmt"
	"log/slog"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/storage"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/pglass/checkin/internal/backup"
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

// settingsList is a List setting (config.Field.List): one entry per value, each
// with Browse… (for a directory setting) and Remove buttons, and an Add button
// below them.
type settingsList struct {
	field config.Field
	// orig is the values the list was built with, so an edit that is undone
	// counts as unchanged again.
	orig    []string
	entries []*widget.Entry
	// items holds one row per entry; rebuilt in place as values are added and
	// removed.
	items *fyne.Container
}

// values returns the list's current values, in order, including empty ones;
// the field's SetList drops those on save.
func (l *settingsList) values() []string {
	out := make([]string, len(l.entries))
	for i, e := range l.entries {
		out[i] = e.Text
	}
	return out
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
	// depends on where the window was opened from. An empty string means every
	// setting applies as soon as it is saved, and no warning is shown at all.
	settingsRestartNote() string
}

// settings holds the widgets for one Settings window. Rows are generated from
// config.Fields, so a setting added to that table appears here automatically —
// there is no per-setting code in this file.
type settings struct {
	host  settingsHost
	win   fyne.Window
	rows  []*settingsRow
	lists []*settingsList
	// restartNote warns that saved settings only take effect at the next
	// startup. Shown only once a value actually differs from what was loaded,
	// and nil when the host has no warning to give.
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
		// Hidden settings live in settings.ini only; see config.Field.Hidden.
		if f.Hidden {
			continue
		}
		if f.Key == config.KeyBackupDirs {
			// The internal directory is shown just above the list it is
			// copied to, so the two read as one sequence.
			blocks = append(blocks, s.internalBackupDirBlock()...)
		}
		if f.List {
			blocks = append(blocks, s.buildList(f, cfg)...)
			continue
		}
		row := &settingsRow{field: f}

		row.orig = f.Get(cfg)
		// Validate as the input changes so an error appears next to the
		// offending value rather than only after pressing Save, and re-check
		// whether the restart warning still applies.
		onChange := func() {
			s.validateRow(row)
			s.updateRestartNote()
		}

		key := settingsKey(f.Key)
		descBlock := settingsDesc(f.Desc)

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
			// A directory setting gets a Browse… button beside its entry.
			// Typing stays available: a path on a drive that is not mounted
			// right now cannot be browsed to, but is still worth saving.
			if f.Directory {
				input = container.NewBorder(nil, nil, nil,
					widget.NewButton("Browse…", func() { s.browseDir(row.entry) }), row.entry)
			}
		}

		top := settingsKeyed(key, input)

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
	// window a user must not miss. A host with nothing to warn about -- the
	// selection window, where no Center has read a setting yet and every saved
	// value is live -- gives an empty note and gets no widget at all, rather
	// than a reassurance nobody needs taking up the same red line.
	bottomItems := []fyne.CanvasObject{widget.NewSeparator()}
	if note := s.host.settingsRestartNote(); note != "" {
		s.restartNote = canvas.NewText(note, theme.Color(theme.ColorNameError))
		s.restartNote.TextSize = theme.CaptionTextSize()
		s.restartNote.TextStyle = fyne.TextStyle{Bold: true}
		// Nothing has been edited yet, so there is nothing to warn about.
		s.restartNote.Hide()
		bottomItems = append(bottomItems, s.restartNote)
	}
	bottomItems = append(bottomItems, buttons)

	bottom := container.NewVBox(bottomItems...)

	// The form scrolls so the window stays usable as settings are added.
	return container.NewBorder(nil, bottom, nil, nil, container.NewVScroll(form))
}

// settingsKey is the label in a setting's key column.
func settingsKey(k string) *canvas.Text {
	key := canvas.NewText(k, theme.Color(theme.ColorNameForeground))
	key.TextSize = theme.TextSize()
	return key
}

// settingsKeyed puts key in the fixed-width key column beside input, which keeps
// every input left-aligned with the others.
func settingsKeyed(key, input fyne.CanvasObject) fyne.CanvasObject {
	keyCol := container.New(fixedWidthLayout{w: settingsKeyWidth}, key)
	return container.NewBorder(nil, nil, keyCol, nil, input)
}

// settingsDesc is the small description shown under a setting's input.
//
// A wrapping RichText, not canvas.Text: canvas.Text is single-line and reports
// its full string width as its minimum size, so a long description forced the
// whole window wide. RichText wraps, and unlike widget.Label it takes an exact
// theme colour, so the description keeps the placeholder grey and caption size.
func settingsDesc(text string) fyne.CanvasObject {
	desc := widget.NewRichText(&widget.TextSegment{
		Text: text,
		Style: widget.RichTextStyle{
			ColorName: theme.ColorNamePlaceHolder,
			SizeName:  theme.SizeNameCaptionText,
		},
	})
	desc.Wrapping = fyne.TextWrapWord
	// Sits tight under the input it describes, with clear space before the
	// next setting.
	return container.New(
		marginLayout{top: settingsDescTopMargin, bottom: settingsDescBottomMargin}, desc)
}

// internalBackupDirKey labels the internal backup directory in the window. It
// is not a setting -- the directory is fixed beside the app directory -- so it
// has no entry in config.Fields and nothing in settings.ini.
const internalBackupDirKey = "internal_backup_dir"

// internalBackupDirBlock shows the internal backup directory, read-only. The
// label is selectable, so the path can be copied to go and look at it.
func (s *settings) internalBackupDirBlock() []fyne.CanvasObject {
	// settings.ini lives at the top of the app directory (center.SettingsPath),
	// which is what the internal backup directory is derived from.
	dir := backup.InternalDir(filepath.Dir(s.host.settingsPath()))
	path := widget.NewLabel(dir)
	path.Selectable = true
	path.Truncation = fyne.TextTruncateEllipsis
	return []fyne.CanvasObject{
		settingsKeyed(settingsKey(internalBackupDirKey), path),
		settingsDesc("Backups are written first to this internal directory. " +
			"Then they are copied to the following other backup directories."),
	}
}

// buildList lays out a List setting: an entry per current value, then the Add
// button, beside the key, with the description below.
func (s *settings) buildList(f config.Field, cfg config.Config) []fyne.CanvasObject {
	l := &settingsList{field: f, orig: append([]string(nil), f.GetList(cfg)...)}
	l.items = container.NewVBox()
	for _, v := range l.orig {
		s.addListEntry(l, v)
	}
	add := widget.NewButtonWithIcon(f.AddLabel, theme.ContentAddIcon(), func() {
		s.addListEntry(l, "")
		s.updateRestartNote()
	})
	s.lists = append(s.lists, l)
	return []fyne.CanvasObject{
		settingsKeyed(settingsKey(f.Key), container.NewVBox(l.items, container.NewHBox(add))),
		settingsDesc(f.Desc),
	}
}

// addListEntry appends an entry holding v to l, with its Browse… and Remove
// buttons.
func (s *settings) addListEntry(l *settingsList, v string) {
	e := widget.NewEntry()
	e.SetText(v)
	e.OnChanged = func(string) { s.updateRestartNote() }
	l.entries = append(l.entries, e)

	buttons := container.NewHBox()
	if l.field.Directory {
		buttons.Add(widget.NewButton("Browse…", func() { s.browseDir(e) }))
	}
	buttons.Add(widget.NewButton("Remove", func() { s.confirmRemoveListEntry(l, e) }))
	l.items.Add(container.NewBorder(nil, nil, nil, buttons, e))
}

// confirmRemoveListEntry asks before removing e from l. An empty entry holds
// nothing worth confirming, so it goes straight away.
func (s *settings) confirmRemoveListEntry(l *settingsList, e *widget.Entry) {
	v := strings.TrimSpace(e.Text)
	if v == "" {
		s.removeListEntry(l, e)
		return
	}
	d := dialog.NewConfirm("Remove", fmt.Sprintf(l.field.RemovePrompt, v), func(ok bool) {
		if ok {
			s.removeListEntry(l, e)
		}
	}, s.win)
	d.SetConfirmText("Remove")
	d.Show()
}

// removeListEntry drops e and its row from l. Nothing is saved until Save.
func (s *settings) removeListEntry(l *settingsList, e *widget.Entry) {
	for i, x := range l.entries {
		if x != e {
			continue
		}
		l.entries = append(l.entries[:i], l.entries[i+1:]...)
		l.items.Remove(l.items.Objects[i])
		break
	}
	s.updateRestartNote()
}

// browseDir opens a folder picker for a directory setting and puts the chosen
// path into entry. Setting the text fires the entry's OnChanged, so the value
// is validated and the restart note updated exactly as if it had been typed.
//
// The picker starts in the directory the entry currently names, so reopening it
// after a mistake does not begin from scratch. An unset or unusable path falls
// back to the picker's default location rather than failing.
func (s *settings) browseDir(entry *widget.Entry) {
	d := dialog.NewFolderOpen(func(lu fyne.ListableURI, err error) {
		if err != nil {
			dialog.ShowError(err, s.win)
			return
		}
		if lu == nil {
			return // cancelled
		}
		entry.SetText(lu.Path())
	}, s.win)

	if start := listableDir(entry.Text); start != nil {
		d.SetLocation(start)
	}

	// Show before Resize: Resize reaches through the internal window that Show
	// creates, so resizing first panics (see importer.onBrowse).
	d.Show()
	d.Resize(s.win.Canvas().Size())
}

// listableDir returns path as a listable URI, or nil when it cannot be listed
// -- empty, missing, not a directory, or on a drive that is not mounted. A nil
// result means "no starting location", not an error.
func listableDir(path string) fyne.ListableURI {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	lu, err := storage.ListerForURI(storage.NewFileURI(path))
	if err != nil {
		return nil
	}
	return lu
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
// A list counts as changed only if its non-empty values differ, so adding a
// blank entry is not yet a change.
func (s *settings) dirty() bool {
	for _, row := range s.rows {
		if row.value() != row.orig {
			return true
		}
	}
	for _, l := range s.lists {
		var now, was config.Config
		_ = l.field.SetList(&now, l.values())
		_ = l.field.SetList(&was, l.orig)
		if !slices.Equal(l.field.GetList(now), l.field.GetList(was)) {
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
	for _, l := range s.lists {
		if err := l.field.SetList(&cfg, l.values()); err != nil {
			dialog.ShowError(err, s.win)
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
