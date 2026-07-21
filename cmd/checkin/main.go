package main

import (
	"context"
	"flag"
	"log"
	"log/slog"
	"path/filepath"

	"github.com/pglass/checkin/internal/config"
	"github.com/pglass/checkin/internal/logging"
	"github.com/pglass/checkin/internal/store"
	"github.com/pglass/checkin/internal/ui"
)

func main() {
	logFileFlag := flag.String("log-file", "",
		`directory for the rotating log file (default: alongside the database); "-" logs to stdout only`)
	logLevelFlag := flag.String("log-level", "INFO", "log level: DEBUG, INFO, WARN, or ERROR")
	dbPathFlag := flag.String("db-path", "", "override the SQLite database path (default: per-OS app directory)")
	flag.Parse()

	level, err := logging.ParseLevel(*logLevelFlag)
	if err != nil {
		log.Fatalf("%v", err)
	}

	path := *dbPathFlag
	if path == "" {
		path, err = store.DefaultDBPath()
		if err != nil {
			log.Fatalf("resolve db path: %v", err)
		}
	}

	// Default log destination is the database's directory; "-" means stdout.
	logDest := *logFileFlag
	if logDest == "" {
		logDest = filepath.Dir(path)
	}
	closer, err := logging.Setup(logDest, level)
	if err != nil {
		log.Fatalf("setup logging: %v", err)
	}
	if closer != nil {
		defer closer.Close()
	}

	// Settings live alongside the database; created with defaults if absent.
	cfgPath := filepath.Join(filepath.Dir(path), "settings.ini")
	cfg, err := config.Load(cfgPath)
	if err != nil {
		slog.Error("load settings", "path", absPath(cfgPath), "error", err)
		log.Fatalf("load settings: %v", err)
	}
	slog.Info("settings loaded", "path", absPath(cfgPath))

	s, err := store.Open(path)
	if err != nil {
		slog.Error("open database", "path", absPath(path), "error", err)
		log.Fatalf("open db: %v", err)
	}
	defer s.Close()
	slog.Info("database opened", "path", absPath(path))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	app := ui.NewApp(ctx, s, cfg.CameraFPS, cfg.CameraRequestWidth, cfg.CameraRequestHeight, cfg.QRScanCooldown)

	// Start background pruning after the UI is constructed; first pass fires
	// after the configured interval so startup stays fast.
	s.StartPruner(ctx, cfg.PruneInterval, cfg.PruneBatchSize)

	app.Run()
}

// absPath returns the absolute form of p for logging, falling back to p itself
// if it cannot be resolved.
func absPath(p string) string {
	if abs, err := filepath.Abs(p); err == nil {
		return abs
	}
	return p
}
