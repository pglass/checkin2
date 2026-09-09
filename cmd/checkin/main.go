package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"os"
	"path/filepath"

	"fyne.io/fyne/v2"

	"github.com/pglass/checkin/internal/center"
	"github.com/pglass/checkin/internal/config"
	"github.com/pglass/checkin/internal/logging"
	"github.com/pglass/checkin/internal/store"
	"github.com/pglass/checkin/internal/ui"
	"github.com/pglass/checkin/internal/version"
)

func main() {
	logFileFlag := flag.String("log-file", "",
		`directory for the rotating log file (default: the application directory); "-" logs to stdout only`)
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

	// One log file for the whole run, in the app directory, configured before
	// anything else happens. Work done with no Center open -- choosing one,
	// changing settings, taking or restoring a backup -- is logged like
	// everything else rather than being lost on a packaged build with no
	// terminal attached.
	logDest := *logFileFlag
	if logDest == "" {
		logDest = appDir
	}
	logCloser, err := logging.Setup(logDest, level)
	if err != nil {
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

	// The database outlives run(): it is opened when a Center is selected and
	// must stay open for as long as the UI is running, so it is closed here
	// after fa.Run() returns rather than deferred inside run(). The log file is
	// open from startup and closed alongside it.
	var openStore *store.Store
	defer func() {
		if openStore != nil {
			openStore.Close()
		}
		if logCloser != nil {
			logCloser.Close()
		}
	}()

	// run opens the Center's database, then shows the main view.
	// It does not block: the caller drives the event loop via fa.Run(). It
	// returns store.ErrAlreadyOpen (and opens nothing) when the Center is already
	// held by another instance, so the caller can report that and stay running;
	// any other failure is fatal.
	//
	// win is the window to build the main view in. The selection window passes
	// its own window so it becomes the main window in place -- never closing a
	// window from inside the click that asked for it, which crashes the glfw
	// driver. A nil win means there is no selection window (-center / -db-path),
	// so one is created.
	run := func(c center.Center, dbPath string, win fyne.Window) error {
		// Open takes the one-process-per-Center lock before anything else, so an
		// instance that loses the race has changed nothing.
		s, err := store.Open(dbPath)
		if errors.Is(err, store.ErrAlreadyOpen) {
			slog.Warn("center already open in another instance", "name", c.Name, "dir", c.Dir)
			return err
		}
		if err != nil {
			slog.Error("open database", "path", absPath(dbPath), "error", err)
			log.Fatalf("open db: %v", err)
		}
		openStore = s

		// The log file does not change with the Center; the Center becomes a
		// field on every later line instead.
		logging.SetCenter(c.Name)
		// The name is already on every line from here on, so only the directory
		// is worth adding.
		slog.Info("center opened", "dir", c.Dir)
		slog.Info("database opened", "path", absPath(dbPath))

		var app *ui.App
		if win != nil {
			app = ui.NewAppInWindow(ctx, fa, win, s, c.Name, cfg, cfgPath)
		} else {
			app = ui.NewApp(ctx, fa, s, c.Name, cfg, cfgPath)
		}

		app.Show()
		return nil
	}

	// A direct -db-path or -center skips the selection window entirely. With no
	// selection window to fall back to, an already-open Center shows a standalone
	// message window instead.
	switch {
	case *dbPathFlag != "":
		// Escape hatch for development: the database's parent directory stands in
		// for the Center, which names it in the log.
		dir := filepath.Dir(*dbPathFlag)
		c := center.Center{Name: filepath.Base(dir), Dir: dir}
		if err := run(c, *dbPathFlag, nil); errors.Is(err, store.ErrAlreadyOpen) {
			ui.ShowFatalError(fa, alreadyOpenMessage(c.Name))
		}
	case *centerFlag != "":
		c, err := center.Create(appDir, *centerFlag)
		if errors.Is(err, center.ErrExists) {
			c = center.Center{Name: *centerFlag, Dir: filepath.Join(appDir, *centerFlag)}
		} else if err != nil {
			log.Fatalf("open center %q: %v", *centerFlag, err)
		}
		if err := run(c, c.DBPath(), nil); errors.Is(err, store.ErrAlreadyOpen) {
			ui.ShowFatalError(fa, alreadyOpenMessage(c.Name))
		}
	default:
		ui.ShowStartup(fa, appDir, cfg, cfgPath, func(c center.Center, win fyne.Window) error {
			return run(c, c.DBPath(), win)
		})
	}

	fa.Run()
}

// alreadyOpenMessage is the user-facing text shown when a Center chosen via
// -center or -db-path is already open in another instance.
func alreadyOpenMessage(name string) string {
	return fmt.Sprintf("The Center %q is already open in another window.\n\n"+
		"Close that window first, or choose a different Center.", name)
}

// absPath returns the absolute form of p for logging, falling back to p itself
// if it cannot be resolved.
func absPath(p string) string {
	if abs, err := filepath.Abs(p); err == nil {
		return abs
	}
	return p
}
