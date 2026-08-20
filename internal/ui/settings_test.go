package ui

import (
	"os"
	"path/filepath"
	"testing"

	"fyne.io/fyne/v2/test"

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
		if row.entry.Text != config.Fields[i].Get(config.Default()) {
			t.Errorf("row %s prefilled with %q, want current value %q",
				row.field.Key, row.entry.Text, config.Fields[i].Get(config.Default()))
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
		case "prune_interval":
			row.entry.SetText("30m")
		case "prune_batch_size":
			row.entry.SetText("250")
		}
	}
	want.CameraFPS = 15
	want.CameraRequestWidth = 1280
	want.CameraRequestHeight = 720
	want.QRScanCooldown = 3e9          // 3s
	want.PruneInterval = 30 * 60 * 1e9 // 30m
	want.PruneBatchSize = 250

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
