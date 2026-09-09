package ui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
)

// Kiosk mode replaces the student list with a single instruction message and
// strips the menubar down to the one item that leaves again. The feedback bar
// and status bar stay, so a scan still reports its result and the clock and
// camera state remain visible.
//
// The mode is a runtime toggle only -- it is not persisted, so relaunching the
// app always comes back in the normal view.

// kioskPrompt is the whole of the kiosk screen's instructions.
const kioskPrompt = "Scan your QR code to sign in or out"

// kioskContent builds the centered instruction shown in place of the student
// list.
func kioskContent() fyne.CanvasObject {
	msg := canvas.NewText(kioskPrompt, theme.Color(theme.ColorNameForeground))
	msg.Alignment = fyne.TextAlignCenter
	msg.TextStyle = fyne.TextStyle{Bold: true}
	msg.TextSize = theme.TextSize() * 2
	return container.NewCenter(msg)
}

// enterKiosk switches the main window to the kiosk view: instruction message
// instead of the student list, a single-item menubar, and fullscreen so the
// desktop behind it is out of the way. Fullscreen is used rather than a
// maximize call because fyne has no cross-platform maximize.
func (a *App) enterKiosk() {
	a.kiosk = true
	a.applyMode()
	// The kiosk screen carries its own prompt, so the feedback bar's hint is
	// dropped; refresh is what recomputes it for the new mode.
	a.refresh()
}

// leaveKiosk restores the student list, the full menubar, and the windowed
// size.
func (a *App) leaveKiosk() {
	a.kiosk = false
	a.applyMode()
	// The list was not repainted while kiosk mode was showing, so any check-in
	// that happened in the meantime is only in the store.
	a.refresh()
}

// applyMode repaints the window for the current value of a.kiosk.
//
// Deferred onto the main loop rather than run inline: both toggles are invoked
// from a menu item's own click dispatch, and SetMainMenu tears down and
// rebuilds the menu widgets while that click is still being handled, after
// which the driver dereferences the destroyed widgets and panics inside
// processMouseClicked (the same hazard cameraMenu documents). fyne.Do queues
// the swap until the current event has drained.
func (a *App) applyMode() {
	kiosk := a.kiosk
	fyne.Do(func() {
		a.setMainContent()
		a.win.SetMainMenu(a.buildMenu())
		a.win.SetFullScreen(kiosk)
	})
}

// setMainContent installs the kiosk message or the student list above the
// feedback and status bars, whichever the current mode calls for.
func (a *App) setMainContent() {
	bottom := container.NewVBox(a.feedback.widget(), a.status.widget())
	var main fyne.CanvasObject
	if a.kiosk {
		main = kioskContent()
	} else {
		main = a.table.widget()
	}
	a.win.SetContent(withWindowMargin(
		container.NewBorder(nil, bottom, nil, nil, main)))
}
