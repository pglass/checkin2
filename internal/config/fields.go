package config

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Field describes one user-editable setting: its INI key, the help text shown
// in the settings UI and written as an INI comment, and typed access to the
// Config struct.
//
// This table is the single source of truth for settings. Load, Write, and the
// Settings window all iterate Fields, so adding a setting means adding one
// entry here (plus the struct field it reads and writes) and every one of
// those surfaces picks it up automatically. TestFieldsCoverAllConfigFields
// fails if a Config field has no entry, so a new setting cannot be added
// without also appearing in the UI and the file.
type Field struct {
	// Key is the INI key, also used as the label in the settings UI.
	Key string
	// Section is the INI section header the key is written under.
	Section string
	// Desc is the help text: shown under the input in the settings UI and
	// written above the key as a comment in settings.ini.
	Desc string
	// Get renders the current value as the string shown in the INI file and
	// the settings UI input.
	Get func(c Config) string
	// Set parses s and stores it in c. It returns an error describing the
	// expected format when s is invalid; the settings UI shows that error
	// verbatim, so it must read as a complete user-facing sentence.
	Set func(c *Config, s string) error
	// StructField is the name of the Config struct field this maps to, used
	// only by the test that checks every field is covered.
	StructField string
	// Advanced marks a setting most users never need. The settings UI hides
	// these behind a toggle; settings.ini is unaffected, so an advanced setting
	// is still written to the file and still editable by hand.
	Advanced bool
}

// Fields lists every user-editable setting, in the order they appear both in
// settings.ini and in the Settings window.
var Fields = []Field{
	{
		Key:         "camera_fps",
		Section:     "camera",
		Desc:        "Webcam capture/detect rate in frames per second.",
		StructField: "CameraFPS",
		Get:         func(c Config) string { return strconv.Itoa(c.CameraFPS) },
		Set:         func(c *Config, s string) error { return setPositiveInt(&c.CameraFPS, "camera_fps", s) },
	},
	{
		Key:     "camera_request_width",
		Section: "camera",
		Desc: "Requested camera resolution width, in pixels. The camera chooses the closest " +
			"supported resolution, so the actual frame size may not match exactly. " +
			"Reduce for less CPU. Raise if QR code scans are unreliable.",
		StructField: "CameraRequestWidth",
		Get:         func(c Config) string { return strconv.Itoa(c.CameraRequestWidth) },
		Set: func(c *Config, s string) error {
			return setPositiveInt(&c.CameraRequestWidth, "camera_request_width", s)
		},
	},
	{
		Key:         "camera_request_height",
		Section:     "camera",
		Desc:        "Requested camera resolution height, in pixels. See camera_request_width.",
		StructField: "CameraRequestHeight",
		Get:         func(c Config) string { return strconv.Itoa(c.CameraRequestHeight) },
		Set: func(c *Config, s string) error {
			return setPositiveInt(&c.CameraRequestHeight, "camera_request_height", s)
		},
	},
	{
		Key:         "qr_scan_cooldown",
		Section:     "camera",
		Desc:        "Time before the same QR code scans again (e.g. 8s, 500ms). Setting this to at least a few seconds helps prevent a student from scanning in and then immediately scanning out because they held the QR code to the camera for too long.",
		StructField: "QRScanCooldown",
		Get:         func(c Config) string { return c.QRScanCooldown.String() },
		Set: func(c *Config, s string) error {
			return setPositiveDuration(&c.QRScanCooldown, "qr_scan_cooldown", "8s", s)
		},
	},
	{
		Key:         "prune_interval",
		Section:     "database",
		Desc:        "Controls how often database pruning is run in the background (e.g. 30m, 1h).",
		StructField: "PruneInterval",
		Advanced:    true,
		Get:         func(c Config) string { return c.PruneInterval.String() },
		Set: func(c *Config, s string) error {
			return setPositiveDuration(&c.PruneInterval, "prune_interval", "30m", s)
		},
	},
	{
		Key:         "prune_batch_size",
		Section:     "database",
		Desc:        "Max number of old database rows pruned per run.",
		StructField: "PruneBatchSize",
		Advanced:    true,
		Get:         func(c Config) string { return strconv.Itoa(c.PruneBatchSize) },
		Set:         func(c *Config, s string) error { return setPositiveInt(&c.PruneBatchSize, "prune_batch_size", s) },
	},
}

// fieldByKey looks up a field by its INI key (case-insensitively).
func fieldByKey(key string) (Field, bool) {
	key = strings.ToLower(key)
	for _, f := range Fields {
		if f.Key == key {
			return f, true
		}
	}
	return Field{}, false
}

// setPositiveInt parses s as a positive integer into dst. Surrounding space is
// trimmed, so a value pasted with stray whitespace still validates.
func setPositiveInt(dst *int, key, s string) error {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || n <= 0 {
		return fmt.Errorf("%s must be a positive integer, got %q", key, s)
	}
	*dst = n
	return nil
}

// setPositiveDuration parses s as a positive Go duration into dst. example is
// shown in the error message to hint at the accepted format.
func setPositiveDuration(dst *time.Duration, key, example, s string) error {
	d, err := time.ParseDuration(strings.TrimSpace(s))
	if err != nil || d <= 0 {
		return fmt.Errorf("%s must be a positive duration (e.g. %s), got %q", key, example, s)
	}
	*dst = d
	return nil
}
