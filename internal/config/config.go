// Package config loads and persists app settings in a small INI file kept
// alongside the database and log file. Missing files are created with defaults.
package config

import (
	"bufio"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/pglass/checkin/internal/qr"
)

// Config holds user-tunable settings.
type Config struct {
	// CameraFPS is the target capture/detect rate of the webcam loop.
	CameraFPS int
	// CameraRequestWidth and CameraRequestHeight are the capture resolution we
	// ask the webcam for. The driver picks the closest supported resolution, so
	// the actual frame size may differ from what is requested. Lower resolution
	// cuts CPU since QR detection cost scales with pixel count.
	CameraRequestWidth  int
	CameraRequestHeight int
	// QRScanCooldown is how long a scanned code is ignored after a successful
	// scan, so the same student's dialog is not re-triggered immediately.
	QRScanCooldown time.Duration
	// AutoBackupFrequencyDays is how many days old the newest backup may get
	// before one is taken automatically at startup.
	AutoBackupFrequencyDays int
	// BackupDirs are the external directories every backup archive is copied
	// to, after it is written and verified in the internal backup directory
	// (see backup.InternalDir). None by default: an external location is
	// machine-specific, and copying backups somewhere the user never chose
	// would be worse than not copying them.
	BackupDirs []string
	// QRBaseURL is the base that a student's encoded name is appended to when a
	// QR code is generated. It is deliberately absent from the Settings window:
	// changing it invalidates every card already printed against the old base,
	// so it is an install-time choice made in the file, not a routine one.
	QRBaseURL string
}

// Default returns the built-in default settings.
func Default() Config {
	return Config{
		CameraFPS:               6,
		CameraRequestWidth:      640,
		CameraRequestHeight:     480,
		QRScanCooldown:          8 * time.Second,
		AutoBackupFrequencyDays: 2,
		QRBaseURL:               qr.DefaultBaseURL,
	}
}

// legacyBackupDirKey is the single backup directory setting that backup_dirs
// replaced. Load migrates it into BackupDirs; see Load.
const legacyBackupDirKey = "backup_dir"

// Load reads the INI file at path. If it does not exist, it is created with
// default values and those defaults are returned. Unknown keys are ignored;
// missing keys keep their default. An invalid value for a known key is an error.
//
// A List field is read from numbered keys (backup_dirs1, backup_dirs2, ...),
// ordered by number; gaps in the numbering are fine, so a hand-deleted line
// needs no renumbering. Write numbers them 1..N again.
//
// The legacy backup_dir key is migrated: its value is added to BackupDirs and
// the file is rewritten without it, so the migration happens exactly once. If
// it ran on every load instead, a directory the user later removed from the
// list would keep coming back.
func Load(path string) (Config, error) {
	cfg := Default()

	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		if werr := Write(path, cfg); werr != nil {
			return cfg, werr
		}
		return cfg, nil
	}
	if err != nil {
		return cfg, err
	}

	lists := map[string][]listItem{} // List field key -> its numbered values
	var legacySeen bool
	var legacyDir string

	sc := bufio.NewScanner(strings.NewReader(string(data)))
	line := 0
	for sc.Scan() {
		line++
		text := strings.TrimSpace(sc.Text())
		if text == "" || strings.HasPrefix(text, "#") || strings.HasPrefix(text, ";") {
			continue
		}
		if strings.HasPrefix(text, "[") { // section headers: single flat namespace, ignore
			continue
		}
		key, val, ok := strings.Cut(text, "=")
		if !ok {
			return cfg, fmt.Errorf("%s:%d: malformed line %q", path, line, text)
		}
		key = strings.ToLower(strings.TrimSpace(key))
		val = strings.TrimSpace(val)

		if key == legacyBackupDirKey {
			legacySeen = true
			legacyDir = val
			continue
		}
		if f, n, ok := listFieldByKey(key); ok {
			lists[f.Key] = append(lists[f.Key], listItem{n: n, val: val})
			continue
		}
		f, ok := fieldByKey(key)
		if !ok {
			continue // unknown key: ignored, so old files still load
		}
		if err := f.Set(&cfg, val); err != nil {
			return cfg, fmt.Errorf("%s:%d: %w", path, line, err)
		}
	}
	if err := sc.Err(); err != nil {
		return cfg, err
	}

	for _, f := range Fields {
		if !f.List {
			continue
		}
		items := lists[f.Key]
		sort.SliceStable(items, func(i, j int) bool { return items[i].n < items[j].n })
		vals := make([]string, 0, len(items))
		for _, it := range items {
			vals = append(vals, it.val)
		}
		if err := f.SetList(&cfg, vals); err != nil {
			return cfg, fmt.Errorf("%s: %w", path, err)
		}
	}

	if legacySeen {
		// Appended last so directories already in backup_dirs keep their
		// place; SetList drops it again if it is already listed or empty.
		if err := setBackupDirs(&cfg, append(cfg.BackupDirs, legacyDir)); err != nil {
			return cfg, fmt.Errorf("%s: %w", path, err)
		}
		if err := Write(path, cfg); err != nil {
			return cfg, fmt.Errorf("migrate %s to backup_dirs: %w", legacyBackupDirKey, err)
		}
		slog.Info("settings migrated", "from", legacyBackupDirKey, "dir", legacyDir, "path", path)
	}
	return cfg, nil
}

// listItem is one numbered value of a List field as read from the file.
type listItem struct {
	n   int
	val string
}

// Write persists cfg to path in INI format (overwriting any existing file).
// Keys are grouped under their section in Fields order, each preceded by its
// description as a comment. The Settings window saves through this function, so
// a hand-edited file and a UI-saved file have the same shape.
func Write(path string, cfg Config) error {
	var b strings.Builder
	b.WriteString("# checkin settings\n")

	section := ""
	for _, f := range Fields {
		if f.Section != section {
			b.WriteString("\n[" + f.Section + "]\n")
			section = f.Section
		}
		for _, line := range wrapComment(f.Desc, 78) {
			b.WriteString("# " + line + "\n")
		}
		if f.List {
			writeList(&b, f, f.GetList(cfg))
			continue
		}
		fmt.Fprintf(&b, "%s = %s\n", f.Key, f.Get(cfg))
	}
	return os.WriteFile(path, []byte(b.String()), 0o644)
}

// writeList writes a List field's values as numbered keys, 1..N. With no values
// it writes a commented-out example instead, so the file still shows how to add
// one by hand.
func writeList(b *strings.Builder, f Field, vals []string) {
	fmt.Fprintf(b, "# One per line, numbered: %s1, %s2, ...\n", f.Key, f.Key)
	if len(vals) == 0 {
		fmt.Fprintf(b, "# %s1 = %s\n", f.Key, f.ListExample)
		return
	}
	for i, v := range vals {
		fmt.Fprintf(b, "%s%d = %s\n", f.Key, i+1, v)
	}
}

// wrapComment splits s into lines of at most width characters, breaking on
// spaces, so long descriptions do not run off the edge of the settings file.
func wrapComment(s string, width int) []string {
	words := strings.Fields(s)
	if len(words) == 0 {
		return nil
	}
	lines := []string{}
	cur := words[0]
	for _, w := range words[1:] {
		if len(cur)+1+len(w) > width {
			lines = append(lines, cur)
			cur = w
			continue
		}
		cur += " " + w
	}
	return append(lines, cur)
}
