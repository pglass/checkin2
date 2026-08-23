package ui

import (
	"os"
	"path/filepath"
	"testing"

	"fyne.io/fyne/v2"

	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/pglass/checkin/internal/config"
)

// newTestSettings builds a Settings window backed by a temp settings.ini,
// without opening a store or camera.
func newTestSettings(t *testing.T) (*settings, string) {
	t.Helper()
	fa := test.NewApp()
	t.Cleanup(fa.Quit)

	path := filepath.Join(t.TempDir(), "settings.ini")
	a := &App{fyneApp: fa, cfg: config.Default(), cfgPath: path, camDevice: deviceNone}
	a.win = fa.NewWindow("main")

	s := &settings{app: a}
	s.win = fa.NewWindow("Settings")
	s.win.SetContent(s.build())
	return s, path
}

// The window is generated from config.Fields, so every setting gets a row.
func TestSettingsBuildsRowPerField(t *testing.T) {
	s, _ := newTestSettings(t)
	if len(s.rows) != len(config.Fields) {
		t.Fatalf("rows = %d, want %d (one per config.Fields entry)", len(s.rows), len(config.Fields))
	}
	for i, row := range s.rows {
		if row.field.Key != config.Fields[i].Key {
			t.Errorf("row %d key = %q, want %q", i, row.field.Key, config.Fields[i].Key)
		}
		if row.value() != config.Fields[i].Get(config.Default()) {
			t.Errorf("row %s prefilled with %q, want current value %q",
				row.field.Key, row.value(), config.Fields[i].Get(config.Default()))
		}
	}
}

// Bad input shows a red error under the row and blocks the save entirely: the
// file must not be written, not even for the rows that did parse.
func TestSettingsSaveRejectsInvalidInput(t *testing.T) {
	s, path := newTestSettings(t)

	s.rows[0].entry.SetText("not-a-number")
	if s.rows[0].errLb.Text == "" {
		t.Fatal("typing an invalid value should display an error")
	}
	// A second row holds a valid new value; it must not be persisted either.
	s.rows[1].entry.SetText("800")

	s.save()

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("settings.ini written despite invalid input (stat err = %v)", err)
	}
	if s.app.cfg != config.Default() {
		t.Fatalf("live config changed despite invalid input: %+v", s.app.cfg)
	}
}

// Fixing the bad value clears its error.
func TestSettingsErrorClearsWhenFixed(t *testing.T) {
	s, _ := newTestSettings(t)
	s.rows[0].entry.SetText("oops")
	if s.rows[0].errLb.Text == "" {
		t.Fatal("want error for invalid value")
	}
	s.rows[0].entry.SetText("12")
	if s.rows[0].errLb.Text != "" {
		t.Fatalf("error not cleared after fix: %q", s.rows[0].errLb.Text)
	}
}

// A valid save writes the file, updates the live config, and round-trips
// through Load. Values are entered with surrounding space to confirm trimming.
func TestSettingsSaveWritesAndApplies(t *testing.T) {
	s, path := newTestSettings(t)

	want := config.Default()
	for _, row := range s.rows {
		// Bump each numeric/duration value to something distinct from default.
		switch row.field.Key {
		case "camera_fps":
			row.entry.SetText("  15  ")
		case "camera_request_width":
			row.entry.SetText("1280")
		case "camera_request_height":
			row.entry.SetText("720")
		case "qr_scan_cooldown":
			row.entry.SetText(" 3s ")
		}
	}
	want.CameraFPS = 15
	want.CameraRequestWidth = 1280
	want.CameraRequestHeight = 720
	want.QRScanCooldown = 3e9 // 3s

	s.save()

	if s.app.cfg != want {
		t.Fatalf("live config = %+v, want %+v", s.app.cfg, want)
	}
	got, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load written file: %v", err)
	}
	if got != want {
		t.Fatalf("reloaded = %+v, want %+v", got, want)
	}
}

// Cancel closes without writing anything.
func TestSettingsCancelDoesNotWrite(t *testing.T) {
	s, path := newTestSettings(t)
	s.rows[0].entry.SetText("99")
	s.win.Close()

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("cancel wrote settings.ini (stat err = %v)", err)
	}
	if s.app.cfg != config.Default() {
		t.Fatalf("cancel changed live config: %+v", s.app.cfg)
	}
}

// Saving persists to disk and updates the App's copy of the config, but does
// not restart anything: the running camera and pruner keep the settings they
// were started with until the user restarts the app, which is what the note at
// the bottom of the window tells them.
func TestSettingsSaveDoesNotRestartSubsystems(t *testing.T) {
	s, _ := newTestSettings(t)
	s.app.camDevice = deviceNone

	for _, row := range s.rows {
		if row.field.Key == "camera_fps" {
			row.entry.SetText("30")
		}
	}
	s.save()

	if s.app.cfg.CameraFPS != 30 {
		t.Fatalf("saved config not stored on App: CameraFPS = %d", s.app.cfg.CameraFPS)
	}
	// No camera was running and none may be started by a save.
	if s.app.camDevice != deviceNone {
		t.Fatalf("save started a camera: camDevice = %d", s.app.camDevice)
	}
}

// The per-setting error only occupies a line while it has something to say.
// A visible-but-empty error would space every setting apart.
func TestSettingsErrorHiddenUntilNeeded(t *testing.T) {
	s, _ := newTestSettings(t)

	for _, row := range s.rows {
		if row.errLb.Visible() {
			t.Fatalf("%s: error visible with no error text", row.field.Key)
		}
	}

	s.rows[0].entry.SetText("bad")
	if !s.rows[0].errLb.Visible() {
		t.Error("error not shown for invalid input")
	}

	s.rows[0].entry.SetText("12")
	if s.rows[0].errLb.Visible() {
		t.Error("error still visible after the value was fixed")
	}
}

// The restart warning appears only once a value actually differs from what the
// window was opened with, and goes away again if the edit is undone.
func TestSettingsRestartNoteOnlyWhenChanged(t *testing.T) {
	s, _ := newTestSettings(t)

	if s.restartNote.Visible() {
		t.Fatal("restart note visible before anything was changed")
	}

	orig := s.rows[0].entry.Text
	s.rows[0].entry.SetText("99")
	if !s.restartNote.Visible() {
		t.Error("restart note not shown after a value was changed")
	}

	// Undoing the edit means nothing will change on save.
	s.rows[0].entry.SetText(orig)
	if s.restartNote.Visible() {
		t.Error("restart note still visible after the edit was undone")
	}
}

// Retyping the same value is not a change.
func TestSettingsRestartNoteIgnoresIdenticalRetype(t *testing.T) {
	s, _ := newTestSettings(t)

	for _, row := range s.rows {
		if row.check != nil {
			row.check.SetChecked(row.orig == "true")
			continue
		}
		row.entry.SetText(row.orig)
	}
	if s.restartNote.Visible() {
		t.Error("restart note shown after retyping the same values")
	}
}

// descRichText digs the description widget out of row i's margin wrapper.
func descRichText(t *testing.T, row *settingsRow) *widget.RichText {
	t.Helper()
	// block[1] is the description, wrapped in its margin container; see build().
	wrapper, ok := row.block[1].(*fyne.Container)
	if !ok {
		t.Fatalf("%s: description block is %T, want a container", row.field.Key, row.block[1])
	}
	rt, ok := wrapper.Objects[0].(*widget.RichText)
	if !ok {
		t.Fatalf("%s: description is %T, want *widget.RichText", row.field.Key, wrapper.Objects[0])
	}
	return rt
}

// Descriptions wrap rather than forcing the window wide. A canvas.Text reports
// its whole string width as its minimum size, so a long description used to
// push the settings window out to fit it on one line.
func TestSettingsDescriptionsWrap(t *testing.T) {
	s, _ := newTestSettings(t)

	longest := ""
	for _, f := range config.Fields {
		if len(f.Desc) > len(longest) {
			longest = f.Desc
		}
	}
	if len(longest) < 60 {
		t.Skipf("no description long enough to be a useful check (%d chars)", len(longest))
	}

	var desc *widget.RichText
	for _, row := range s.rows {
		if row.field.Desc == longest {
			desc = descRichText(t, row)
		}
	}
	if desc == nil {
		t.Fatal("longest description not found among the rows")
	}

	if desc.Wrapping != fyne.TextWrapWord {
		t.Errorf("Wrapping = %v, want TextWrapWord", desc.Wrapping)
	}
	// The whole point: it must not demand the full one-line width.
	oneLine := widget.NewRichText(&widget.TextSegment{
		Text:  longest,
		Style: widget.RichTextStyle{SizeName: theme.SizeNameCaptionText},
	})
	if desc.MinSize().Width >= oneLine.MinSize().Width {
		t.Errorf("wrapped description min width %v is not smaller than unwrapped %v",
			desc.MinSize().Width, oneLine.MinSize().Width)
	}
}

// The description keeps the exact caption size and placeholder colour it had as
// a canvas.Text.
func TestSettingsDescriptionStyle(t *testing.T) {
	s, _ := newTestSettings(t)
	for _, row := range s.rows {
		seg, ok := descRichText(t, row).Segments[0].(*widget.TextSegment)
		if !ok {
			t.Fatalf("%s: segment is %T, want *widget.TextSegment", row.field.Key, seg)
		}
		if seg.Style.SizeName != theme.SizeNameCaptionText {
			t.Errorf("%s: SizeName = %q, want caption", row.field.Key, seg.Style.SizeName)
		}
		if seg.Style.ColorName != theme.ColorNamePlaceHolder {
			t.Errorf("%s: ColorName = %q, want placeholder", row.field.Key, seg.Style.ColorName)
		}
	}
}

// The description hugs the input above it and leaves more space below, so
// settings read as blocks rather than an even column of lines.
func TestSettingsDescriptionMargins(t *testing.T) {
	s, _ := newTestSettings(t)

	wrapper, ok := s.rows[0].block[1].(*fyne.Container)
	if !ok {
		t.Fatalf("description block is %T, want a container", s.rows[0].block[1])
	}
	m, ok := wrapper.Layout.(marginLayout)
	if !ok {
		t.Fatalf("description layout is %T, want marginLayout", wrapper.Layout)
	}
	if m.bottom <= m.top {
		t.Errorf("bottom margin %v should exceed top margin %v", m.bottom, m.top)
	}
}
