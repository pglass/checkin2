package ui

import (
	"image/color"
	"testing"
	"time"

	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"

	"github.com/pglass/checkin/internal/store"
)

// alphaOf returns a colour's alpha channel, 0-255.
func alphaOf(c color.Color) uint8 {
	_, _, _, a := c.RGBA()
	return uint8(a >> 8)
}

// A check-in flashes green with the student's name and time.
func TestFeedbackBarCheckInMessage(t *testing.T) {
	fa := test.NewApp()
	defer fa.Quit()

	b := newFeedbackBar()
	at := time.Date(2026, time.January, 1, 15, 4, 0, 0, time.Local)

	b.showCheckIn("Alice", at)

	if got, want := b.message(), "Alice checked in at 3:04 PM"; got != want {
		t.Errorf("text = %q, want %q", got, want)
	}
	if b.fillColor() != theme.Color(theme.ColorNameSuccess) {
		t.Errorf("fill = %v, want the theme success colour", b.fillColor())
	}
}

// A check-out uses the same shape with "checked out".
func TestFeedbackBarCheckOutMessage(t *testing.T) {
	fa := test.NewApp()
	defer fa.Quit()

	b := newFeedbackBar()
	at := time.Date(2026, time.January, 1, 9, 30, 0, 0, time.Local)

	b.showCheckOut("Bob", at)

	if got, want := b.message(), "Bob checked out at 9:30 AM"; got != want {
		t.Errorf("text = %q, want %q", got, want)
	}
	if b.fillColor() != theme.Color(theme.ColorNameSuccess) {
		t.Errorf("fill = %v, want the theme success colour", b.fillColor())
	}
}

// The result is held at full colour, then both the colour and the text fade
// out, leaving the bar empty rather than showing a stale message forever.
func TestFeedbackBarHoldsThenFades(t *testing.T) {
	fa := test.NewApp()
	defer fa.Quit()

	b := newFeedbackBar()
	b.showCheckIn("Alice", time.Now())

	// Still fully visible partway through the hold.
	time.Sleep(feedbackHold / 2)
	if alphaOf(b.fillColor()) == 0 {
		t.Error("colour cleared during the hold window")
	}
	if alphaOf(b.textColor()) == 0 {
		t.Error("text faded during the hold window")
	}

	// After hold + fade, both are transparent.
	deadline := time.Now().Add(feedbackHold + feedbackFade + 3*time.Second)
	for time.Now().Before(deadline) {
		if alphaOf(b.fillColor()) == 0 && alphaOf(b.textColor()) == 0 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if a := alphaOf(b.fillColor()); a != 0 {
		t.Errorf("background alpha = %d after hold+fade, want 0", a)
	}
	if a := alphaOf(b.textColor()); a != 0 {
		t.Errorf("text alpha = %d after hold+fade, want 0", a)
	}
}

// A new result during the hold or fade replaces the old one immediately and
// restarts the timing, rather than inheriting the old one's remaining time.
func TestFeedbackBarNewResultResetsTiming(t *testing.T) {
	fa := test.NewApp()
	defer fa.Quit()

	b := newFeedbackBar()
	b.showCheckIn("Alice", time.Now())
	time.Sleep(200 * time.Millisecond)
	b.showCheckOut("Bob", time.Now())

	// The first flash's hold expiry is stale now, so a correct implementation
	// ignores it. Deterministic, unlike waiting on the real timer.
	b.startFade(1, theme.Color(theme.ColorNameSuccess), theme.Color(theme.ColorNameForeground))

	if alphaOf(b.fillColor()) == 0 {
		t.Error("a stale hold expiry started fading the current result")
	}
	if got, want := b.message(), "Bob checked out at "; len(got) < len(want) || got[:len(want)] != want {
		t.Errorf("text = %q, want it to show the second scan", got)
	}
}

// A scan arriving mid-fade restores full opacity rather than continuing to
// fade out.
func TestFeedbackBarNewResultDuringFadeIsOpaque(t *testing.T) {
	fa := test.NewApp()
	defer fa.Quit()

	b := newFeedbackBar()
	b.showCheckIn("Alice", time.Now())

	// Let the hold expire and the fade get under way.
	time.Sleep(feedbackHold + feedbackFade/3)
	if a := alphaOf(b.fillColor()); a == 255 {
		t.Skip("fade had not visibly started; timing-dependent, skipping")
	}

	b.showCheckOut("Bob", time.Now())

	if a := alphaOf(b.fillColor()); a != 255 {
		t.Errorf("background alpha = %d after a new scan mid-fade, want 255", a)
	}
	if a := alphaOf(b.textColor()); a != 255 {
		t.Errorf("text alpha = %d after a new scan mid-fade, want 255", a)
	}
}

// The bar is wired into applyCheckInOut, so it reports scans and dialog
// confirmations alike.
func TestFeedbackBarWiredToCheckInOut(t *testing.T) {
	a := newScanApp(t)
	a.feedback = newFeedbackBar()

	st, err := a.store.AddStudent(a.ctx, testStudentName("Alice"))
	if err != nil {
		t.Fatal(err)
	}
	row := store.StudentRow{ID: st.ID, Name: testStudentNameOf(st)}

	a.applyCheckInOut(row, "Parent")
	if got, want := a.feedback.message(), "Alice Aliceson checked in at "; len(got) < len(want) || got[:len(want)] != want {
		t.Errorf("text = %q, want it to start %q", got, want)
	}

	a.applyCheckInOut(row, "Parent")
	if got, want := a.feedback.message(), "Alice Aliceson checked out at "; len(got) < len(want) || got[:len(want)] != want {
		t.Errorf("text = %q, want it to start %q", got, want)
	}
}

// feedbackHint picks the message from the three things it depends on.
func TestFeedbackHint(t *testing.T) {
	cases := []struct {
		name          string
		kiosk         bool
		students      int
		cameraRunning bool
		want          string
	}{
		{"empty center, camera on", false, 0, true, noStudentsMsg},
		{"empty center, camera off", false, 0, false, noStudentsMsg},
		{"students, camera on", false, 3, true, idleMsgWithCamera},
		{"students, camera off", false, 3, false, idleMsgWithoutCamera},
		// Kiosk mode has its own on-screen prompt and no list to double-click.
		{"kiosk with students", true, 3, true, ""},
		{"kiosk empty", true, 0, false, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := feedbackHint(tc.kiosk, tc.students, tc.cameraRunning); got != tc.want {
				t.Errorf("feedbackHint = %q, want %q", got, tc.want)
			}
		})
	}
}

// The exact wording of both idle hints and the empty-center notice.
func TestFeedbackHintWording(t *testing.T) {
	if got, want := noStudentsMsg,
		"No students found. Use 'File > Add Student..' or 'File > Import...' to add students."; got != want {
		t.Errorf("no-students message = %q, want %q", got, want)
	}
	if got, want := idleMsgWithCamera,
		"Scan a QR code or double-click a student to check in or out"; got != want {
		t.Errorf("idle message = %q, want %q", got, want)
	}
	if got, want := idleMsgWithoutCamera,
		"Double-click a student to check-in or out. (Camera not available for QR code scans)"; got != want {
		t.Errorf("no-camera idle message = %q, want %q", got, want)
	}
}

// An empty Center says so as soon as it is opened, without waiting out the idle
// delay: there is nothing to have flashed yet.
func TestFeedbackBarHintShowsImmediatelyWhenIdle(t *testing.T) {
	fa := test.NewApp()
	defer fa.Quit()

	b := newFeedbackBar()
	b.setHint(noStudentsMsg)

	if got := b.message(); got != noStudentsMsg {
		t.Errorf("text = %q, want the hint painted at once", got)
	}
	// Hints are the resting state, not an event: no coloured band behind them.
	if alphaOf(b.fillColor()) != 0 {
		t.Errorf("hint has a background fill (alpha %d), want none", alphaOf(b.fillColor()))
	}
	if alphaOf(b.textColor()) == 0 {
		t.Error("hint text is transparent, want it visible")
	}
}

// A flash owns the bar: a hint set while a result is showing waits rather than
// wiping the result off the screen.
func TestFeedbackBarHintDoesNotInterruptFlash(t *testing.T) {
	fa := test.NewApp()
	defer fa.Quit()

	b := newFeedbackBar()
	b.showCheckIn("Alice", time.Now())
	b.setHint(idleMsgWithCamera)

	if got, want := b.message(), "Alice checked in at "; len(got) < len(want) || got[:len(want)] != want {
		t.Errorf("text = %q, want the check-in still showing", got)
	}
}

// After the idle delay with nothing else to show, the bar falls back to the
// hint.
func TestFeedbackBarShowsHintAfterIdleDelay(t *testing.T) {
	fa := test.NewApp()
	defer fa.Quit()

	restore := feedbackIdleDelay
	feedbackIdleDelay = 150 * time.Millisecond
	defer func() { feedbackIdleDelay = restore }()

	b := newFeedbackBar()
	b.setHint(idleMsgWithCamera)
	b.showCheckIn("Alice", time.Now())

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if b.message() == idleMsgWithCamera {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if got := b.message(); got != idleMsgWithCamera {
		t.Errorf("text = %q, want the idle hint after the delay", got)
	}
	// The hint replaced the flash rather than the flash fading to nothing.
	if alphaOf(b.textColor()) == 0 {
		t.Error("hint text is transparent, want it visible")
	}
}

// A second check-in restarts the idle clock, so a busy period never drops the
// hint on top of a fresh result.
func TestFeedbackBarFlashRestartsIdleClock(t *testing.T) {
	fa := test.NewApp()
	defer fa.Quit()

	restore := feedbackIdleDelay
	feedbackIdleDelay = 300 * time.Millisecond
	defer func() { feedbackIdleDelay = restore }()

	b := newFeedbackBar()
	b.setHint(idleMsgWithCamera)

	b.showCheckIn("Alice", time.Now())
	time.Sleep(200 * time.Millisecond) // most of the way through the first delay
	b.showCheckOut("Bob", time.Now())

	// The first flash's idle expiry is now stale; had it not been restarted it
	// would fire here and replace Bob with the hint.
	time.Sleep(150 * time.Millisecond)
	if got, want := b.message(), "Bob checked out at "; len(got) < len(want) || got[:len(want)] != want {
		t.Errorf("text = %q, want Bob's result still showing", got)
	}
}

// Opening a Center with no students puts the notice on the bar via refresh.
func TestAppShowsNoStudentsHint(t *testing.T) {
	a := newScanApp(t)
	a.feedback = newFeedbackBar()

	a.refresh()

	if got := a.feedback.message(); got != noStudentsMsg {
		t.Errorf("text = %q, want the no-students notice", got)
	}
}

// Adding a student swaps the notice for the idle hint on the next refresh.
func TestAppHintSwitchesWhenStudentsExist(t *testing.T) {
	a := newScanApp(t)
	a.feedback = newFeedbackBar()

	a.refresh()
	if got := a.feedback.message(); got != noStudentsMsg {
		t.Fatalf("text = %q, want the no-students notice first", got)
	}

	if _, err := a.store.AddStudent(a.ctx, testStudentName("Alice")); err != nil {
		t.Fatal(err)
	}
	a.refresh()

	// newScanApp runs no camera, so this is the no-camera wording.
	if got := a.feedback.message(); got != idleMsgWithoutCamera {
		t.Errorf("text = %q, want the no-camera idle hint", got)
	}
}
