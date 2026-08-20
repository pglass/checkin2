package ui

import (
	"context"
	"fmt"
	"log/slog"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"

	"github.com/pglass/checkin/internal/camera"
	"github.com/pglass/checkin/internal/config"
	"github.com/pglass/checkin/internal/store"
	"github.com/pglass/checkin/internal/version"
)

// App is the top-level GUI controller.
type App struct {
	fyneApp fyne.App
	win     fyne.Window
	store   *store.Store
	ctx     context.Context

	// about supplies the About menu and its windows, shared with the startup
	// window so the menu exists before a Center is open.
	about about

	table     *studentTable
	cam       *camera.Camera
	camCancel context.CancelFunc // stops the current camera; set per device
	camDevice int                // index of the running device

	// cfg is the live settings, seeded at startup and replaced when the
	// Settings window saves. Camera restarts and pruner restarts read from it,
	// so a save takes effect without restarting the app. cfgPath is the
	// settings.ini it is written back to.
	cfg     config.Config
	cfgPath string

	// Camera preview lives in its own window, created on demand.
	preview    *canvas.Image
	previewWin fyne.Window
	resLabel   *canvas.Text // status bar showing the actual resolution

	// qrWin is the Generate QR PDF window; tracked so reopening raises the
	// existing one instead of spawning a duplicate.
	qrWin fyne.Window

	// historyWin is the History window; tracked like qrWin so reopening raises
	// the existing one instead of spawning a duplicate.
	historyWin fyne.Window

	// importWin is the Import window; tracked like qrWin so reopening raises
	// the existing one instead of spawning a duplicate.
	importWin fyne.Window

	// settingsWin is the Settings window; tracked like qrWin so reopening
	// raises the existing one instead of spawning a duplicate.
	settingsWin fyne.Window

	// popupOpen is true while a scan-triggered popup (check-in/out, or the
	// "student not found" Add dialog) is showing. QR scans are ignored while
	// it is set, so a second scan can't stack another popup on top.
	// Only touched on the UI thread.
	popupOpen bool
}

// NewFyneApp creates the Fyne application shared by every window (the startup
// Center selector and, afterwards, the main window). Only one of these exists
// per process, and exactly one Run drives it.
func NewFyneApp() fyne.App {
	fa := app.NewWithID("com.pglass.checkin")
	// On packaged builds (e.g. macOS `fyne package --appVersion`) the version
	// lives in the Fyne app metadata rather than the ldflags var; feed it in so
	// the About dialog and logs report the same value on every platform.
	version.SetMetadataVersion(fa.Metadata().Version)
	slog.Info("resolved app version", "version", version.Resolve())
	// Tight, consistent spacing across every window.
	fa.Settings().SetTheme(newCompactTheme())
	return fa
}

// NewApp builds the main window (menubar + student list) but does not run it.
// centerName is shown in the title bar so the open Center is always visible.
// cfg is the loaded settings and cfgPath the file the Settings window saves to.
func NewApp(ctx context.Context, fa fyne.App, s *store.Store, centerName string, cfg config.Config, cfgPath string) *App {
	win := fa.NewWindow("Check-In — " + centerName)

	a := &App{fyneApp: fa, win: win, store: s, ctx: ctx,
		about:   about{fyneApp: fa, parent: win},
		cfg:     cfg,
		cfgPath: cfgPath,
		// No camera until startCamera picks one; 0 would mean "device 0 running".
		camDevice: deviceNone}
	a.table = newStudentTable(a)

	win.SetMainMenu(a.buildMenu())
	// Main view is the student list only; the camera feed opens in its own window.
	win.SetContent(withWindowMargin(a.table.widget()))
	win.Resize(fyne.NewSize(640, 480))

	a.refresh()
	a.startCamera(ctx)
	return a
}

// Show displays the main window. The caller drives the event loop.
func (a *App) Show() { a.win.Show() }

// Run shows the window and blocks until it is closed.
func (a *App) Run() { a.win.ShowAndRun() }

// Window exposes the main window for dialog parenting.
func (a *App) Window() fyne.Window { return a.win }

// buildMenu constructs the Admin and About menubar menus.
func (a *App) buildMenu() *fyne.MainMenu {
	admin := fyne.NewMenu("Admin",
		fyne.NewMenuItem("Add Student…", a.showAddStudentDialog),
		fyne.NewMenuItem("Import…", a.showImportWindow),
		fyne.NewMenuItem("Generate QR PDF…", a.showGenerateQRDialog),
		fyne.NewMenuItem("History…", a.showHistoryWindow),
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem("Show/Hide Camera", a.toggleCamera),
		fyne.NewMenuItem("Settings…", a.showSettingsWindow),
	)
	return fyne.NewMainMenu(admin, a.about.menu())
}

// refresh reloads rows from the store and repaints the table. Safe to call
// from the UI goroutine only; background callers must wrap in fyne.Do.
func (a *App) refresh() {
	rows, err := loadRows(a.ctx, a.store)
	if err != nil {
		a.showError(err)
		return
	}
	a.table.setRows(rows)
}

func (a *App) showError(err error) {
	dialog.ShowError(err, a.win)
}

// toggleCamera opens the camera feed in its own window, or closes it if already
// open. The first live frame arrives within about one capture frame of opening,
// so the preview starts blank only momentarily.
func (a *App) toggleCamera() {
	if a.previewWin != nil {
		a.previewWin.Close() // triggers SetOnClosed, which clears the fields
		return
	}

	a.preview = newPreview()
	a.resLabel = canvas.NewText("", theme.Color(theme.ColorNameForeground))
	a.resLabel.TextSize = theme.CaptionTextSize()
	if a.cam != nil {
		// Resume frame production; the drainer skips it while nobody is watching.
		a.cam.SetPreviewing(true)
	}
	a.updateResLabel()

	// Device picker across the top; preview fills the rest, with a thin status
	// bar at the bottom showing resolution.
	picker := a.newCameraPicker()
	content := container.NewBorder(picker, a.resLabel, nil, nil, a.preview)

	w := a.fyneApp.NewWindow("Camera")
	w.SetContent(withWindowMargin(content))
	w.Resize(fyne.NewSize(480, 360))
	w.SetOnClosed(func() {
		if a.cam != nil {
			a.cam.SetPreviewing(false)
		}
		a.previewWin = nil
		a.preview = nil
		a.resLabel = nil
	})
	a.previewWin = w
	w.Show()
}

// updateResLabel writes the actual capture resolution into the camera window's
// status bar. Reads "pending" until the first frame arrives.
func (a *App) updateResLabel() {
	if a.resLabel == nil {
		return
	}
	// No camera running (stopped, or none connected).
	if a.cam == nil {
		a.resLabel.Text = "camera off"
		a.resLabel.Refresh()
		return
	}
	act := a.cam.Actual()
	text := "pending"
	if act.Width > 0 && act.Height > 0 {
		text = fmt.Sprintf("%d×%d", act.Width, act.Height)
	}
	a.resLabel.Text = text
	a.resLabel.Refresh()
}
