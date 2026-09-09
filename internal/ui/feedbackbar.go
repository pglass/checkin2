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

// feedbackIdleDelay is how long the bar must go without a message before it
// falls back to the idle hint. Measured from the last flash, not from the end
// of its fade: the hint is for a user who is not doing anything, and one who
// just checked a student in has seen their result and does not need telling how
// to check one in.
//
// A var rather than a const so tests can shrink it; nothing else assigns to it.
var feedbackIdleDelay = 10 * time.Second

// Idle hints, shown when nothing else has been on the bar for
// feedbackIdleDelay. Which one depends on whether QR scanning is available:
// telling a user to scan a code with no camera attached sends them looking for
// a feature they do not have.
const (
	idleMsgWithCamera    = "Scan a QR code or double-click a student to check in or out"
	idleMsgWithoutCamera = "Double-click a student to check-in or out. (Camera not available for QR code scans)"
	// noStudentsMsg replaces the idle hint entirely when the Center is empty:
	// neither scanning nor double-clicking is possible with no students, so the
	// only useful instruction is how to add some.
	noStudentsMsg = "No students found. Use 'File > Add Student..' or 'File > Import...' to add students."
)

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

	// hint is the standing message the bar falls back to when nothing has
	// flashed for feedbackIdleDelay: the idle instructions, or the "no students"
	// notice, or "" in kiosk mode where the kiosk screen carries its own prompt.
	// Set by setHint, which the App recomputes on every refresh.
	hint string
	// idleTimer fires feedbackIdleDelay after the last flash and paints hint.
	// Guarded by mu alongside the flash timer, since both are restarted from
	// flash and cancelled from the same places.
	idleTimer *time.Timer
	// hintShowing is true while the bar is displaying the hint rather than a
	// flash. A fresh bar shows nothing, which is the hint's territory rather
	// than a flash's, so it starts true: the first setHint on opening a Center
	// paints immediately instead of waiting out an idle delay that no flash has
	// started.
	hintShowing bool
}

func newFeedbackBar() *feedbackBar {
	b := &feedbackBar{
		bg:   canvas.NewRectangle(color.Transparent),
		text: canvas.NewText("", color.Transparent),
	}
	b.text.TextSize = theme.TextSize()
	b.text.Alignment = fyne.TextAlignCenter
	// Nothing has flashed yet, so the bar is at rest and a hint may paint at
	// once; see the field comment.
	b.hintShowing = true
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

// setHint sets the standing message the bar falls back to when idle, and paints
// it right away if the bar is not currently showing a flash.
//
// The App recomputes this on every refresh, so the hint tracks the things it
// depends on -- how many students there are, whether a camera is running,
// whether kiosk mode is on -- without the bar knowing about any of them. An
// empty hint means "show nothing when idle".
//
// Repainting immediately rather than waiting for the next idle expiry matters
// on open: the very first refresh happens before anything has flashed, and a
// Center with no students should say so at once rather than after ten seconds.
func (b *feedbackBar) setHint(hint string) {
	b.mu.Lock()
	unchanged := hint == b.hint
	b.hint = hint
	// A flash is on screen: it owns the bar until its own idle timer expires,
	// at which point the new hint is what gets painted. hintShowing is the test
	// rather than "is a timer pending", since the flash timer stays non-nil
	// long after the flash it belonged to has gone.
	flashing := !b.hintShowing
	b.mu.Unlock()

	if unchanged || flashing {
		return
	}
	b.showHint()
}

// showHint paints the standing hint, or clears the bar when there is none.
// Hints do not fade: they are the bar's resting state, not an event.
func (b *feedbackBar) showHint() {
	b.mu.Lock()
	msg := b.hint
	// A hint is not a flash, so any pending fade for the flash it replaces must
	// not run: it would fade the hint out and leave the bar blank.
	if b.anim != nil {
		b.anim.Stop()
		b.anim = nil
	}
	b.gen++
	b.hintShowing = true

	b.text.Text = msg
	// Hints are advisory, not results: plain foreground text on no background,
	// so a check-in's green flash still stands out against them.
	b.text.Color = theme.Color(theme.ColorNameForeground)
	b.bg.FillColor = color.Transparent
	b.mu.Unlock()

	b.text.Refresh()
	b.bg.Refresh()
}

// startIdleTimer schedules the hint to be shown feedbackIdleDelay from now,
// replacing any previously scheduled one. Caller must hold mu.
func (b *feedbackBar) startIdleTimer() {
	if b.idleTimer != nil {
		b.idleTimer.Stop()
	}
	gen := b.gen
	b.idleTimer = time.AfterFunc(feedbackIdleDelay, func() {
		fyne.Do(func() {
			// A flash after this timer was set owns the bar and has scheduled
			// its own idle expiry; this one is stale.
			b.mu.Lock()
			stale := b.gen != gen
			b.mu.Unlock()
			if stale {
				return
			}
			b.showHint()
		})
	})
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
	b.hintShowing = false

	b.text.Text = msg
	b.text.Color = textColor
	b.bg.FillColor = fill

	b.timer = time.AfterFunc(feedbackHold, func() {
		fyne.Do(func() { b.startFade(gen, fill, textColor) })
	})
	// The idle clock runs from this message, so a burst of check-ins keeps the
	// hint away until ten quiet seconds have passed since the last one.
	b.startIdleTimer()
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

// message returns the bar's current text. Used by tests, which read it from
// outside the UI goroutine while a timer may be writing it.
func (b *feedbackBar) message() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.text.Text
}

// textColor returns the bar's current text colour, for the same reason.
func (b *feedbackBar) textColor() color.Color {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.text.Color
}
