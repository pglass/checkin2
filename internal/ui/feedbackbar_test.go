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

	if got, want := b.text.Text, "Alice checked in at 3:04 PM"; got != want {
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

	if got, want := b.text.Text, "Bob checked out at 9:30 AM"; got != want {
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
	if got, want := b.text.Text, "Bob checked out at "; len(got) < len(want) || got[:len(want)] != want {
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
	if got, want := a.feedback.text.Text, "Alice Aliceson checked in at "; len(got) < len(want) || got[:len(want)] != want {
		t.Errorf("text = %q, want it to start %q", got, want)
	}

	a.applyCheckInOut(row, "Parent")
	if got, want := a.feedback.text.Text, "Alice Aliceson checked out at "; len(got) < len(want) || got[:len(want)] != want {
		t.Errorf("text = %q, want it to start %q", got, want)
	}
}
