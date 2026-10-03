package ui

import (
	"image/color"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/pglass/checkin/internal/store"
)

// qrButtonText is the text on the per-row QR button. Short on purpose: it sits
// on every row, so a longer label would eat the name columns.
const qrButtonText = "Show Code"

// qrButtonLabel is what the button is actually built with: the text, led by
// enough blank space for the glyph that is drawn over it. The button centers
// this whole string, so the padding reserves the glyph's place rather than the
// icon overlapping the words.
var qrButtonLabel = strings.Repeat(" ", qrIconLabelPad) + qrButtonText

// qrIconLabelPad is how many spaces stand in for the glyph in the label.
// Tuned so the gap is wider than the icon, giving it breathing room on both
// sides rather than butting up against the text; qrIconInsetX then centers the
// glyph within that gap.
const qrIconLabelPad = 7

// rowButtonTheme is shared by every row's button: one instance, since
// widget.List builds and recycles these constantly.
var rowButtonTheme = newRowButtonTheme()

// studentTable renders the main student list with Last Name / First Name / In /
// Out columns plus a per-row QR button, and wires double-click (check-in/out)
// and right-click (remove) per row.
type studentTable struct {
	app  *App
	rows []store.StudentRow
	list *widget.List

	// onQR is what a row's QR button calls, with that row's student. It is a
	// field rather than a direct call so tests can observe which student a
	// button is bound to without opening a window.
	onQR func(store.StudentRow)
}

func newStudentTable(a *App) *studentTable {
	t := &studentTable{app: a}
	t.onQR = func(r store.StudentRow) { a.generateQRForStudent(r) }
	t.list = widget.NewList(
		func() int { return len(t.rows) },
		func() fyne.CanvasObject { return newStudentRow() },
		func(i widget.ListItemID, o fyne.CanvasObject) {
			row := o.(*studentRow)
			r := t.rows[i]
			row.update(r)
			row.onDouble = func() { t.app.showCheckInOutDialog(r) }
			row.onSecondary = func(pos fyne.Position, mod fyne.KeyModifier) {
				t.app.showRowContextMenu(r, pos, mod)
			}
			row.onQR = func() { t.onQR(r) }
		},
	)
	return t
}

func (t *studentTable) widget() fyne.CanvasObject {
	// The QR button column is deliberately unlabelled: the button carries its
	// own text, and a header over it would read as a sortable column.
	header := container.NewBorder(nil, nil, qrColumnSpacer(), nil,
		container.NewGridWithColumns(4,
			boldText("Last Name"), boldText("First Name"),
			boldText("Check In"), boldText("Check Out"),
		),
	)
	return container.NewBorder(header, nil, nil, nil, t.list)
}

// qrColumnSpacer is an invisible stand-in for the QR button, so the four text
// headers line up with the four text columns rather than spreading across the
// button's width too.
func qrColumnSpacer() fyne.CanvasObject {
	s := canvas.NewRectangle(color.Transparent)
	s.SetMinSize(fyne.NewSize(qrColumnWidth(), 0))
	return s
}

// These are deliberately plain functions rather than cached values: they read
// the live theme (directly, or through rowHeight), and a cached copy would
// freeze whichever theme happened to be installed at first call. They are only
// arithmetic over a text measurement, and rowHeight is already called per row
// per layout, so there is nothing here worth caching.

// qrButtonWidth is the width reserved for the per-row QR button. Measured from
// the button's own shrunken theme, not the app's, or the column would be sized
// for text larger than the button actually draws.
//
// The glyph needs no separate allowance: it is drawn over the label's leading
// spaces, which this measurement already includes.
func qrButtonWidth() float32 {
	return qrLabelWidth() + 2*rowButtonInnerPadding + 2*theme.Padding()
}

// qrColumnGap is clear space between the button and the Last Name column. It
// sits outside the button's own cell -- the button fills that cell, so folding
// the gap into its width would stretch the button instead of separating it.
const qrColumnGap = 8

// qrColumnWidth is the full width the button column occupies: the button plus
// the gap after it. This is what the header reserves so the names line up with
// their header.
func qrColumnWidth() float32 { return qrButtonWidth() + qrColumnGap }

// qrLabelWidth is the rendered width of the button's padded label.
func qrLabelWidth() float32 {
	return fyne.MeasureText(qrButtonLabel, rowButtonTextSize, fyne.TextStyle{}).Width
}

// qrIconInsetX centers the glyph inside the blank space the label leads with.
// It is measured from where the button's centered label starts, so the glyph
// tracks the text rather than drifting if the label or button width changes.
func qrIconInsetX() float32 {
	labelStartX := (qrButtonWidth() - qrLabelWidth()) / 2
	padW := fyne.MeasureText(strings.Repeat(" ", qrIconLabelPad), rowButtonTextSize, fyne.TextStyle{}).Width
	return labelStartX + (padW-rowButtonIconSize)/2
}

// qrIconInsetY is the glyph's vertical offset inside the row. Centering it
// would put it level with the text's full line box, which reads a shade high
// against the shorter glyph, so it is nudged one point further down -- the
// adjustment this function exists for.
func qrIconInsetY() float32 {
	return (rowHeight()-rowButtonIconSize)/2 + qrIconNudgeY
}

// qrIconNudgeY is the manual downward adjustment on the glyph, in points.
// 0 leaves it centered in the row.
const qrIconNudgeY = 0

func (t *studentTable) setRows(rows []store.StudentRow) {
	t.rows = rows
	t.list.Refresh()
}

// boldText mirrors the row cells (unpadded canvas.Text) but bold, so the header
// lines up with the compact rows.
func boldText(s string) *canvas.Text {
	t := canvas.NewText(s, theme.Color(theme.ColorNameForeground))
	t.TextSize = theme.TextSize()
	t.TextStyle = fyne.TextStyle{Bold: true}
	return t
}

// rowHeight is the fixed height of a list row: text height plus the theme's
// padding above and below. Derived from the theme (not a hard-coded gap) so the
// list's vertical spacing tracks the app theme and stays consistent with the
// rest of the UI. The custom renderer is still needed to override widget.List's
// tall default row height.
//
// The row's QR button is sized to fit within this rather than the other way
// round: it is themed down by rowButtonTheme so that adding it left the list as
// compact as it was before.
func rowHeight() float32 { return theme.TextSize() + 2*theme.Padding() }

// studentRow is a custom list row supporting double-tap and secondary-tap. It
// uses unpadded canvas.Text (not widget.Label) to avoid the label's built-in
// vertical padding, keeping rows compact.
type studentRow struct {
	widget.BaseWidget
	last, first, in, out *canvas.Text
	qr                   *widget.Button
	// icon is the QR glyph drawn over the button; see newStudentRow for why it
	// is not the button's own icon.
	icon *canvas.Image

	onDouble    func()
	onSecondary func(fyne.Position, fyne.KeyModifier)
	// onQR is called when the row's QR button is tapped. Rebound on every list
	// update, since widget.List recycles rows across indices.
	onQR func()
}

func newStudentRow() *studentRow {
	mk := func() *canvas.Text {
		t := canvas.NewText("", theme.Color(theme.ColorNameForeground))
		t.TextSize = theme.TextSize()
		return t
	}
	r := &studentRow{last: mk(), first: mk(), in: mk(), out: mk()}
	// The callback goes through r.onQR rather than being set per update, so the
	// button itself is created once and recycled with the row.
	//
	// The glyph is NOT the button's own icon: widget.Button vertically centers
	// its label but not its icon (see buttonRenderer.Layout), which leaves the
	// glyph sitting a touch high against the text. The button is built
	// text-only -- which takes its properly centered "label only" path -- and
	// the icon is drawn over it at a position this code controls.
	r.qr = widget.NewButton(qrButtonLabel, func() {
		if r.onQR != nil {
			r.onQR()
		}
	})
	r.qr.Importance = widget.LowImportance
	r.icon = canvas.NewImageFromResource(qrCodeIconResource)
	r.icon.FillMode = canvas.ImageFillContain
	r.icon.SetMinSize(fyne.NewSquareSize(rowButtonIconSize))
	r.ExtendBaseWidget(r)
	return r
}

func (r *studentRow) update(row store.StudentRow) {
	r.last.Text = row.Name.Last
	r.first.Text = row.Name.First
	r.in.Text = timeFmt(row.In)
	r.out.Text = timeFmt(row.Out)
	r.last.Refresh()
	r.first.Refresh()
	r.in.Refresh()
	r.out.Refresh()
}

func (r *studentRow) CreateRenderer() fyne.WidgetRenderer {
	bg := canvas.NewRectangle(color.Transparent)
	grid := container.NewGridWithColumns(4, r.last, r.first, r.in, r.out)
	// The button is pinned to the leading edge at its natural width; the four
	// text columns share what is left, matching the header above.
	//
	// GridWrap pins the cell to exactly the row's height so the button cannot
	// push the row taller than rowHeight, and the ThemeOverride is what makes
	// that height one the button can actually fit inside.
	btn := container.NewThemeOverride(r.qr, rowButtonTheme)
	// The glyph is stacked over the button rather than set as its icon, so its
	// vertical position is ours to set; qrIconOffset nudges it down into line
	// with the label. The button's own text is padded leading-side by
	// qrButtonLabel's spaces to leave the glyph a clear space to sit in.
	icon := container.New(fixedSizeAtLayout{x: qrIconInsetX(), y: qrIconInsetY()}, r.icon)
	cell := container.NewStack(btn, icon)
	// The column is wider than the button by qrColumnGap; the button is left at
	// its own width inside it, so the gap falls between it and the names.
	buttonCol := container.NewGridWrap(fyne.NewSize(qrButtonWidth(), rowHeight()), cell)
	cols := container.NewBorder(nil, nil,
		container.NewGridWrap(fyne.NewSize(qrColumnWidth(), rowHeight()), buttonCol),
		nil, grid)
	c := container.NewStack(bg, cols)
	return &rowRenderer{obj: c}
}

// rowRenderer forces a compact fixed height regardless of child min sizes.
type rowRenderer struct {
	obj fyne.CanvasObject
}

func (r *rowRenderer) Layout(size fyne.Size)        { r.obj.Resize(size) }
func (r *rowRenderer) MinSize() fyne.Size           { return fyne.NewSize(r.obj.MinSize().Width, rowHeight()) }
func (r *rowRenderer) Refresh()                     { r.obj.Refresh() }
func (r *rowRenderer) Objects() []fyne.CanvasObject { return []fyne.CanvasObject{r.obj} }
func (r *rowRenderer) Destroy()                     {}

// DoubleTapped implements fyne.DoubleTappable.
func (r *studentRow) DoubleTapped(_ *fyne.PointEvent) {
	if r.onDouble != nil {
		r.onDouble()
	}
}

// Tapped implements fyne.Tappable (needed so DoubleTapped fires).
func (r *studentRow) Tapped(_ *fyne.PointEvent) {}

// MouseDown implements desktop.Mouseable so the right-click context menu opens
// on press-down (matching platform conventions) rather than on release.
func (r *studentRow) MouseDown(e *desktop.MouseEvent) {
	if e.Button == desktop.MouseButtonSecondary && r.onSecondary != nil {
		r.onSecondary(e.AbsolutePosition, e.Modifier)
	}
}

// MouseUp implements desktop.Mouseable (no-op; the menu opens on MouseDown).
func (r *studentRow) MouseUp(_ *desktop.MouseEvent) {}

var _ fyne.Tappable = (*studentRow)(nil)
var _ fyne.DoubleTappable = (*studentRow)(nil)
var _ desktop.Mouseable = (*studentRow)(nil)

// keep theme import used for potential future styling
var _ = theme.Color
