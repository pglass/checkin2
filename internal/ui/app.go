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

	// about supplies the About window, shared with the startup window so the
	// version and license information is reachable before a Center is open.
	about about

	table     *studentTable
	status    *statusBar
	feedback  *feedbackBar
	cam       *camera.Camera
	camCancel context.CancelFunc // stops the current camera; set per device
	camDevice int                // index of the running device
	// camMenuItems maps a device index (deviceNone for "off") to its Camera >
	// Select Camera item, so the checkmark can be moved without rebuilding the
	// menubar.
	camMenuItems map[int]*fyne.MenuItem

	// cfg is the live settings, seeded at startup and replaced when the
	// Settings window saves. Camera restarts read from it, so a save takes
	// effect without restarting the app. cfgPath is the settings.ini it is
	// written back to.
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

	// kiosk is true while the kiosk view is showing: the student list is
	// replaced by a sign-in message and the menubar holds only "Leave Kiosk
	// Mode". Toggled from the File menu; never persisted.
	kiosk bool

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

// NewApp creates a new window and builds the main view in it. Used by the
// -center / -db-path flags, which skip the Center selection window entirely so
// there is no existing window to take over.
func NewApp(ctx context.Context, fa fyne.App, s *store.Store, centerName string, cfg config.Config, cfgPath string) *App {
	return NewAppInWindow(ctx, fa, fa.NewWindow(""), s, centerName, cfg, cfgPath)
}

// NewAppInWindow builds the main view (menubar + student list) inside win,
// replacing whatever it was showing, but does not run it. centerName is shown
// in the title bar so the open Center is always visible. cfg is the loaded
// settings and cfgPath the file the Settings window saves to.
//
// Taking over an existing window is what lets the Center selection window
// become the main window in place. Closing a window from inside the click that
// asked for it destroys the GLFW handle while fyne's processMouseClicked is
// still using it, which panics in the driver -- reusing the window means there
// is no window destruction on that path at all.
func NewAppInWindow(ctx context.Context, fa fyne.App, win fyne.Window, s *store.Store, centerName string, cfg config.Config, cfgPath string) *App {
	win.SetTitle("Check-In — " + centerName)

	a := &App{fyneApp: fa, win: win, store: s, ctx: ctx,
		about:   about{fyneApp: fa, parent: win},
		cfg:     cfg,
		cfgPath: cfgPath,
		// No camera until startCamera picks one; 0 would mean "device 0 running".
		camDevice: deviceNone}
	a.table = newStudentTable(a)
	a.status = newStatusBar()
	a.feedback = newFeedbackBar()

	win.SetMainMenu(a.buildMenu())
	// Student list, then the scan feedback bar, then the status bar along the
	// bottom; the camera feed opens in its own window.
	a.setMainContent()
	win.Resize(fyne.NewSize(640, 480))
	win.CenterOnScreen()
	// The selection window installed a SetOnClosed that quits the app when no
	// Center was chosen. This window is now the main window, so closing it
	// should quit unconditionally -- and the old handler's "did the user pick a
	// Center" logic no longer applies.
	win.SetOnClosed(func() { fa.Quit() })

	a.refresh()
	a.startStatusClock(ctx)
	a.startCamera(ctx)
	// The menu was built before a device was chosen, so its checkmark still says
	// "off"; move it now that startCamera has picked one.
	a.markCameraMenuItem()
	return a
}

// Show displays the main window. The caller drives the event loop.
func (a *App) Show() { a.win.Show() }

// Run shows the window and blocks until it is closed.
func (a *App) Run() { a.win.ShowAndRun() }

// Window exposes the main window for dialog parenting.
func (a *App) Window() fyne.Window { return a.win }

// buildMenu constructs the File and Camera menubar menus. About sits at the
// bottom of File, after a separator, matching the startup window's menu.
//
// Kiosk mode gets its own cut-down menubar instead: one item to leave again,
// and no Camera menu, so nothing else is reachable from the kiosk screen.
func (a *App) buildMenu() *fyne.MainMenu {
	if a.kiosk {
		return fyne.NewMainMenu(fyne.NewMenu("File",
			fyne.NewMenuItem("Leave Kiosk Mode", a.leaveKiosk),
		))
	}
	file := fyne.NewMenu("File",
		fyne.NewMenuItem("Add Student…", a.showAddStudentDialog),
		fyne.NewMenuItem("Import…", a.showImportWindow),
		fyne.NewMenuItem("Generate QR PDF…", a.showGenerateQRDialog),
		fyne.NewMenuItem("History…", a.showHistoryWindow),
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem("Enter Kiosk Mode", a.enterKiosk),
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem("Settings…", a.showSettingsWindow),
		fyne.NewMenuItemSeparator(),
		a.about.menuItem(),
	)
	return fyne.NewMainMenu(file, a.cameraMenu())
}

// cameraMenu builds the Camera menu: the preview window, then the off switch
// and every connected device, all at the top level.
//
// The device items are retained in camMenuItems so a selection can move the
// checkmark by mutating them in place. Rebuilding the menubar from a menu
// item's own callback is NOT safe: fyne's SetMainMenu tears down and rebuilds
// the menu widgets while the click that triggered it is still being dispatched,
// after which the driver dereferences the destroyed window and panics inside
// processMouseClicked.
func (a *App) cameraMenu() *fyne.Menu {
	off := fyne.NewMenuItem("Turn Off Camera", func() { a.selectCameraDevice(deviceNone) })
	off.Checked = a.camDevice == deviceNone

	items := []*fyne.MenuItem{
		fyne.NewMenuItem("Show Camera Window", a.showCameraWindow),
		fyne.NewMenuItemSeparator(),
		off,
	}

	// deviceNone keys the off switch so one map covers every choice.
	a.camMenuItems = map[int]*fyne.MenuItem{deviceNone: off}

	for _, d := range camera.List() {
		idx, label := d.Index, d.Name
		item := fyne.NewMenuItem(label, func() { a.selectCameraDevice(idx) })
		item.Checked = a.camDevice == idx
		items = append(items, item)
		a.camMenuItems[idx] = item
	}

	return fyne.NewMenu("Camera", items...)
}

// selectCameraDevice switches capture to deviceID (deviceNone stops it) and
// moves the checkmark to match.
func (a *App) selectCameraDevice(deviceID int) {
	if deviceID != a.camDevice {
		slog.Info("camera selection changed", "device", deviceID)
		a.startCameraDevice(a.ctx, deviceID)
	}
	// Turning the camera off leaves nothing to preview, so the window goes with
	// it. Closed before updateResLabel so that runs against the cleared
	// preview/resLabel fields rather than widgets of a window on its way out.
	if deviceID == deviceNone && a.previewWin != nil {
		// Deferred, not called directly: this runs from the Camera menu item's
		// own dispatch, and destroying a window frees the GLFW handle that the
		// driver still dereferences once the handler returns (panic: nil
		// pointer in glfw.(*Window).GetCursorPos). fyne.Do queues it onto the
		// main loop, which drains after the current event.
		//
		// Unlike the main window -- which the selection window hands over
		// rather than closing, so no destroy happens there at all -- the camera
		// window genuinely has to be destroyed here, so the deferral is the
		// protection.
		win := a.previewWin
		fyne.Do(func() { win.Close() }) // SetOnClosed clears previewWin, preview, resLabel
	}
	a.updateResLabel()
	a.markCameraMenuItem()
}

// markCameraMenuItem checks the running device's item and clears the rest,
// mutating the existing items rather than rebuilding the menubar (see
// cameraMenu for why a rebuild from a menu callback crashes).
func (a *App) markCameraMenuItem() {
	for idx, item := range a.camMenuItems {
		item.Checked = idx == a.camDevice
	}
	// No SetMainMenu here on purpose: fyne reads item.Checked when it next
	// renders the menu, and re-setting the menubar from inside a menu callback
	// destroys the window mid-click (see cameraMenu).
}

// refresh reloads rows from the store and repaints the table. Safe to call
// from the UI goroutine only; background callers must wrap in fyne.Do.
func (a *App) refresh() {
	rows, err := loadRows(a.ctx, a.store)
	if err != nil {
		a.showError(err)
		return
	}
	// The list is not on screen in kiosk mode, but the rows still feed the
	// status bar counts.
	if !a.kiosk {
		a.table.setRows(rows)
	}
	a.updateStatus(rows)
}

// updateStatus refreshes the status bar from the rows just loaded, plus the
// live camera state. The clock is driven separately by startStatusClock.
func (a *App) updateStatus(rows []store.StudentRow) {
	if a.status == nil {
		return // tests may build an App without the main view
	}
	a.status.setRows(rows)
	a.status.setClock(time.Now())
	a.status.setCamera(a.camDevice != deviceNone)
}

// startStatusClock ticks the status bar's clock once a second until ctx is
// cancelled. Only the clock cell is touched; the counts change on refresh.
func (a *App) startStatusClock(ctx context.Context) {
	if a.status == nil {
		return
	}
	go func() {
		tick := time.NewTicker(time.Second)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-tick.C:
				fyne.Do(func() { a.status.setClock(now) })
			}
		}
	}()
}

func (a *App) showError(err error) {
	dialog.ShowError(err, a.win)
}

// showCameraWindow opens the camera feed in its own window, raising the
// existing one if it is already open. The first live frame arrives within about
// one capture frame of opening, so the preview starts blank only momentarily.
func (a *App) showCameraWindow() {
	if a.previewWin != nil {
		a.previewWin.RequestFocus()
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

	// Preview fills the window, with a thin status bar at the bottom showing
	// resolution. The device is chosen from the Camera menu, not from here.
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
	// The status bar's camera cell tracks the same transitions, and this runs on
	// every one of them (start, stop, device switch), so it is updated here
	// rather than from each call site.
	if a.status != nil {
		a.status.setCamera(a.camDevice != deviceNone)
	}
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
