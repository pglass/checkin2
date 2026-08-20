// Package config loads and persists app settings in a small INI file kept
// alongside the database and log file. Missing files are created with defaults.
package config

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"time"
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
	// PruneInterval is how often the background pruner deletes old log rows.
	PruneInterval time.Duration
	// PruneBatchSize is the max number of rows the pruner deletes per wake-up.
	PruneBatchSize int
	// ConfirmScan requires a confirmation pop-up before a scanned QR code
	// checks a student in or out. With it off, a scan is applied immediately.
	ConfirmScan bool
}

// Default returns the built-in default settings.
func Default() Config {
	return Config{
		CameraFPS:           6,
		CameraRequestWidth:  640,
		CameraRequestHeight: 480,
		QRScanCooldown:      8 * time.Second,
		PruneInterval:       5 * time.Minute,
		PruneBatchSize:      100,
		// Confirming by default preserves the behaviour every existing install
		// already has; turning it off is an opt-in to faster unattended scanning.
		ConfirmScan: true,
	}
}

// Load reads the INI file at path. If it does not exist, it is created with
// default values and those defaults are returned. Unknown keys are ignored;
// missing keys keep their default. An invalid value for a known key is an error.
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
		key = strings.TrimSpace(key)
		val = strings.TrimSpace(val)

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
	return cfg, nil
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
		fmt.Fprintf(&b, "%s = %s\n", f.Key, f.Get(cfg))
	}
	return os.WriteFile(path, []byte(b.String()), 0o644)
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
