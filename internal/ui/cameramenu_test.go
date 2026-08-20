package ui

import (
	"context"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
)

func newTestCameraApp(t *testing.T) *App {
	t.Helper()
	fa := test.NewApp()
	t.Cleanup(fa.Quit)

	a := &App{fyneApp: fa, camDevice: deviceNone, ctx: context.Background()}
	a.win = fa.NewWindow("main")
	a.about = about{fyneApp: fa, parent: a.win}
	return a
}

// findMenu returns the named top-level menu.
func findMenu(t *testing.T, m *fyne.MainMenu, label string) *fyne.Menu {
	t.Helper()
	for _, menu := range m.Items {
		if menu.Label == label {
			return menu
		}
	}
	t.Fatalf("no %q menu", label)
	return nil
}

// The Camera menu holds the window item and a Select Camera submenu, and
// Show/Hide Camera is gone from File.
func TestCameraMenuStructure(t *testing.T) {
	a := newTestCameraApp(t)
	m := a.buildMenu()

	for _, item := range findMenu(t, m, "File").Items {
		if item.Label == "Show/Hide Camera" {
			t.Error("Show/Hide Camera should have been removed from File")
		}
	}

	cam := findMenu(t, m, "Camera")
	if cam.Items[0].Label != "Show Camera Window" {
		t.Errorf("first item = %q, want Show Camera Window", cam.Items[0].Label)
	}
	if !cam.Items[1].IsSeparator {
		t.Error("want a separator after Show Camera Window")
	}
	if cam.Items[2].Label != "Turn Off Camera" {
		t.Errorf("third item = %q, want Turn Off Camera", cam.Items[2].Label)
	}

	// Devices sit at the top level, not behind a submenu.
	for _, item := range cam.Items {
		if item.Label == "Select Camera" {
			t.Error("Select Camera submenu should have been removed")
		}
		if item.ChildMenu != nil {
			t.Errorf("%q unexpectedly has a submenu", item.Label)
		}
	}
}

// With no camera running, Turn Off Camera carries the checkmark. Each connected
// device is listed after it.
func TestCameraMenuChecksRunningDevice(t *testing.T) {
	a := newTestCameraApp(t)

	cam := findMenu(t, a.buildMenu(), "Camera")
	off := cam.Items[2]
	if !off.Checked {
		t.Error("Turn Off Camera not checked while no camera is running")
	}

	// Devices follow the off switch; none may be checked while capture is off.
	for _, item := range cam.Items[3:] {
		if item.Checked {
			t.Errorf("device %q checked while capture is off", item.Label)
		}
	}
}

// The menubar is built before startCamera picks a device, so the checkmark must
// be corrected afterwards -- otherwise "Turn Off Camera" stays checked while a
// camera is actually capturing.
func TestCameraMenuCheckmarkFollowsStartupDevice(t *testing.T) {
	a := newTestCameraApp(t)
	a.win.SetMainMenu(a.buildMenu())

	off := a.camMenuItems[deviceNone]
	if !off.Checked {
		t.Fatal("off switch should be checked before a device is running")
	}

	// Stand in for startCamera having selected device 0.
	a.camMenuItems[0] = fyne.NewMenuItem("fake", nil)
	a.camDevice = 0
	a.markCameraMenuItem()

	if off.Checked {
		t.Error("Turn Off Camera still checked while a camera is running")
	}
	if !a.camMenuItems[0].Checked {
		t.Error("running device not checked")
	}
}

// Opening the camera window twice raises the first rather than making a second.
func TestShowCameraWindowRaisesExisting(t *testing.T) {
	a := newTestCameraApp(t)

	a.showCameraWindow()
	if a.previewWin == nil {
		t.Fatal("camera window not opened")
	}
	first := a.previewWin

	a.showCameraWindow()
	if a.previewWin != first {
		t.Error("second call created a new window instead of raising the existing one")
	}

	a.previewWin.Close()
	if a.previewWin != nil {
		t.Error("window reference not cleared on close")
	}
}

// hasSelect reports whether a widget tree contains a drop-down.
func hasSelect(o fyne.CanvasObject) bool {
	switch v := o.(type) {
	case *fyne.Container:
		for _, child := range v.Objects {
			if hasSelect(child) {
				return true
			}
		}
	case *widget.Select:
		return true
	}
	return false
}

// The camera window no longer carries a device picker; selection moved to the
// Camera menu.
func TestCameraWindowHasNoPicker(t *testing.T) {
	a := newTestCameraApp(t)
	a.showCameraWindow()
	t.Cleanup(func() { a.previewWin.Close() })

	if hasSelect(a.previewWin.Content()) {
		t.Error("camera window still contains a device picker")
	}
}

// Selecting a device moves the checkmark by mutating the existing menu items.
// Rebuilding the menubar from a menu callback crashes the glfw driver, so this
// also guards that the items are updated in place.
func TestSelectCameraDeviceMovesCheckmark(t *testing.T) {
	a := newTestCameraApp(t)
	a.win.SetMainMenu(a.buildMenu())

	off, ok := a.camMenuItems[deviceNone]
	if !ok {
		t.Fatal("no menu item for the off switch")
	}
	if !off.Checked {
		t.Fatal("off switch not checked initially")
	}

	// Selecting the off switch again is a no-op that must keep it checked.
	a.selectCameraDevice(deviceNone)
	if !off.Checked {
		t.Error("off switch lost its checkmark")
	}

	// Simulate a device being selected without opening a real camera.
	a.camMenuItems[0] = fyne.NewMenuItem("fake", nil)
	a.camDevice = 0
	a.markCameraMenuItem()

	if off.Checked {
		t.Error("off switch still checked after a device was selected")
	}
	if !a.camMenuItems[0].Checked {
		t.Error("selected device not checked")
	}
}

// Turning the camera off closes the preview window: with capture stopped there
// is nothing left to show.
func TestTurnOffCameraClosesWindow(t *testing.T) {
	a := newTestCameraApp(t)
	a.win.SetMainMenu(a.buildMenu())

	a.showCameraWindow()
	if a.previewWin == nil {
		t.Fatal("camera window not opened")
	}

	// Pretend a device is running so the off switch is a real change.
	a.camMenuItems[0] = fyne.NewMenuItem("fake", nil)
	a.camDevice = 0

	a.selectCameraDevice(deviceNone)

	if a.previewWin != nil {
		t.Error("camera window still open after turning the camera off")
	}
	if a.preview != nil || a.resLabel != nil {
		t.Error("preview widgets not cleared after the window closed")
	}
	if !a.camMenuItems[deviceNone].Checked {
		t.Error("off switch not checked after turning the camera off")
	}
}

// Turning the camera off closes the window even when capture was already
// stopped: the user asked for the camera off, so the window goes either way.
func TestTurnOffCameraClosesWindowWhenAlreadyOff(t *testing.T) {
	a := newTestCameraApp(t)
	a.win.SetMainMenu(a.buildMenu())

	a.showCameraWindow()
	if a.previewWin == nil {
		t.Fatal("camera window not opened")
	}
	if a.camDevice != deviceNone {
		t.Fatalf("camDevice = %d, want none for this test", a.camDevice)
	}

	a.selectCameraDevice(deviceNone)

	if a.previewWin != nil {
		t.Error("camera window still open after turning the camera off")
	}
}
