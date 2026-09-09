// Package logging configures the application's slog logger, writing either to
// stdout or to a rotating log file in the application directory.
package logging

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/natefinch/lumberjack.v2"
)

// LogFileName is the log file created in the application directory.
const LogFileName = "checkin.log"

// Rotation settings keep log files from growing without bound.
const (
	maxSizeMB  = 10 // rotate after a file reaches this size
	maxBackups = 5  // keep at most this many rotated files
	maxAgeDays = 90 // delete rotated files older than this
)

// ParseLevel converts a level name (case-insensitive) to a slog.Level.
// Recognizes DEBUG, INFO, WARN/WARNING, ERROR. Defaults to INFO on unknown input.
func ParseLevel(name string) (slog.Level, error) {
	switch strings.ToUpper(strings.TrimSpace(name)) {
	case "", "INFO":
		return slog.LevelInfo, nil
	case "DEBUG":
		return slog.LevelDebug, nil
	case "WARN", "WARNING":
		return slog.LevelWarn, nil
	case "ERROR":
		return slog.LevelError, nil
	default:
		return slog.LevelInfo, fmt.Errorf("unknown log level %q", name)
	}
}

// Setup configures slog as the default logger and returns an io.Closer for any
// underlying file (nil when logging to stdout).
//
// logFile == "-" logs to stdout with no rotation. Any other value is treated as
// a directory in which the rotating log file is created; the application
// directory is what the app passes, so one log covers the whole run.
//
// Setup is called once, at startup, before a Center is chosen. Work done with
// no Center open -- choosing one, changing settings, taking or restoring a
// backup -- is logged to the same file as everything else. Once a Center is
// open, SetCenter adds it as a field rather than redirecting the log.
func Setup(logFile string, level slog.Level) (io.Closer, error) {
	var writer io.Writer
	var closer io.Closer

	if logFile == "-" {
		writer = os.Stdout
	} else {
		if err := os.MkdirAll(logFile, 0o755); err != nil {
			return nil, err
		}
		lj := &lumberjack.Logger{
			Filename:   filepath.Join(logFile, LogFileName),
			MaxSize:    maxSizeMB,
			MaxBackups: maxBackups,
			MaxAge:     maxAgeDays,
			Compress:   true,
		}
		writer = lj
		closer = lj
	}

	handler := slog.NewTextHandler(writer, &slog.HandlerOptions{Level: level})
	base = slog.New(handler)
	slog.SetDefault(base)
	return closer, nil
}

// base is the logger Setup installed, without any Center attached. SetCenter
// derives from it rather than from the current default, so switching Centers
// cannot stack one "center" field on top of another.
var base *slog.Logger

// SetCenter makes name appear as a "center" field on every later log line, for
// telling one Center's activity from another in a shared log file. An empty
// name clears it, restoring the logger Setup installed.
//
// It is a no-op before Setup runs, so a caller that logs before configuring
// still writes to the standard-library default rather than panicking.
func SetCenter(name string) {
	if base == nil {
		return
	}
	if name == "" {
		slog.SetDefault(base)
		return
	}
	slog.SetDefault(base.With("center", name))
}
