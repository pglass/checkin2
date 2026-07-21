// Package config loads and persists app settings in a small INI file kept
// alongside the database and log file. Missing files are created with defaults.
package config

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
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

		switch strings.ToLower(key) {
		case "camera_fps":
			n, err := strconv.Atoi(val)
			if err != nil || n <= 0 {
				return cfg, fmt.Errorf("%s:%d: camera_fps must be a positive integer, got %q", path, line, val)
			}
			cfg.CameraFPS = n
		case "camera_request_width":
			n, err := strconv.Atoi(val)
			if err != nil || n <= 0 {
				return cfg, fmt.Errorf("%s:%d: camera_request_width must be a positive integer, got %q", path, line, val)
			}
			cfg.CameraRequestWidth = n
		case "camera_request_height":
			n, err := strconv.Atoi(val)
			if err != nil || n <= 0 {
				return cfg, fmt.Errorf("%s:%d: camera_request_height must be a positive integer, got %q", path, line, val)
			}
			cfg.CameraRequestHeight = n
		case "qr_scan_cooldown":
			d, err := time.ParseDuration(val)
			if err != nil || d <= 0 {
				return cfg, fmt.Errorf("%s:%d: qr_scan_cooldown must be a positive duration (e.g. 8s), got %q", path, line, val)
			}
			cfg.QRScanCooldown = d
		case "prune_interval":
			d, err := time.ParseDuration(val)
			if err != nil || d <= 0 {
				return cfg, fmt.Errorf("%s:%d: prune_interval must be a positive duration (e.g. 30m), got %q", path, line, val)
			}
			cfg.PruneInterval = d
		case "prune_batch_size":
			n, err := strconv.Atoi(val)
			if err != nil || n <= 0 {
				return cfg, fmt.Errorf("%s:%d: prune_batch_size must be a positive integer, got %q", path, line, val)
			}
			cfg.PruneBatchSize = n
		}
	}
	if err := sc.Err(); err != nil {
		return cfg, err
	}
	return cfg, nil
}

// Write persists cfg to path in INI format (overwriting any existing file).
func Write(path string, cfg Config) error {
	var b strings.Builder
	b.WriteString("# checkin settings\n\n")
	b.WriteString("[camera]\n")
	b.WriteString("# Webcam capture/detect rate in frames per second.\n")
	fmt.Fprintf(&b, "camera_fps = %d\n", cfg.CameraFPS)
	b.WriteString("# Requested capture resolution. The camera chooses the closest supported\n")
	b.WriteString("# resolution, so the actual frame size may not match exactly. Lower = less CPU.\n")
	fmt.Fprintf(&b, "camera_request_width = %d\n", cfg.CameraRequestWidth)
	fmt.Fprintf(&b, "camera_request_height = %d\n", cfg.CameraRequestHeight)
	b.WriteString("# How long a scanned QR code is ignored after a scan (e.g. 8s, 500ms).\n")
	fmt.Fprintf(&b, "qr_scan_cooldown = %s\n", cfg.QRScanCooldown)
	b.WriteString("\n[database]\n")
	b.WriteString("# How often old log rows are pruned in the background (e.g. 30m, 1h).\n")
	fmt.Fprintf(&b, "prune_interval = %s\n", cfg.PruneInterval)
	b.WriteString("# Max number of rows deleted per prune wake-up.\n")
	fmt.Fprintf(&b, "prune_batch_size = %d\n", cfg.PruneBatchSize)
	return os.WriteFile(path, []byte(b.String()), 0o644)
}
