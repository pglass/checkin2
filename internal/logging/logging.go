// Package logging configures the application's slog logger, writing either to
// stdout or to a rotating log file next to the database.
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

// LogFileName is the default log file, created alongside the database.
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
// a directory in which the default rotating log file is created; pass the
// database's directory to co-locate the log with the DB.
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
	slog.SetDefault(slog.New(handler))
	return closer, nil
}
