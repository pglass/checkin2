package ui

import (
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"

	"github.com/pglass/checkin/internal/store"
)

func tp(t time.Time) *time.Time { return &t }

// The summary is one sentence: total students, then today's check-in and
// check-out counts.
func TestStatusBarSummary(t *testing.T) {
	b := newStatusBar()
	now := time.Now()

	b.setRows([]store.StudentRow{
		{Name: "In only", In: tp(now)},
		{Name: "In and out", In: tp(now), Out: tp(now)},
		{Name: "Not in"},
		{Name: "Also not in"},
	})

	// Two students have an In time; one of them also has an Out.
	want := "4 total students. 2 check-ins, 1 check-out today"
	if got := b.summary.Text; got != want {
		t.Errorf("summary = %q, want %q", got, want)
	}
}

// Singular and zero forms read correctly.
func TestStatusBarPluralisation(t *testing.T) {
	b := newStatusBar()

	b.setRows(nil)
	if got, want := b.summary.Text, "0 total students. 0 check-ins, 0 check-outs today"; got != want {
		t.Errorf("summary = %q, want %q", got, want)
	}

	b.setRows([]store.StudentRow{{Name: "Solo", In: tp(time.Now())}})
	if got, want := b.summary.Text, "1 total student. 1 check-in, 0 check-outs today"; got != want {
		t.Errorf("summary = %q, want %q", got, want)
	}
}

// The clock is the local-time "Jan 1, 2026 HH:MM:SS" form.
func TestStatusBarClockFormat(t *testing.T) {
	b := newStatusBar()
	b.setClock(time.Date(2026, time.January, 1, 9, 5, 3, 0, time.Local))

	if got, want := b.clock.Text, "Jan 1, 2026 09:05:03"; got != want {
		t.Errorf("clock = %q, want %q", got, want)
	}
}

// The camera cell reads On or Off.
func TestStatusBarCamera(t *testing.T) {
	b := newStatusBar()

	b.setCamera(false)
	if got, want := b.camera.Text, "Camera Off"; got != want {
		t.Errorf("camera = %q, want %q", got, want)
	}
	b.setCamera(true)
	if got, want := b.camera.Text, "Camera On"; got != want {
		t.Errorf("camera = %q, want %q", got, want)
	}
}

// Three groups: camera pinned left, summary centred, clock pinned right.
func TestStatusBarGrouping(t *testing.T) {
	b := newStatusBar()
	b.setRows([]store.StudentRow{{Name: "A"}})
	b.setClock(time.Date(2026, time.January, 1, 0, 0, 0, 0, time.Local))
	b.setCamera(true)

	w := b.widget()
	// Lay the bar out wide so the three groups are clearly separated.
	w.Resize(fyne.NewSize(800, 24))

	cam := posOf(t, w, b.camera)
	sum := posOf(t, w, b.summary)
	clk := posOf(t, w, b.clock)

	if !(cam < sum && sum < clk) {
		t.Fatalf("order left-to-right = camera %.1f, summary %.1f, clock %.1f; want camera < summary < clock",
			cam, sum, clk)
	}
	// The summary is centred, not merely between the two: its midpoint should
	// sit near the bar's midpoint.
	mid := sum + b.summary.MinSize().Width/2
	if diff := mid - 400; diff > 30 || diff < -30 {
		t.Errorf("summary centre = %.1f, want near 400 (bar is 800 wide)", mid)
	}
	// Camera hugs the left edge and the clock the right.
	if cam > 30 {
		t.Errorf("camera x = %.1f, want it pinned near the left edge", cam)
	}
	if right := clk + b.clock.MinSize().Width; right < 800-30 {
		t.Errorf("clock right edge = %.1f, want it pinned near 800", right)
	}
}

// posOf returns the x position of obj within the laid-out tree rooted at root.
func posOf(t *testing.T, root fyne.CanvasObject, obj *canvas.Text) float32 {
	t.Helper()
	var walk func(o fyne.CanvasObject, offset float32) (float32, bool)
	walk = func(o fyne.CanvasObject, offset float32) (float32, bool) {
		if o == obj {
			return offset + o.Position().X, true
		}
		if c, ok := o.(*fyne.Container); ok {
			for _, child := range c.Objects {
				if x, found := walk(child, offset+c.Position().X); found {
					return x, true
				}
			}
		}
		return 0, false
	}
	x, found := walk(root, 0)
	if !found {
		t.Fatal("object not found in the laid-out tree")
	}
	return x
}

// The bar is wired into refresh: checking a student in through the app updates
// the summary without any explicit status call.
func TestStatusBarUpdatesOnRefresh(t *testing.T) {
	a := newScanApp(t, false)
	a.status = newStatusBar()

	st, err := a.store.AddStudent(a.ctx, "Alice")
	if err != nil {
		t.Fatal(err)
	}
	a.refresh()
	if got, want := a.status.summary.Text, "1 total student. 0 check-ins, 0 check-outs today"; got != want {
		t.Errorf("summary = %q, want %q", got, want)
	}

	// applyCheckInOut refreshes internally.
	a.applyCheckInOut(store.StudentRow{ID: st.ID, Name: st.Name})

	if got, want := a.status.summary.Text, "1 total student. 1 check-in, 0 check-outs today"; got != want {
		t.Errorf("after check-in: summary = %q, want %q", got, want)
	}
	// No camera in this test app.
	if got, want := a.status.camera.Text, "Camera Off"; got != want {
		t.Errorf("camera = %q, want %q", got, want)
	}
}

// Starting a camera must flip the status bar to "Camera On". stopCamera (which
// startCameraDevice calls first) updates the bar while camDevice is still
// deviceNone, so the start path needs its own update or the bar stays "Off"
// with a camera running.
func TestStatusBarCameraFollowsDeviceStart(t *testing.T) {
	a := newScanApp(t, false)
	a.status = newStatusBar()
	a.updateStatus(nil)

	if got, want := a.status.camera.Text, "Camera Off"; got != want {
		t.Fatalf("camera = %q, want %q before any camera starts", got, want)
	}

	// Start device 0. There is no real webcam under test, so Camera.Run fails
	// in its goroutine -- but camDevice is set synchronously, which is what the
	// status bar reflects.
	a.startCameraDevice(a.ctx, 0)

	if got, want := a.status.camera.Text, "Camera On"; got != want {
		t.Errorf("camera = %q, want %q after starting a device", got, want)
	}

	a.stopCamera()
	if got, want := a.status.camera.Text, "Camera Off"; got != want {
		t.Errorf("camera = %q, want %q after stopping", got, want)
	}
}
