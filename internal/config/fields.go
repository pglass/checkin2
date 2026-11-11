package config

import (
	"fmt"
	"path/filepath"
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
	// FreeText marks a setting whose value is arbitrary text (a file path, for
	// example) rather than a number, duration, or boolean. Such a field has
	// nothing to validate -- including "" , which is how an optional setting is
	// turned off -- so Set always succeeds. The settings UI still renders it as
	// a text entry; this only tells the validation test not to expect errors.
	FreeText bool
	// Hidden keeps a setting out of the Settings window while leaving it in
	// settings.ini. It is for install-time values that should not be changed
	// casually: the entry still lives in this table, so the file round-trips it
	// and the coverage test still sees the Config field covered.
	Hidden bool
	// Directory marks a setting whose value is a directory path. The settings
	// UI puts a Browse… button next to the entry so the folder can be picked
	// rather than typed. Typing remains available: a path on a drive that is not
	// mounted right now cannot be browsed to, but is still worth saving.
	Directory bool

	// List marks a setting holding any number of values. Key is then a prefix:
	// settings.ini holds Key1, Key2, ... (see Load and Write), and the settings
	// UI shows one entry per value with Remove buttons and an Add button. A List
	// field uses GetList and SetList in place of Get and Set.
	List bool
	// GetList returns a List field's values.
	GetList func(c Config) []string
	// SetList validates and stores a List field's values. Like Set, its error
	// is shown to the user verbatim.
	SetList func(c *Config, vals []string) error
	// ListExample is the placeholder value in the commented-out example line
	// Write emits for an empty List field.
	ListExample string
	// AddLabel is the settings UI's button for adding a List value.
	AddLabel string
	// RemovePrompt is the confirmation shown before a List value is removed in
	// the settings UI. It is a format string taking the value being removed.
	RemovePrompt string
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
		Key:          KeyBackupDirs,
		Section:      "backup",
		Desc:         "Each new backup archive is copied to every one of these directories.",
		StructField:  "BackupDirs",
		FreeText:     true,
		Directory:    true,
		List:         true,
		GetList:      func(c Config) []string { return c.BackupDirs },
		SetList:      setBackupDirs,
		ListExample:  "/path/to/backups",
		AddLabel:     "Add backup directory",
		RemovePrompt: "This will remove %s from the list of backup directories. (No existing backups will be deleted)",
	},
	{
		Key:     "qr_base_url",
		Section: "qr",
		Desc: "Base URL that generated QR codes point at. The student's name is base64url-encoded " +
			"and appended to this, so a scanned code opens a page for that student. Changing this " +
			"does not stop existing printed cards from scanning (only the final path segment is " +
			"read), but codes generated afterwards carry the new base.",
		StructField: "QRBaseURL",
		FreeText:    true,
		Hidden:      true,
		Get:         func(c Config) string { return c.QRBaseURL },
		Set: func(c *Config, s string) error {
			// Any string is accepted: this is not validated as a URL because a
			// self-hosted or intranet base is legitimate, and rejecting an
			// unusual-looking one at load time would make the app unstartable
			// over a setting it can otherwise ignore.
			c.QRBaseURL = strings.TrimSpace(s)
			return nil
		},
	},
}

// KeyBackupDirs is the key prefix of the external backup directories list.
const KeyBackupDirs = "backup_dirs"

// fieldByKey looks up a single-valued field by its INI key
// (case-insensitively). List fields are found with listFieldByKey.
func fieldByKey(key string) (Field, bool) {
	key = strings.ToLower(key)
	for _, f := range Fields {
		if f.Key == key && !f.List {
			return f, true
		}
	}
	return Field{}, false
}

// listFieldByKey matches key against the List fields' numbered keys, returning
// the field and the number: "backup_dirs3" is item 3 of backup_dirs. A bare
// "backup_dirs" is accepted as item 0, so a hand-written single entry without a
// number still loads.
func listFieldByKey(key string) (Field, int, bool) {
	key = strings.ToLower(key)
	for _, f := range Fields {
		if !f.List || !strings.HasPrefix(key, f.Key) {
			continue
		}
		suffix := key[len(f.Key):]
		if suffix == "" {
			return f, 0, true
		}
		n, err := strconv.Atoi(suffix)
		if err != nil || n < 0 || strconv.Itoa(n) != suffix {
			continue
		}
		return f, n, true
	}
	return Field{}, 0, false
}

// setBackupDirs stores the backup directories, trimmed, with empty values and
// repeats of the same directory dropped. Any path is accepted, including one
// that does not exist yet: validating existence here would reject a removable
// drive that is simply not plugged in at the moment.
func setBackupDirs(c *Config, vals []string) error {
	var out []string // nil when empty, matching Default
	seen := map[string]bool{}
	for _, v := range vals {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		key := filepath.Clean(v)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, v)
	}
	c.BackupDirs = out
	return nil
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

// setBool parses s as a boolean into dst, accepting the forms strconv.ParseBool
// does (true/false/1/0/t/f, any case). Surrounding space is trimmed, so a value
// pasted with stray whitespace still validates.
func setBool(dst *bool, key, s string) error {
	b, err := strconv.ParseBool(strings.TrimSpace(s))
	if err != nil {
		return fmt.Errorf("%s must be true or false, got %q", key, s)
	}
	*dst = b
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
