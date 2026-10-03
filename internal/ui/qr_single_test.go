package ui

import (
	"image"
	"os"
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"

	"github.com/pglass/checkin/internal/qr"
)

// allTextOf returns the text of every label in o, so a view's wording can be
// asserted without reaching into its layout.
func allTextOf(o fyne.CanvasObject) []string {
	var out []string
	for _, obj := range test.LaidOutObjects(o) {
		if l, ok := obj.(*widget.Label); ok {
			out = append(out, l.Text)
		}
	}
	return out
}

// The single-code view shows the rendered card and tells the user how to get it
// onto their phone. The hint is the whole point of showing the code on screen
// rather than handing over a file, so it is asserted rather than assumed.
func TestQRCardViewShowsImageAndHint(t *testing.T) {
	fa := test.NewApp()
	defer fa.Quit()

	card := image.NewRGBA(image.Rect(0, 0, qr.CardWidth, qr.CardHeight))
	view := qrCardView(card)

	w := fa.NewWindow("card")
	defer w.Close()
	w.SetContent(view)

	var img *canvas.Image
	for _, obj := range test.LaidOutObjects(view) {
		if ci, ok := obj.(*canvas.Image); ok {
			img = ci
			break
		}
	}
	if img == nil {
		t.Fatal("card view shows no image")
	}
	if img.Image != card {
		t.Error("card view is not showing the rendered card")
	}

	if got := allTextOf(view); !containsText(got, scanHint) {
		t.Errorf("card view text = %v, want it to include the scan hint", got)
	}
}

func containsText(all []string, want string) bool {
	for _, s := range all {
		if strings.Contains(s, want) {
			return true
		}
	}
	return false
}

// Showing one student's code must not write anything to disk: the card is
// rendered in memory and displayed. This is what separates the row button from
// the picker's batch path, which does write files.
func TestSingleQRWritesNoFiles(t *testing.T) {
	a := newScanApp(t)
	// newScanApp leaves the Center unnamed, which makes CenterDir fail and
	// would skip the whole check; name it so the directory is a real path.
	a.centerName = "QRSingleTestCenter"

	dir, err := qr.CenterDir(a.centerName)
	if err != nil {
		t.Fatalf("CenterDir: %v", err)
	}
	if dirExists(dir) {
		t.Skipf("%s already exists; cannot tell a new write from an old one", dir)
	}

	w := a.fyneApp.NewWindow("qr")
	defer w.Close()
	a.runSingleGeneration(w, qr.Student{First: "Ann", Last: "Lee", Label: "Lee, Ann"}, qr.DefaultBaseURL)

	// Wait for the card to actually render, or the check would pass simply
	// because nothing had happened yet.
	waitFor(t, func() bool { return containsText(allTextOf(w.Content()), scanHint) })

	if dirExists(dir) {
		t.Errorf("showing one QR code created %s; it should write nothing", dir)
	}
}

// waitFor polls cond until it holds or the deadline passes, so a test can wait
// on the render goroutine without sleeping for a fixed time.
func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("timed out waiting for the card to render")
}

func dirExists(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.IsDir()
}

// Clicking Show Code on several students reuses one window rather than opening
// one per click, and the window retitles to whichever student was asked for.
func TestShowCodeReusesOneWindow(t *testing.T) {
	a := newScanApp(t)

	for _, first := range []string{"Alice", "Bob"} {
		if _, err := a.store.AddStudent(a.ctx, testStudentName(first)); err != nil {
			t.Fatalf("AddStudent(%s): %v", first, err)
		}
	}
	a.refresh()

	a.generateQRForStudent(a.table.rows[0])
	first := a.codeWin
	if first == nil {
		t.Fatal("Show Code opened no window")
	}

	a.generateQRForStudent(a.table.rows[1])
	if a.codeWin != first {
		t.Error("Show Code opened a second window; it should reuse the open one")
	}
	defer a.codeWin.Close()

	// The reused window must name the student just asked for, or it would show
	// one student's code under another's title.
	want := a.table.rows[1].Name.Display()
	if got := a.codeWin.Title(); !strings.Contains(got, want) {
		t.Errorf("window title = %q, want it to name %q", got, want)
	}
}

// A render that finishes after a later click must drop its result, or the
// window could end up showing the wrong student's code.
func TestStaleCodeRenderIsDiscarded(t *testing.T) {
	a := newScanApp(t)
	a.centerName = "StaleRenderTestCenter"

	w := a.fyneApp.NewWindow("qr")
	defer w.Close()

	// Start one render, then supersede it before it can apply.
	a.runSingleGeneration(w, qr.Student{First: "Ann", Last: "Lee", Label: "Lee, Ann"}, qr.DefaultBaseURL)
	stale := a.codeGen
	a.runSingleGeneration(w, qr.Student{First: "Bo", Last: "Ray", Label: "Ray, Bo"}, qr.DefaultBaseURL)

	if a.codeGen == stale {
		t.Fatal("second run did not claim a new generation; the stale guard cannot work")
	}

	// Let both finish; the window should end on the second student's card.
	waitFor(t, func() bool { return containsText(allTextOf(w.Content()), scanHint) })
}
