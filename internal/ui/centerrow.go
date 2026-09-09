package ui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"
)

// centerRow is one row of the Center selection list: a button that opens the
// Center, plus a right-click context menu it cannot get from widget.Button
// alone.
//
// It embeds widget.Button rather than wrapping one, so the row keeps the
// button's appearance and tap handling exactly as before and gains only
// MouseDown. The main window's studentRow builds its own renderer because it
// lays out four columns; a Center row is a single label, so there is nothing to
// gain by re-implementing what Button already draws.
type centerRow struct {
	widget.Button

	// onSecondary is called on right-click, with the click's absolute position
	// so the menu can be popped up under the pointer. Rebound on every list
	// update, since widget.List recycles rows across indices.
	onSecondary func(fyne.Position)
}

func newCenterRow() *centerRow {
	r := &centerRow{}
	r.ExtendBaseWidget(r)
	r.Alignment = widget.ButtonAlignLeading
	r.Importance = widget.LowImportance
	return r
}

// MouseDown implements desktop.Mouseable, opening the context menu on
// press-down to match the main window's student rows and platform convention.
func (r *centerRow) MouseDown(e *desktop.MouseEvent) {
	if e.Button == desktop.MouseButtonSecondary && r.onSecondary != nil {
		r.onSecondary(e.AbsolutePosition)
	}
}

// MouseUp implements desktop.Mouseable (no-op; the menu opens on MouseDown).
func (r *centerRow) MouseUp(_ *desktop.MouseEvent) {}

var _ desktop.Mouseable = (*centerRow)(nil)
