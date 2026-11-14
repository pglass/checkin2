package ui

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
)

// precheckFixture is a pre-check over one available backup directory and one
// on an "unplugged drive" (its parent does not exist), with the countdown
// stepped by hand.
type precheckFixture struct {
	p        *backupPrecheck
	note     string
	gone     string
	proceeds int
	// tick is the countdown's pending tick, run by the test in place of the
	// one-second timer; nil when none is scheduled.
	tick    func()
	stopped bool
}

func newPrecheckFixture(t *testing.T) *precheckFixture {
	t.Helper()
	return newPrecheckFixtureWithNote(t, "")
}

// newPrecheckFixtureWithNote is newPrecheckFixture for an automatic backup,
// whose dialog opens with note.
func newPrecheckFixtureWithNote(t *testing.T, note string) *precheckFixture {
	t.Helper()
	fa := test.NewApp()
	t.Cleanup(fa.Quit)
	win := fa.NewWindow("startup")

	f := &precheckFixture{gone: filepath.Join(t.TempDir(), "USB", "backups"), note: note}
	f.p = checkBackupDirs(win, t.TempDir(), []string{t.TempDir(), f.gone}, f.note, func() { f.proceeds++ })
	if f.p == nil {
		t.Fatal("no pre-check dialog with a backup directory unavailable")
	}
	f.p.after = func(d time.Duration, fn func()) func() {
		if d != time.Second {
			t.Errorf("tick scheduled after %v, want 1s", d)
		}
		f.tick = fn
		return func() { f.stopped = true }
	}
	return f
}

// connect makes the unplugged drive's directory reachable.
func (f *precheckFixture) connect(t *testing.T) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(f.gone), 0o755); err != nil {
		t.Fatal(err)
	}
}

// runTick runs the pending countdown tick.
func (f *precheckFixture) runTick(t *testing.T) {
	t.Helper()
	if f.tick == nil {
		t.Fatal("no countdown tick scheduled")
	}
	fn := f.tick
	f.tick = nil
	fn()
}

func (f *precheckFixture) text() string { return segmentsText(f.p.body.Segments) }

// buttonLabels are the dialog's buttons, left to right.
func (f *precheckFixture) buttonLabels() []string {
	var out []string
	for _, o := range f.p.buttons.Objects {
		if b, ok := o.(*widget.Button); ok {
			out = append(out, b.Text)
		}
	}
	return out
}

// With every backup directory available there is nothing to warn about, and
// the backup starts straight away.
func TestPrecheckAllAvailableProceedsImmediately(t *testing.T) {
	fa := test.NewApp()
	t.Cleanup(fa.Quit)

	proceeds := 0
	p := checkBackupDirs(fa.NewWindow("w"), t.TempDir(), []string{t.TempDir()}, "", func() { proceeds++ })
	if p != nil {
		t.Error("pre-check dialog shown with every directory available")
	}
	if proceeds != 1 {
		t.Errorf("proceeded %d times, want 1", proceeds)
	}
}

// An unavailable directory is listed with the instructions, the count of
// locations the backup will still reach, and Refresh, Cancel and Proceed.
func TestPrecheckWarnsAboutUnavailableDirs(t *testing.T) {
	f := newPrecheckFixture(t)

	if f.proceeds != 0 {
		t.Fatal("backup started before the user chose")
	}
	got := f.text()
	for _, want := range []string{
		"Warning: The following backup location(s) are not available:\n",
		"- " + f.gone + "\n",
		"connect the external drive and press Refresh",
		// The internal directory plus the one available backup directory.
		"Press 'Proceed' to backup into 2 other available backup location(s).",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("message = %q, want it to contain %q", got, want)
		}
	}
	if got, want := f.buttonLabels(), []string{"Refresh", "Cancel", "Proceed"}; !slices.Equal(got, want) {
		t.Errorf("buttons = %q, want %q", got, want)
	}
}

// The path is shown small and monospace, the lead-in bold.
func TestPrecheckWarningStyles(t *testing.T) {
	segs := precheckWarningSegments([]string{"/x"}, 0, 1)
	lead := segs[0].(*widget.ParagraphSegment).Texts[0].(*widget.TextSegment)
	if lead.Text != "Warning" || !lead.Style.TextStyle.Bold {
		t.Errorf("lead-in = %q (bold %v), want bold Warning", lead.Text, lead.Style.TextStyle.Bold)
	}
	// The dash shares the path's style, so the two line up (a ListSegment's
	// bullet is drawn at full size and sits high beside small text).
	item := segs[1].(*widget.ParagraphSegment).Texts[0].(*widget.TextSegment)
	if item.Text != "- /x" || !item.Style.TextStyle.Monospace || item.Style.SizeName != precheckPathEnd.SizeName {
		t.Errorf("path = %q with style %+v, want small monospace \"- /x\"", item.Text, item.Style)
	}
}

// A Refresh that finds the drive still missing says how many refreshes have
// run, so the press visibly did something.
func TestPrecheckRefreshStillUnavailableCounts(t *testing.T) {
	f := newPrecheckFixture(t)

	f.p.refreshBtn.OnTapped()
	if want := "not available after 1 refresh(es):"; !strings.Contains(f.text(), want) {
		t.Errorf("message = %q, want %q", f.text(), want)
	}
	f.p.refreshBtn.OnTapped()
	if want := "not available after 2 refresh(es):"; !strings.Contains(f.text(), want) {
		t.Errorf("message = %q, want %q", f.text(), want)
	}
	if f.proceeds != 0 || f.tick != nil {
		t.Error("backup or countdown started with a directory still unavailable")
	}
}

// Once a Refresh finds everything available, the dialog counts down from 4
// with only Cancel, then starts the backup by itself.
func TestPrecheckRefreshAllAvailableCountsDown(t *testing.T) {
	f := newPrecheckFixture(t)
	f.connect(t)
	f.p.refreshBtn.OnTapped()

	if got, want := f.buttonLabels(), []string{"Cancel"}; !slices.Equal(got, want) {
		t.Errorf("buttons = %q, want %q", got, want)
	}
	for _, n := range []string{"4 seconds", "3 seconds", "2 seconds", "1 second."} {
		got := f.text()
		if !strings.Contains(got, "Refreshed: All backup locations are available.") ||
			!strings.Contains(got, "Starting backup in "+n) {
			t.Errorf("message = %q, want the countdown at %s", got, n)
		}
		if f.proceeds != 0 {
			t.Fatalf("backup started with %s left", n)
		}
		f.runTick(t)
	}
	if f.proceeds != 1 {
		t.Errorf("proceeded %d times after the countdown, want 1", f.proceeds)
	}
	if f.tick != nil {
		t.Error("countdown kept ticking after the backup started")
	}
}

// Cancel during the countdown stops it: no backup, and a tick already on its
// way does nothing.
func TestPrecheckCancelDuringCountdown(t *testing.T) {
	f := newPrecheckFixture(t)
	f.connect(t)
	f.p.refreshBtn.OnTapped()
	pending := f.tick

	f.p.cancelBtn.OnTapped()
	if !f.stopped {
		t.Error("cancel did not stop the countdown timer")
	}
	pending() // a tick that fired just as Cancel was pressed
	if f.proceeds != 0 {
		t.Error("backup started after Cancel")
	}
}

// Proceed starts the backup once, skipping the unavailable directory.
func TestPrecheckProceed(t *testing.T) {
	f := newPrecheckFixture(t)
	f.p.proceedBtn.OnTapped()
	f.p.proceedBtn.OnTapped()
	if f.proceeds != 1 {
		t.Errorf("proceeded %d times, want 1", f.proceeds)
	}
}

// Cancel closes the dialog without backing up.
func TestPrecheckCancel(t *testing.T) {
	f := newPrecheckFixture(t)
	f.p.cancelBtn.OnTapped()
	if f.proceeds != 0 {
		t.Error("backup started after Cancel")
	}
	if f.p.parent.Canvas().Overlays().Top() != nil {
		t.Error("dialog still showing after Cancel")
	}
}

// The countdown pluralises its unit.
func TestPrecheckCountdownText(t *testing.T) {
	if got := segmentsText(precheckCountdownSegments(1)); !strings.Contains(got, "in 1 second...") {
		t.Errorf("text = %q", got)
	}
}

// Each part of the message ends its line, with blank lines before the
// instructions and before the Proceed explanation. RichText only breaks after a
// non-inline segment, so this is checked on the segments themselves.
func TestPrecheckWarningLineBreaks(t *testing.T) {
	got := segmentsText(precheckWarningSegments([]string{"/a", "/b"}, 0, 2))
	want := "Warning: The following backup location(s) are not available:\n" +
		"- /a\n- /b\n" +
		"\nIf any are located on an external drive, connect the external drive and press Refresh.\n" +
		"\nPress 'Proceed'"
	if !strings.HasPrefix(got, want) {
		t.Errorf("text = %q, want it to start %q", got, want)
	}
	for _, s := range precheckWarningSegments([]string{"/a"}, 0, 2) {
		if s.Inline() {
			t.Errorf("top-level segment %q is inline, so the next part would run on", s.Textual())
		}
		if p, ok := s.(*widget.ParagraphSegment); ok && p.Texts[len(p.Texts)-1].Inline() {
			t.Errorf("paragraph %q ends inline, so it does not end its line", s.Textual())
		}
	}
	if got := segmentsText(precheckCountdownSegments(4)); got !=
		"Refreshed: All backup locations are available.\n\nStarting backup in 4 seconds...\n" {
		t.Errorf("countdown text = %q", got)
	}
}

// An automatic backup's dialog opens with its note, on its own line, in both
// the warning and the countdown; a manual one has none.
func TestPrecheckShowsAutoBackupNote(t *testing.T) {
	note := autoBackupNote()
	f := newPrecheckFixtureWithNote(t, note)
	if want := note + "\n\nWarning: "; !strings.HasPrefix(f.text(), want) {
		t.Errorf("warning = %q, want it to start %q", f.text(), want)
	}
	f.connect(t)
	f.p.refreshBtn.OnTapped()
	if want := note + "\n\nRefreshed: "; !strings.HasPrefix(f.text(), want) {
		t.Errorf("countdown = %q, want it to start %q", f.text(), want)
	}

	manual := newPrecheckFixture(t)
	if strings.Contains(manual.text(), "Automatic") {
		t.Errorf("manual backup's warning mentions the automatic backup: %q", manual.text())
	}
}
