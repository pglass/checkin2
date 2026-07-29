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

// showVersionDialog reports the program version. This is the version surface for
// Windows and Linux; on macOS the OS also provides a native "About" panel
// populated from the app bundle's Info.plist (see the Makefile's --appVersion).
func (a *App) showVersionDialog() {
	dialog.ShowInformation("Version",
		fmt.Sprintf("Checkin\nVersion %s", version.Resolve()), a.win)
}

// showLicensesWindow opens (or raises) a scrollable window listing the
// third-party open-source licenses bundled into the app. Tracked like the other
// secondary windows so reopening raises the existing one instead of duplicating.
func (a *App) showLicensesWindow() {
	if a.licensesWin != nil {
		a.licensesWin.RequestFocus()
		return
	}

	w := a.fyneApp.NewWindow("Licenses")
	text := widget.NewLabel(licenseText)
	text.TextStyle = fyne.TextStyle{Monospace: true}
	w.SetContent(container.NewScroll(text))
	w.Resize(fyne.NewSize(760, 620))
	w.SetOnClosed(func() { a.licensesWin = nil })
	a.licensesWin = w
	w.Show()
}
