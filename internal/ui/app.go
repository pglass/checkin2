package ui

import (
	"context"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
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
	preview *canvas.Image
	cam     *camera.Camera
}

// NewApp builds the main window (menubar + student list) but does not run it.
func NewApp(ctx context.Context, s *store.Store) *App {
	fa := app.NewWithID("com.pglass.checkin")
	win := fa.NewWindow("Check-In")

	a := &App{fyneApp: fa, win: win, store: s, ctx: ctx}
	a.table = newStudentTable(a)
	a.preview = newPreview()
	a.preview.Hide()

	win.SetMainMenu(a.buildMenu())
	// Camera preview sits to the right of the list (hidden until toggled).
	win.SetContent(container.NewBorder(nil, nil, nil, a.preview, a.table.widget()))
	win.Resize(fyne.NewSize(760, 480))

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

// toggleCamera shows or hides the camera preview pane.
func (a *App) toggleCamera() {
	if a.preview.Visible() {
		a.preview.Hide()
	} else {
		a.preview.Show()
	}
}
