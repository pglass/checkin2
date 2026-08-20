package ui

import (
	"fmt"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"

	"github.com/pglass/checkin/internal/store"
)

// statusClockFormat renders the clock as "Jan 2, 2006 15:04:05" in local time
// (time.Time.Format uses that reference date). Seconds are shown, so the bar
// ticks once a second.
const statusClockFormat = "Jan 2, 2006 15:04:05"

// statusBar is the one-line summary along the bottom of the main window:
// student count, the current time, today's check-in/out totals, and whether the
// camera is running.
type statusBar struct {
	// camera sits on the left, summary is centred, clock is on the right.
	camera  *canvas.Text
	summary *canvas.Text
	clock   *canvas.Text
}

// newStatusBar builds the bar. Every cell is caption-sized, matching the
// camera window's resolution readout.
func newStatusBar() *statusBar {
	b := &statusBar{
		camera:  statusText(),
		summary: statusText(),
		clock:   statusText(),
	}
	return b
}

// statusText makes one caption-sized cell.
func statusText() *canvas.Text {
	t := canvas.NewText("", theme.Color(theme.ColorNamePlaceHolder))
	t.TextSize = theme.CaptionTextSize()
	return t
}

// widget lays the bar out in three groups: camera state pinned left, the
// student summary centred, and the clock pinned right.
//
// A Border with the summary as its centre keeps that group truly centred in the
// bar rather than merely between the other two -- an HBox with spacers would
// shift it as the side texts change width.
func (b *statusBar) widget() fyne.CanvasObject {
	return container.NewBorder(nil, nil, b.camera, b.clock,
		container.NewCenter(b.summary))
}

// setRows refreshes the counts derived from the student list: the total, and
// how many have checked in or out today. Taken from the rows the main table is
// already showing, so no extra queries are needed.
func (b *statusBar) setRows(rows []store.StudentRow) {
	var in, out int
	for _, r := range rows {
		if r.In != nil {
			in++
		}
		if r.Out != nil {
			out++
		}
	}
	setStatusText(b.summary, fmt.Sprintf("%d total %s. %d %s, %d %s today",
		len(rows), plural(len(rows), "student"),
		in, plural(in, "check-in"),
		out, plural(out, "check-out")))
}

// setClock refreshes the date/time cell.
func (b *statusBar) setClock(now time.Time) {
	setStatusText(b.clock, now.Format(statusClockFormat))
}

// setCamera refreshes the camera cell.
func (b *statusBar) setCamera(on bool) {
	text := "Camera Off"
	if on {
		text = "Camera On"
	}
	setStatusText(b.camera, text)
}

// setStatusText updates a cell only when the text actually changed: the clock
// ticks every second, and repainting unchanged cells would be wasted work.
func setStatusText(t *canvas.Text, s string) {
	if t.Text == s {
		return
	}
	t.Text = s
	t.Refresh()
}

// plural appends "s" to word unless n is 1.
func plural(n int, word string) string {
	if n == 1 {
		return word
	}
	return word + "s"
}
