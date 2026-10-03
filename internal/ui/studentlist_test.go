package ui

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"

	"github.com/pglass/checkin/internal/store"
)

// gridTexts returns the text of every canvas.Text in o, in layout order.
func gridTexts(o fyne.CanvasObject) []string {
	var out []string
	for _, obj := range test.LaidOutObjects(o) {
		if t, ok := obj.(*canvas.Text); ok {
			out = append(out, t.Text)
		}
	}
	return out
}

// The main list's columns are Last Name, First Name, Check In, Check Out --
// last name first, matching the roster sort order.
func TestStudentTableHeaderColumns(t *testing.T) {
	a := newScanApp(t)

	got := gridTexts(a.table.widget())
	want := []string{"Last Name", "First Name", "Check In", "Check Out"}
	if len(got) < len(want) {
		t.Fatalf("header columns = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("header columns = %v, want %v", got[:len(want)], want)
		}
	}
}

// A row puts the last name in the first column and the first name in the
// second, so the list reads in the same order as the header.
func TestStudentRowSplitsNameColumns(t *testing.T) {
	fa := test.NewApp()
	defer fa.Quit()

	r := newStudentRow()
	at := time.Date(2026, 7, 19, 8, 30, 0, 0, time.Local)
	r.update(store.StudentRow{
		Name: store.Name{First: "John", Last: "Smith"},
		In:   &at,
	})

	if r.last.Text != "Smith" {
		t.Errorf("last name cell = %q, want %q", r.last.Text, "Smith")
	}
	if r.first.Text != "John" {
		t.Errorf("first name cell = %q, want %q", r.first.Text, "John")
	}
	if r.in.Text == "" {
		t.Error("check-in cell is empty")
	}
	if r.out.Text != timeFmt(nil) {
		t.Errorf("check-out cell = %q, want the empty-time form %q", r.out.Text, timeFmt(nil))
	}
}

// Every row carries a "Get QR Code" button with the embedded Material Symbols
// glyph on it, so one code can be generated without opening the picker.
func TestStudentRowHasQRButton(t *testing.T) {
	fa := test.NewApp()
	defer fa.Quit()

	r := newStudentRow()
	if r.qr == nil {
		t.Fatal("row has no QR button")
	}
	if !strings.Contains(r.qr.Text, qrButtonText) {
		t.Errorf("QR button label = %q, want it to contain %q", r.qr.Text, qrButtonText)
	}
	// The glyph is stacked over the button rather than set as its icon, so the
	// button's own icon slot is deliberately empty.
	if r.qr.Icon != nil {
		t.Error("QR button has an icon set; the glyph should be the separately placed overlay")
	}
	if r.icon == nil {
		t.Fatal("row has no QR glyph")
	}
	if r.icon.Resource != qrCodeIconResource {
		t.Error("row glyph is not the embedded QR icon")
	}
}

// The embedded icon has to be an SVG that Fyne actually recognizes as one:
// that is what gets it rasterized at device resolution rather than resampled
// from a fixed-size bitmap. Fyne sniffs the format from the resource's name and
// its first bytes, so a wrong extension or a stray leading blank line would
// silently cost the sharpness the SVG was adopted for.
//
// Fyne's own svg package is internal, so the check goes through the public
// canvas.Image, which is what the button builds underneath anyway: resizing it
// drives the same renderSVG path, and a malformed path or an element oksvg
// cannot handle surfaces there rather than at the first repaint.
func TestQRCodeIconIsRenderableSVG(t *testing.T) {
	if ext := filepath.Ext(qrCodeIconResource.Name()); ext != ".svg" {
		t.Errorf("icon resource name = %q, want a .svg extension so Fyne sniffs it as SVG",
			qrCodeIconResource.Name())
	}
	if head := strings.ToLower(string(qrCodeIconSVG[:5])); head != "<?xml" && head != "<svg " && head != "<!doc" {
		t.Errorf("icon content starts %q, which Fyne does not sniff as SVG", head)
	}

	fa := test.NewApp()
	defer fa.Quit()

	img := canvas.NewImageFromResource(qrCodeIconResource)
	w := fa.NewWindow("icon")
	defer w.Close()
	w.SetContent(img)
	img.Resize(fyne.NewSize(20, 20)) // rasterizes; logs and bails on a bad SVG

	if img.Image == nil {
		t.Fatal("embedded QR icon did not rasterize")
	}
	if b := img.Image.Bounds(); b.Dx() == 0 || b.Dy() == 0 {
		t.Fatalf("embedded QR icon rasterized to %dx%d", b.Dx(), b.Dy())
	}
}

// Tapping a row's button generates for that student alone. The row's callback
// is rebound on every list update (widget.List recycles rows), so the binding
// is checked through the table rather than on a bare row.
func TestRowQRButtonTargetsThatStudent(t *testing.T) {
	a := newScanApp(t)

	for _, first := range []string{"Alice", "Bob"} {
		if _, err := a.store.AddStudent(a.ctx, testStudentName(first)); err != nil {
			t.Fatalf("AddStudent(%s): %v", first, err)
		}
	}
	a.refresh()
	if len(a.table.rows) != 2 {
		t.Fatalf("table has %d rows, want 2", len(a.table.rows))
	}

	var got []store.StudentRow
	a.table.onQR = func(r store.StudentRow) { got = append(got, r) }

	// Drive the list's update function the way widget.List does, then tap the
	// button, for each row in turn.
	row := newStudentRow()
	for i := range a.table.rows {
		a.table.list.UpdateItem(i, row)
		row.qr.OnTapped()
	}

	if len(got) != 2 || got[0].ID != a.table.rows[0].ID || got[1].ID != a.table.rows[1].ID {
		t.Fatalf("buttons generated for %v, want one per row in order", got)
	}
}

// Adding the QR button must not have made the list taller: the row height is
// still text plus the theme's padding, exactly as it was when a row held only
// the four text columns.
func TestRowHeightUnaffectedByQRButton(t *testing.T) {
	fa := test.NewApp()
	defer fa.Quit()
	fa.Settings().SetTheme(newCompactTheme())

	want := theme.TextSize() + 2*theme.Padding()
	if got := rowHeight(); got != want {
		t.Errorf("rowHeight() = %v, want %v (text + 2*padding, the pre-button height)", got, want)
	}

	r := newStudentRow()
	w := fa.NewWindow("row")
	defer w.Close()
	w.SetContent(r)
	if got := r.MinSize().Height; got != want {
		t.Errorf("row MinSize height = %v, want %v -- the button is forcing the row taller", got, want)
	}
}

// The button has to fit inside that row height, or it gets clipped. This is
// what holds the rowButtonTheme sizes honest: bump the icon or text size too
// far and this fails rather than silently cropping the glyph on every row.
func TestRowButtonFitsRow(t *testing.T) {
	fa := test.NewApp()
	defer fa.Quit()
	fa.Settings().SetTheme(newCompactTheme())

	r := newStudentRow()
	ov := container.NewThemeOverride(r.qr, rowButtonTheme)
	w := fa.NewWindow("button")
	defer w.Close()
	w.SetContent(ov)

	if got := r.qr.MinSize().Height; got > rowHeight() {
		t.Errorf("themed row button needs %v but the row is only %v tall; it will be clipped",
			got, rowHeight())
	}

	// The glyph is nudged down by qrIconNudgeY, so its bottom edge is the thing
	// that can run past the row; overshoot it and the icon is cropped.
	if bottom := qrIconInsetY() + rowButtonIconSize; bottom > rowHeight() {
		t.Errorf("QR glyph reaches %v but the row is only %v tall; it will be clipped",
			bottom, rowHeight())
	}
	if qrIconInsetY() < 0 {
		t.Errorf("QR glyph offset is %v; it would be cropped at the top", qrIconInsetY())
	}
}
