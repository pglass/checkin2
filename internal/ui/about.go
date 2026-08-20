package ui

import (
	_ "embed"
	"fmt"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"

	"github.com/pglass/checkin/internal/version"
)

// licenseText is the third-party notices shown in the About window. Embedded so
// the single static exe carries its own attribution and no separate file needs
// to ship alongside it (see internal/ui/licenses.txt).
//
//go:embed licenses.txt
var licenseText string

// about provides the About window: the program version and the third-party
// license notices in one view. It is embedded by both the startup Center
// selector and the main App so the information is available before a Center is
// open, not just afterwards.
type about struct {
	fyneApp fyne.App
	// parent is the window the About item was invoked from; retained so the
	// About window can be created by the same Fyne app that owns it.
	parent fyne.Window
	// win is the About window while open, nil otherwise, so reopening raises
	// the existing one instead of duplicating it.
	win fyne.Window
}

// menuItem builds the About entry. It is placed at the bottom of the File menu
// by every window that has one, rather than in a menu of its own.
//
// On macOS, Fyne's menu driver moves items labelled exactly "About" into the
// native application menu (see handleSpecialItems in fyne's menu_darwin.go), so
// this appears under "checkin" there and under "File" on Windows and Linux.
func (ab *about) menuItem() *fyne.MenuItem {
	return fyne.NewMenuItem("About", ab.showWindow)
}

// showWindow opens (or raises) the About window: version at the top, the
// scrollable third-party licenses below.
//
// This is the version surface for Windows and Linux; on macOS the OS also
// provides a native "About" panel populated from the app bundle's Info.plist
// (see the Makefile's --appVersion).
func (ab *about) showWindow() {
	if ab.win != nil {
		ab.win.RequestFocus()
		return
	}

	title := widget.NewLabelWithStyle("Checkin", fyne.TextAlignLeading,
		fyne.TextStyle{Bold: true})
	ver := widget.NewLabel(fmt.Sprintf("Version %s", version.Resolve()))

	licensesHdr := widget.NewLabelWithStyle("Third-party licenses",
		fyne.TextAlignLeading, fyne.TextStyle{Bold: true})

	licenses := widget.NewLabel(licenseText)
	licenses.TextStyle = fyne.TextStyle{Monospace: true}

	// Version block stays pinned while only the (long) licenses scroll.
	header := container.NewVBox(title, ver, widget.NewSeparator(), licensesHdr)

	w := ab.fyneApp.NewWindow("About")
	w.SetContent(withWindowMargin(
		container.NewBorder(header, nil, nil, nil, container.NewScroll(licenses))))
	w.Resize(fyne.NewSize(760, 620))
	w.SetOnClosed(func() { ab.win = nil })
	ab.win = w
	w.Show()
}
