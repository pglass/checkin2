package ui

import (
	"context"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/dialog"

	"github.com/pglass/checkin/internal/camera"
	"github.com/pglass/checkin/internal/store"
)

// App is the top-level GUI controller.
type App struct {
	fyneApp fyne.App
	win     fyne.Window
	store   *store.Store
	ctx     context.Context

	table   *studentTable
	cam     *camera.Camera

	// Camera preview lives in its own window, created on demand.
	preview    *canvas.Image
	previewWin fyne.Window
}

// NewApp builds the main window (menubar + student list) but does not run it.
func NewApp(ctx context.Context, s *store.Store) *App {
	fa := app.NewWithID("com.pglass.checkin")
	win := fa.NewWindow("Check-In")

	a := &App{fyneApp: fa, win: win, store: s, ctx: ctx}
	a.table = newStudentTable(a)

	win.SetMainMenu(a.buildMenu())
	// Main view is the student list only; the camera feed opens in its own window.
	win.SetContent(a.table.widget())
	win.Resize(fyne.NewSize(640, 480))

	a.refresh()
	a.startCamera(ctx)
	return a
}

// Run shows the window and blocks until it is closed.
func (a *App) Run() { a.win.ShowAndRun() }

// Window exposes the main window for dialog parenting.
func (a *App) Window() fyne.Window { return a.win }

// buildMenu constructs the Admin menubar.
func (a *App) buildMenu() *fyne.MainMenu {
	admin := fyne.NewMenu("Admin",
		fyne.NewMenuItem("Add Student…", a.showAddStudentDialog),
		fyne.NewMenuItem("Generate QR PDF…", a.showGenerateQRDialog),
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem("Show/Hide Camera", a.toggleCamera),
	)
	return fyne.NewMainMenu(admin)
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
	if a.cam != nil {
		if f := a.cam.LatestFrame(); f != nil {
			a.preview.Image = f
			a.preview.Refresh()
		}
	}

	w := a.fyneApp.NewWindow("Camera")
	w.SetContent(a.preview)
	w.Resize(fyne.NewSize(480, 360))
	w.SetOnClosed(func() {
		a.previewWin = nil
		a.preview = nil
	})
	a.previewWin = w
	w.Show()
}
