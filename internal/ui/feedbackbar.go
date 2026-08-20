package ui

import (
	"fmt"
	"image/color"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
)

// How long a scan result stays fully visible, and how long it then takes to
// fade away. Both the background colour and the text fade together, so the bar
// ends up empty rather than holding a stale message indefinitely.
const (
	feedbackHold = 3 * time.Second
	feedbackFade = 3 * time.Second
)

// feedbackTimeFormat renders the time in a check-in/out message, matching the
// confirmation dialog's "3:04 PM".
const feedbackTimeFormat = "3:04 PM"

// feedbackWarnColor is the "nothing to do" colour: a scan that was read fine
// but could not be acted on (the student already checked out today). Amber
// rather than the success green, and rather than the error red -- nothing has
// gone wrong.
var feedbackWarnColor = color.NRGBA{R: 0xE6, G: 0xA2, B: 0x3C, A: 0xFF}

// feedbackBar is the full-width strip above the status bar that reports the
// most recent scan. It holds the result for feedbackHold, then fades both the
// colour and the text out over feedbackFade. A new result during either phase
// replaces it immediately and restarts the timing.
type feedbackBar struct {
	bg   *canvas.Rectangle
	text *canvas.Text

	// mu guards gen and the pending hold timer / fade animation. flash runs on
	// the UI goroutine, but the hold expiry arrives on a time.AfterFunc
	// goroutine.
	mu sync.Mutex
	// gen counts the flashes, so a hold or fade belonging to an earlier scan
	// cannot touch a later one's display.
	gen   int
	timer *time.Timer
	anim  *fyne.Animation
}

func newFeedbackBar() *feedbackBar {
	b := &feedbackBar{
		bg:   canvas.NewRectangle(color.Transparent),
		text: canvas.NewText("", color.Transparent),
	}
	b.text.TextSize = theme.TextSize()
	b.text.Alignment = fyne.TextAlignCenter
	return b
}

// widget stacks the text over the coloured background, sized to a bit more than
// text height so the bar reads as a band rather than a line.
func (b *feedbackBar) widget() fyne.CanvasObject {
	b.bg.SetMinSize(fyne.NewSize(0, theme.TextSize()+4*theme.Padding()))
	return container.NewStack(b.bg, container.NewCenter(b.text))
}

// showCheckIn flashes "<name> checked in at <time>" in green.
func (b *feedbackBar) showCheckIn(name string, at time.Time) {
	b.flash(fmt.Sprintf("%s checked in at %s", name, at.Format(feedbackTimeFormat)),
		theme.Color(theme.ColorNameSuccess))
}

// showCheckOut flashes "<name> checked out at <time>" in green.
func (b *feedbackBar) showCheckOut(name string, at time.Time) {
	b.flash(fmt.Sprintf("%s checked out at %s", name, at.Format(feedbackTimeFormat)),
		theme.Color(theme.ColorNameSuccess))
}

// showAlreadyOut flashes the "nothing to do" warning in amber, for a student
// who has already checked in and out today.
func (b *feedbackBar) showAlreadyOut(name string) {
	b.flash(fmt.Sprintf("%s is already checked out", name), feedbackWarnColor)
}

// flash shows msg on a bar of the given colour, holds it, then fades it out.
// Must be called on the UI goroutine.
func (b *feedbackBar) flash(msg string, fill color.Color) {
	textColor := theme.Color(theme.ColorNameForeground)

	b.mu.Lock()
	// Cancel whatever the previous scan had pending: a hold that has not
	// expired, or a fade already under way. Without this the old timing would
	// keep running and blank this message early.
	if b.timer != nil {
		b.timer.Stop()
	}
	if b.anim != nil {
		b.anim.Stop()
		b.anim = nil
	}
	b.gen++
	gen := b.gen

	b.text.Text = msg
	b.text.Color = textColor
	b.bg.FillColor = fill

	b.timer = time.AfterFunc(feedbackHold, func() {
		fyne.Do(func() { b.startFade(gen, fill, textColor) })
	})
	b.mu.Unlock()

	b.text.Refresh()
	b.bg.Refresh()
}

// startFade animates the background and text from their current colours to
// fully transparent, unless a newer flash has taken over since the hold began.
// Stop() cannot cancel a timer that has already fired, so the generation check
// is the real guard.
func (b *feedbackBar) startFade(gen int, fill, textColor color.Color) {
	b.mu.Lock()
	if b.gen != gen {
		b.mu.Unlock()
		return
	}

	// Two animations rather than one: the background and the text start from
	// different colours, and both must land on fully transparent.
	// The animation ticks come from fyne's animation goroutine, so these writes
	// take the same lock the accessors read under.
	bgAnim := canvas.NewColorRGBAAnimation(fill, color.Transparent, feedbackFade, func(c color.Color) {
		b.mu.Lock()
		b.bg.FillColor = c
		b.mu.Unlock()
		b.bg.Refresh()
	})
	textAnim := canvas.NewColorRGBAAnimation(textColor, color.Transparent, feedbackFade, func(c color.Color) {
		b.mu.Lock()
		b.text.Color = c
		b.mu.Unlock()
		b.text.Refresh()
	})
	b.anim = bgAnim
	b.mu.Unlock()

	bgAnim.Start()
	textAnim.Start()
}

// fillColor returns the bar's current background colour. Used by tests, which
// read it from outside the UI goroutine.
func (b *feedbackBar) fillColor() color.Color {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.bg.FillColor
}

// textColor returns the bar's current text colour, for the same reason.
func (b *feedbackBar) textColor() color.Color {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.text.Color
}
