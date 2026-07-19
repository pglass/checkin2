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

// studentTable renders the main student list with Name / In / Out columns and
// wires double-click (check-in/out) and right-click (remove) per row.
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
			row.onSecondary = func(pos fyne.Position) { t.app.showRemoveDialog(r) }
		},
	)
	return t
}

func (t *studentTable) widget() fyne.CanvasObject {
	header := container.NewGridWithColumns(3,
		boldLabel("Name"), boldLabel("Check In"), boldLabel("Check Out"),
	)
	return container.NewBorder(header, nil, nil, nil, t.list)
}

func (t *studentTable) setRows(rows []store.StudentRow) {
	t.rows = rows
	t.list.Refresh()
}

func boldLabel(s string) *widget.Label {
	l := widget.NewLabel(s)
	l.TextStyle = fyne.TextStyle{Bold: true}
	return l
}

// studentRow is a custom list row supporting double-tap and secondary-tap.
type studentRow struct {
	widget.BaseWidget
	name, in, out *widget.Label

	onDouble    func()
	onSecondary func(fyne.Position)
}

func newStudentRow() *studentRow {
	r := &studentRow{
		name: widget.NewLabel(""),
		in:   widget.NewLabel(""),
		out:  widget.NewLabel(""),
	}
	r.ExtendBaseWidget(r)
	return r
}

func (r *studentRow) update(row store.StudentRow) {
	r.name.SetText(row.Name)
	r.in.SetText(timeFmt(row.In))
	r.out.SetText(timeFmt(row.Out))
}

func (r *studentRow) CreateRenderer() fyne.WidgetRenderer {
	bg := canvas.NewRectangle(color.Transparent)
	grid := container.NewGridWithColumns(3, r.name, r.in, r.out)
	c := container.NewStack(bg, grid)
	return widget.NewSimpleRenderer(c)
}

// DoubleTapped implements fyne.DoubleTappable.
func (r *studentRow) DoubleTapped(_ *fyne.PointEvent) {
	if r.onDouble != nil {
		r.onDouble()
	}
}

// Tapped implements fyne.Tappable (needed so DoubleTapped fires).
func (r *studentRow) Tapped(_ *fyne.PointEvent) {}

// TappedSecondary implements fyne.SecondaryTappable (right-click).
func (r *studentRow) TappedSecondary(e *fyne.PointEvent) {
	if r.onSecondary != nil {
		r.onSecondary(e.AbsolutePosition)
	}
}

var _ fyne.Tappable = (*studentRow)(nil)
var _ fyne.DoubleTappable = (*studentRow)(nil)
var _ fyne.SecondaryTappable = (*studentRow)(nil)

// keep theme import used for potential future styling
var _ = theme.Color
var _ = desktop.MouseButtonPrimary
