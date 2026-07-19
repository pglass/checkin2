package ui

import (
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// holdDuration is how long the Reset button must be held to fire.
const holdDuration = 3 * time.Second

// holdButton fires its action only after being pressed and held for
// holdDuration. Releasing early cancels. A fill bar shows progress.
type holdButton struct {
	widget.BaseWidget
	label  string
	action func()

	fill    *canvas.Rectangle
	labelUI *widget.Label

	anim     *fyne.Animation
	holding  bool
	fireTime *time.Timer
}

func newHoldButton(label string, action func()) *holdButton {
	b := &holdButton{
		label:   label,
		action:  action,
		fill:    canvas.NewRectangle(theme.Color(theme.ColorNamePrimary)),
		labelUI: widget.NewLabel(label + " (hold 3s)"),
	}
	b.fill.Resize(fyne.NewSize(0, 0))
	b.ExtendBaseWidget(b)
	return b
}

func (b *holdButton) CreateRenderer() fyne.WidgetRenderer {
	border := canvas.NewRectangle(theme.Color(theme.ColorNameInputBackground))
	c := container.NewStack(border, b.fill, container.NewCenter(b.labelUI))
	return widget.NewSimpleRenderer(c)
}

// MouseDown starts the hold timer and fill animation.
func (b *holdButton) MouseDown(*desktop.MouseEvent) {
	b.holding = true
	full := b.Size().Width
	b.anim = fyne.NewAnimation(holdDuration, func(f float32) {
		b.fill.Resize(fyne.NewSize(full*f, b.Size().Height))
		b.fill.Refresh()
	})
	b.anim.Start()
	b.fireTime = time.AfterFunc(holdDuration, func() {
		fyne.Do(func() {
			if b.holding && b.action != nil {
				b.reset()
				b.action()
			}
		})
	})
}

// MouseUp cancels the hold if released before holdDuration elapsed.
func (b *holdButton) MouseUp(*desktop.MouseEvent) {
	if b.fireTime != nil {
		b.fireTime.Stop()
	}
	b.reset()
}

func (b *holdButton) reset() {
	b.holding = false
	if b.anim != nil {
		b.anim.Stop()
	}
	b.fill.Resize(fyne.NewSize(0, 0))
	b.fill.Refresh()
}

var _ desktop.Mouseable = (*holdButton)(nil)
