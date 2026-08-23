package ui

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/pglass/checkin/internal/store"
)

// studentTable renders the main student list with Last Name / First Name / In /
// Out columns and wires double-click (check-in/out) and right-click (remove)
// per row.
type studentTable struct {
	app  *App
	rows []store.StudentRow
	list *widget.List
}

func newStudentTable(a *App) *studentTable {
	t := &studentTable{app: a}
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
		},
	)
	return t
}

func (t *studentTable) widget() fyne.CanvasObject {
	header := container.NewGridWithColumns(4,
		boldText("Last Name"), boldText("First Name"),
		boldText("Check In"), boldText("Check Out"),
	)
	return container.NewBorder(header, nil, nil, nil, t.list)
}

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
func rowHeight() float32 { return theme.TextSize() + 2*theme.Padding() }

// studentRow is a custom list row supporting double-tap and secondary-tap. It
// uses unpadded canvas.Text (not widget.Label) to avoid the label's built-in
// vertical padding, keeping rows compact.
type studentRow struct {
	widget.BaseWidget
	last, first, in, out *canvas.Text

	onDouble    func()
	onSecondary func(fyne.Position, fyne.KeyModifier)
}

func newStudentRow() *studentRow {
	mk := func() *canvas.Text {
		t := canvas.NewText("", theme.Color(theme.ColorNameForeground))
		t.TextSize = theme.TextSize()
		return t
	}
	r := &studentRow{last: mk(), first: mk(), in: mk(), out: mk()}
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
	c := container.NewStack(bg, grid)
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
