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
// asserted without reaching into its layout. Both widget.Label and canvas.Text
// are read: the step captions are canvas.Text, and missing them would make the
// assertions vacuous.
func allTextOf(o fyne.CanvasObject) []string {
	var out []string
	for _, obj := range test.LaidOutObjects(o) {
		switch w := obj.(type) {
		case *widget.Label:
			out = append(out, w.Text)
		case *canvas.Text:
			out = append(out, w.Text)
		}
	}
	return out
}

// joinedTextOf is allTextOf flattened to one string, for captions that are
// split across several lines and so match no single element.
func joinedTextOf(o fyne.CanvasObject) string {
	return strings.Join(allTextOf(o), " ")
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

	got := allTextOf(view)
	if !strings.Contains(strings.Join(got, " "), scanHint) {
		t.Errorf("card view text = %v, want it to include the intro line", got)
	}
	// Every step's caption has to be on screen: the procedure is only useful
	// if the user can read all four.
	joined := joinedTextOf(view)
	for _, st := range shareSteps {
		if !strings.Contains(joined, st.text) {
			t.Errorf("card view is missing the step %q; text = %q", st.text, joined)
		}
	}
	// The follow-up explains what happens after the last step; without it the
	// browser opening is a surprise.
	if !strings.Contains(joined, shareFollowUp) {
		t.Errorf("card view is missing the follow-up line; text = %q", joined)
	}
}

// Each step needs a picture as well as a caption, and the embedded files have
// to actually decode -- a missing or malformed one would otherwise show as a
// blank space above the text.
func TestShareStepsHaveRenderableIcons(t *testing.T) {
	fa := test.NewApp()
	defer fa.Quit()

	if len(shareSteps) != 3 {
		t.Fatalf("got %d steps, want 3", len(shareSteps))
	}

	for _, st := range shareSteps {
		if st.icon == nil {
			t.Fatalf("step %q has no icon", st.text)
		}
		cell := shareStepCell(st)
		w := fa.NewWindow("step")
		w.SetContent(cell)

		var img *canvas.Image
		for _, obj := range test.LaidOutObjects(cell) {
			if ci, ok := obj.(*canvas.Image); ok {
				img = ci
				break
			}
		}
		if img == nil {
			t.Errorf("step %q shows no image", st.text)
			w.Close()
			continue
		}
		img.Resize(fyne.NewSize(stepIconSide, stepIconSide)) // forces a decode
		if img.Image == nil && img.Resource == nil {
			t.Errorf("step %q icon did not load", st.text)
		}
		w.Close()
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
	drainPendingRenders(t)

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

	// Both clicks left a render in flight; drain them so neither applies after
	// this test returns.
	drainPendingRenders(t)
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

	// The first render is still in flight, and its fyne.Do would otherwise run
	// after this test returns -- measuring text on one goroutine while the next
	// test measures on another, which crashes Fyne's shared text shaper. Drain
	// it here so no render outlives the test that started it.
	drainPendingRenders(t)
}

// drainPendingRenders waits for any in-flight runSingleGeneration goroutine to
// have applied (or discarded) its result, by queueing a no-op through the same
// fyne.Do channel and waiting for it to run. Anything queued earlier has been
// processed by the time this returns.
func drainPendingRenders(t *testing.T) {
	t.Helper()
	// Renders are only queued after minSingleProgressDisplay has elapsed, so
	// give the slowest in-flight one time to reach its fyne.Do before flushing.
	time.Sleep(minSingleProgressDisplay + 150*time.Millisecond)

	done := make(chan struct{})
	fyne.Do(func() { close(done) })
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out draining pending renders")
	}
}

// The steps are joined by arrows, so the row reads as a sequence: one fewer
// arrow than there are steps, and the steps still share the width evenly
// between them rather than the arrows taking a full column each.
func TestShareStepsRowHasArrowsBetweenSteps(t *testing.T) {
	fa := test.NewApp()
	defer fa.Quit()
	fa.Settings().SetTheme(newCompactTheme())

	row := shareStepsRow()
	w := fa.NewWindow("row")
	defer w.Close()
	w.SetContent(row)

	cont, ok := row.(*fyne.Container)
	if !ok {
		t.Fatalf("steps row is %T, want a container", row)
	}
	wantObjs := 2*len(shareSteps) - 1
	if got := len(cont.Objects); got != wantObjs {
		t.Fatalf("row has %d children, want %d (%d steps joined by %d arrows)",
			got, wantObjs, len(shareSteps), len(shareSteps)-1)
	}

	// Arrows sit at the odd positions and must stay narrow; a step is far
	// wider, so a mix-up here shows as a badly stretched arrow.
	row.Resize(fyne.NewSize(600, row.MinSize().Height))
	for i, o := range cont.Objects {
		if i%2 == 1 && o.Size().Width > stepArrowSide*2 {
			t.Errorf("arrow at %d is %v wide; it should keep its own width", i, o.Size().Width)
		}
	}

	// Each arrow must sit level with the step pictures it joins, not with the
	// captions below them. Every image in the row should share one center line.
	var centers []float32
	for _, obj := range test.LaidOutObjects(row) {
		if ci, ok := obj.(*canvas.Image); ok {
			centers = append(centers, ci.Position().Y+ci.Size().Height/2)
		}
	}
	if len(centers) != wantObjs {
		t.Fatalf("found %d images in the row, want %d", len(centers), wantObjs)
	}
	for i, c := range centers {
		if diff := c - centers[0]; diff > 0.5 || diff < -0.5 {
			t.Errorf("image %d is centered at y=%v, but the first is at y=%v; "+
				"the arrows should line up with the step pictures", i, c, centers[0])
		}
	}
}
