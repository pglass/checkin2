package ui

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"

	"github.com/pglass/checkin/internal/camera"
	"github.com/pglass/checkin/internal/store"
	"github.com/pglass/checkin/internal/version"
)

// App is the top-level GUI controller.
type App struct {
	fyneApp fyne.App
	win     fyne.Window
	store   *store.Store
	ctx     context.Context

	table           *studentTable
	cam             *camera.Camera
	cameraFPS       int
	cameraReqWidth  int
	cameraReqHeight int
	qrScanCooldown  time.Duration

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

	// licensesWin is the About -> Licenses window; tracked like qrWin so
	// reopening raises the existing one instead of spawning a duplicate.
	licensesWin fyne.Window
}

// NewApp builds the main window (menubar + student list) but does not run it.
func NewApp(ctx context.Context, s *store.Store, cameraFPS, cameraReqWidth, cameraReqHeight int, qrScanCooldown time.Duration) *App {
	fa := app.NewWithID("com.pglass.checkin")
	// On packaged builds (e.g. macOS `fyne package --appVersion`) the version
	// lives in the Fyne app metadata rather than the ldflags var; feed it in so
	// the About dialog and logs report the same value on every platform.
	version.SetMetadataVersion(fa.Metadata().Version)
	slog.Info("resolved app version", "version", version.Resolve())
	// Tight, consistent spacing across every window.
	fa.Settings().SetTheme(newCompactTheme())
	win := fa.NewWindow("Check-In")

	a := &App{fyneApp: fa, win: win, store: s, ctx: ctx,
		cameraFPS: cameraFPS, cameraReqWidth: cameraReqWidth, cameraReqHeight: cameraReqHeight,
		qrScanCooldown: qrScanCooldown}
	a.table = newStudentTable(a)

	win.SetMainMenu(a.buildMenu())
	// Main view is the student list only; the camera feed opens in its own window.
	win.SetContent(withWindowMargin(a.table.widget()))
	win.Resize(fyne.NewSize(640, 480))

	a.refresh()
	a.startCamera(ctx)
	return a
}

// Run shows the window and blocks until it is closed.
func (a *App) Run() { a.win.ShowAndRun() }

// Window exposes the main window for dialog parenting.
func (a *App) Window() fyne.Window { return a.win }

// buildMenu constructs the Admin and About menubar menus.
func (a *App) buildMenu() *fyne.MainMenu {
	admin := fyne.NewMenu("Admin",
		fyne.NewMenuItem("Add Student…", a.showAddStudentDialog),
		fyne.NewMenuItem("Generate QR PDF…", a.showGenerateQRDialog),
		fyne.NewMenuItem("History…", a.showHistoryWindow),
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem("Show/Hide Camera", a.toggleCamera),
	)
	about := fyne.NewMenu("About",
		fyne.NewMenuItem("Version…", a.showVersionDialog),
		fyne.NewMenuItem("Licenses…", a.showLicensesWindow),
	)
	return fyne.NewMainMenu(admin, about)
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
// open. The window paints the latest cached frame immediately on open so there
// is no wait for the next frame to arrive.
func (a *App) toggleCamera() {
	if a.previewWin != nil {
		a.previewWin.Close() // triggers SetOnClosed, which clears the fields
		return
	}

	a.preview = newPreview()
	a.resLabel = canvas.NewText("", theme.Color(theme.ColorNameForeground))
	a.resLabel.TextSize = theme.CaptionTextSize()
	if a.cam != nil {
		// Resume frame production; the loop skips it while nobody is watching.
		a.cam.SetPreviewing(true)
		if f := a.cam.LatestFrame(); f != nil {
			a.preview.Image = f
			a.preview.Refresh()
		}
	}
	a.updateResLabel()

	// Preview fills the window; a thin status bar at the bottom shows resolution.
	content := container.NewBorder(nil, a.resLabel, nil, nil, a.preview)

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
	if a.resLabel == nil || a.cam == nil {
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
