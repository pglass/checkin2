package ui

import (
	_ "embed"
	"fmt"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"

	"github.com/pglass/checkin/internal/version"
)

// licenseText is the third-party notices shown in About -> Licenses. Embedded so
// the single static exe carries its own attribution and no separate file needs
// to ship alongside it (see internal/ui/licenses.txt).
//
//go:embed licenses.txt
var licenseText string

// about provides the About menu and its two windows. It is embedded by both the
// startup Center selector and the main App so the menu is available before a
// Center is open, not just afterwards.
type about struct {
	fyneApp fyne.App
	// parent owns the modal Version dialog; set by the embedding window.
	parent fyne.Window
	// licensesWin is the Licenses window while open, nil otherwise, so
	// reopening raises the existing one instead of duplicating it.
	licensesWin fyne.Window
}

// menu builds the About menubar menu.
func (ab *about) menu() *fyne.Menu {
	return fyne.NewMenu("About",
		fyne.NewMenuItem("Version…", ab.showVersionDialog),
		fyne.NewMenuItem("Licenses…", ab.showLicensesWindow),
	)
}

// showVersionDialog reports the program version. This is the version surface for
// Windows and Linux; on macOS the OS also provides a native "About" panel
// populated from the app bundle's Info.plist (see the Makefile's --appVersion).
func (ab *about) showVersionDialog() {
	dialog.ShowInformation("Version",
		fmt.Sprintf("Checkin\nVersion %s", version.Resolve()), ab.parent)
}

// showLicensesWindow opens (or raises) a scrollable window listing the
// third-party open-source licenses bundled into the app.
func (ab *about) showLicensesWindow() {
	if ab.licensesWin != nil {
		ab.licensesWin.RequestFocus()
		return
	}

	w := ab.fyneApp.NewWindow("Licenses")
	text := widget.NewLabel(licenseText)
	text.TextStyle = fyne.TextStyle{Monospace: true}
	w.SetContent(container.NewScroll(text))
	w.Resize(fyne.NewSize(760, 620))
	w.SetOnClosed(func() { ab.licensesWin = nil })
	ab.licensesWin = w
	w.Show()
}
