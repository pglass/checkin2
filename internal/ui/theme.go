package ui

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
)

// compactTheme wraps the default theme and shrinks the spacing sizes so every
// window shares the same tight look. Only size overrides are customized; colors,
// fonts, and icons fall through to the base theme.
type compactTheme struct{ fyne.Theme }

// newCompactTheme wraps the current default theme.
func newCompactTheme() fyne.Theme { return compactTheme{theme.DefaultTheme()} }

func (c compactTheme) Color(n fyne.ThemeColorName, v fyne.ThemeVariant) color.Color {
	return c.Theme.Color(n, v)
}

func (c compactTheme) Size(n fyne.ThemeSizeName) float32 {
	switch n {
	case theme.SizeNamePadding:
		return 2 // default 4: outer margins around widgets
	case theme.SizeNameInnerPadding:
		return 4 // default 8: padding inside buttons/entries
	case theme.SizeNameLineSpacing:
		return 2 // default 4: gap between stacked items
	}
	return c.Theme.Size(n)
}

// Window-edge margins between a window's content and its edges. Kept separate
// from theme padding (which also drives internal container gaps) so the window
// border can breathe without loosening spacing everywhere else.
const (
	windowEdgeInsetX = 8 // left/right
	windowEdgeInsetY = 0 // top/bottom
)

// insetLayout insets its single child by fixed horizontal and vertical margins.
type insetLayout struct{ x, y float32 }

func (l insetLayout) MinSize(objs []fyne.CanvasObject) fyne.Size {
	if len(objs) == 0 {
		return fyne.NewSize(2*l.x, 2*l.y)
	}
	return objs[0].MinSize().AddWidthHeight(2*l.x, 2*l.y)
}

func (l insetLayout) Layout(objs []fyne.CanvasObject, size fyne.Size) {
	if len(objs) == 0 {
		return
	}
	objs[0].Move(fyne.NewPos(l.x, l.y))
	objs[0].Resize(fyne.NewSize(size.Width-2*l.x, size.Height-2*l.y))
}

// withWindowMargin wraps content in the standard window-edge margins.
func withWindowMargin(content fyne.CanvasObject) fyne.CanvasObject {
	return container.New(insetLayout{x: windowEdgeInsetX, y: windowEdgeInsetY}, content)
}
