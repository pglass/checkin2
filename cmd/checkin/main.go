package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/pglass/checkin/internal/center"
	"github.com/pglass/checkin/internal/config"
	"github.com/pglass/checkin/internal/logging"
	"github.com/pglass/checkin/internal/store"
	"github.com/pglass/checkin/internal/ui"
	"github.com/pglass/checkin/internal/version"
)

func main() {
	logFileFlag := flag.String("log-file", "",
		`directory for the rotating log file (default: inside the open Center's directory); "-" logs to stdout only`)
	logLevelFlag := flag.String("log-level", "INFO", "log level: DEBUG, INFO, WARN, or ERROR")
	centerFlag := flag.String("center", "", "open this Center directly, skipping the selection window (created if absent)")
	dbPathFlag := flag.String("db-path", "", "open this database file directly, skipping the selection window")
	versionFlag := flag.Bool("version", false, "print the program version and exit")
	flag.Parse()

	if *versionFlag {
		fmt.Println(version.Version)
		os.Exit(0)
	}

	level, err := logging.ParseLevel(*logLevelFlag)
	if err != nil {
		log.Fatalf("%v", err)
	}

	appDir, err := center.AppDir()
	if err != nil {
		log.Fatalf("resolve app directory: %v", err)
	}

	// Until a Center is open there is no per-Center log directory, so selection
	// logs to stdout. Once the Center is chosen, logging is reconfigured to that
	// Center's directory (unless -log-file overrides the destination).
	if _, err := logging.Setup("-", level); err != nil {
		log.Fatalf("setup logging: %v", err)
	}
	slog.Info("starting checkin", "version", version.Version, "app_dir", appDir)

	// Settings are shared by all Centers and live at the top of the app dir.
	cfgPath := center.SettingsPath(appDir)
	cfg, err := config.Load(cfgPath)
	if err != nil {
		slog.Error("load settings", "path", absPath(cfgPath), "error", err)
		log.Fatalf("load settings: %v", err)
	}
	slog.Info("settings loaded", "path", absPath(cfgPath))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	fa := ui.NewFyneApp()

	// The log file and database outlive run(): they are opened when a Center is
	// selected and must stay open for as long as the UI is running, so they are
	// closed here after fa.Run() returns rather than deferred inside run().
	var (
		logCloser io.Closer
		openStore *store.Store
	)
	defer func() {
		if openStore != nil {
			openStore.Close()
		}
		if logCloser != nil {
			logCloser.Close()
		}
	}()

	// run opens the Center's log file and database, then shows the main window.
	// It does not block: the caller drives the event loop via fa.Run().
	run := func(c center.Center, dbPath string) {
		logDest := *logFileFlag
		if logDest == "" {
			logDest = c.LogDir()
		}
		closer, err := logging.Setup(logDest, level)
		if err != nil {
			log.Fatalf("setup logging: %v", err)
		}
		logCloser = closer // nil when logging to stdout
		slog.Info("center opened", "name", c.Name, "dir", c.Dir)

		s, err := store.Open(dbPath)
		if err != nil {
			slog.Error("open database", "path", absPath(dbPath), "error", err)
			log.Fatalf("open db: %v", err)
		}
		openStore = s
		slog.Info("database opened", "path", absPath(dbPath))

		app := ui.NewApp(ctx, fa, s, c.Name, cfg.CameraFPS, cfg.CameraRequestWidth, cfg.CameraRequestHeight, cfg.QRScanCooldown)

		// Start background pruning after the UI is constructed; first pass fires
		// after the configured interval so startup stays fast. The pruner only
		// touches the open Center's database.
		s.StartPruner(ctx, cfg.PruneInterval, cfg.PruneBatchSize)

		app.Show()
	}

	// A direct -db-path or -center skips the selection window entirely.
	switch {
	case *dbPathFlag != "":
		// Escape hatch for development: the database's parent directory stands in
		// for the Center, so logs land next to the file.
		dir := filepath.Dir(*dbPathFlag)
		c := center.Center{Name: filepath.Base(dir), Dir: dir}
		run(c, *dbPathFlag)
	case *centerFlag != "":
		c, err := center.Create(appDir, *centerFlag)
		if errors.Is(err, center.ErrExists) {
			c = center.Center{Name: *centerFlag, Dir: filepath.Join(appDir, *centerFlag)}
		} else if err != nil {
			log.Fatalf("open center %q: %v", *centerFlag, err)
		}
		run(c, c.DBPath())
	default:
		ui.ShowStartup(fa, appDir, func(c center.Center) { run(c, c.DBPath()) })
	}

	fa.Run()
}

// absPath returns the absolute form of p for logging, falling back to p itself
// if it cannot be resolved.
func absPath(p string) string {
	if abs, err := filepath.Abs(p); err == nil {
		return abs
	}
	return p
}
