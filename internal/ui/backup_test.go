package ui

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/pglass/checkin/internal/backup"
	"github.com/pglass/checkin/internal/center"
	"github.com/pglass/checkin/internal/config"
)

// newTestStartupWithConfig builds a selection window over a temp app dir with
// the given settings, returning it plus the app dir.
func newTestStartupWithConfig(t *testing.T, cfg config.Config, names ...string) (*startup, string) {
	t.Helper()
	fa := test.NewApp()
	t.Cleanup(fa.Quit)

	dir := t.TempDir()
	for _, n := range names {
		if _, err := center.Create(dir, n); err != nil {
			t.Fatalf("Create %s: %v", n, err)
		}
	}

	s := &startup{fyneApp: fa, appDir: dir, cfg: cfg,
		onOpen: func(center.Center, fyne.Window) error { return nil }}
	s.win = fa.NewWindow("startup")
	s.build()
	s.reload()
	return s, dir
}

// The three states of the "last backup" line are distinguishable: off, on but
// never run, and run at a known time.
func TestLastBackupText(t *testing.T) {
	at := time.Date(2026, 3, 4, 15, 6, 0, 0, time.Local)

	if got := lastBackupText("", time.Time{}, false); got != lastBackupText("", at, true) {
		t.Error("an unconfigured backup dir should read the same regardless of any timestamp")
	}
	off := lastBackupText("", time.Time{}, false)
	if !strings.Contains(off, "Settings") {
		t.Errorf("off text %q should point at Settings", off)
	}

	never := lastBackupText("/some/dir", time.Time{}, false)
	if !strings.Contains(never, "never") {
		t.Errorf("never-run text = %q, want it to say never", never)
	}
	if never == off {
		t.Error("never-run should not read the same as backups being off")
	}

	ran := lastBackupText("/some/dir", at, true)
	if !strings.Contains(ran, "2026") || !strings.Contains(ran, "Mar") {
		t.Errorf("ran text = %q, want it to show the date", ran)
	}
}

// With no backup directory configured there is nowhere to write, so the button
// is disabled rather than offering a backup that must then fail.
func TestBackupButtonDisabledWithoutBackupDir(t *testing.T) {
	cfg := config.Default() // BackupDir is empty by default
	s, _ := newTestStartupWithConfig(t, cfg, "Alpha")

	if !s.backups.button.Disabled() {
		t.Error("Back Up Now is enabled with no backup_dir set, want disabled")
	}
}

// With a directory configured the button is live.
func TestBackupButtonEnabledWithBackupDir(t *testing.T) {
	cfg := config.Default()
	cfg.BackupDir = t.TempDir()
	s, _ := newTestStartupWithConfig(t, cfg, "Alpha")

	if s.backups.button.Disabled() {
		t.Error("Back Up Now is disabled with backup_dir set, want enabled")
	}
}

// The row reports the newest archive already in the backup directory, so the
// user sees the real state on startup rather than "never" until they run one.
func TestBackupBarShowsExistingBackupTime(t *testing.T) {
	dir := t.TempDir()
	name := backup.ArchivePrefix + "2025-12-25-090000.zip"
	if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	cfg := config.Default()
	cfg.BackupDir = dir
	s, _ := newTestStartupWithConfig(t, cfg, "Alpha")

	got := s.backups.label.Text
	if !strings.Contains(got, "2025") || !strings.Contains(got, "Dec") {
		t.Errorf("label = %q, want the seeded backup's date", got)
	}
}

// backupSummary names both how many Centers were archived and where the file
// went, and gets the singular/plural right.
func TestBackupSummary(t *testing.T) {
	one := backupSummary(backup.Result{
		Path:     "/backups/a.zip",
		Manifest: backup.Manifest{Centers: []backup.CenterInfo{{Name: "Alpha"}}},
	})
	if !strings.Contains(one, "1 Center") || strings.Contains(one, "Centers") {
		t.Errorf("summary = %q, want singular Center", one)
	}
	if !strings.Contains(one, "/backups/a.zip") {
		t.Errorf("summary = %q, want the archive path", one)
	}

	two := backupSummary(backup.Result{
		Path:     "/backups/b.zip",
		Manifest: backup.Manifest{Centers: []backup.CenterInfo{{Name: "Alpha"}, {Name: "Beta"}}},
	})
	if !strings.Contains(two, "2 Centers") {
		t.Errorf("summary = %q, want plural Centers", two)
	}
}

// Settings is reachable from the selection window: backup_dir is set there and
// used here, so a user told to "set backup_dir in Settings" must be able to get
// to it without opening a Center first.
func TestStartupFileMenuHasSettings(t *testing.T) {
	fa := test.NewApp()
	t.Cleanup(fa.Quit)

	dir := t.TempDir()
	if _, err := center.Create(dir, "Alpha"); err != nil {
		t.Fatalf("Create: %v", err)
	}

	ShowStartup(fa, dir, config.Default(), filepath.Join(dir, "settings.ini"),
		func(center.Center, fyne.Window) error { return nil })

	// Found by title: the driver also holds windows created by dialogs, so the
	// selection window is not reliably first.
	var menu *fyne.MainMenu
	for _, w := range fa.Driver().AllWindows() {
		if w.Title() == "Check-In" {
			menu = w.MainMenu()
		}
	}
	if menu == nil || len(menu.Items) == 0 {
		t.Fatal("selection window has no main menu")
	}
	var labels []string
	for _, it := range menu.Items[0].Items {
		labels = append(labels, it.Label)
	}
	if !slices.Contains(labels, "Settings…") {
		t.Errorf("File menu = %v, want it to include Settings…", labels)
	}
}

// Saving from the selection window applies the new backup directory
// immediately: the backup row is refreshed and the button becomes usable,
// rather than waiting for a restart as the main window's settings do.
func TestStartupApplySettingsRefreshesBackupBar(t *testing.T) {
	s, _ := newTestStartupWithConfig(t, config.Default(), "Alpha")
	if !s.backups.button.Disabled() {
		t.Fatal("precondition: button should start disabled with no backup_dir")
	}

	cfg := config.Default()
	cfg.BackupDir = t.TempDir()
	s.applySettings(cfg)

	if s.backups.button.Disabled() {
		t.Error("Back Up Now still disabled after backup_dir was saved, want enabled")
	}
	if s.cfg.BackupDir != cfg.BackupDir {
		t.Errorf("cfg.BackupDir = %q, want %q", s.cfg.BackupDir, cfg.BackupDir)
	}
}

// The Settings window opened from the selection screen writes to the same
// settings.ini and hands values back to that screen, not to a main window.
func TestSettingsHostedByStartup(t *testing.T) {
	s, dir := newTestStartupWithConfig(t, config.Default(), "Alpha")
	s.cfgPath = filepath.Join(dir, "settings.ini")

	// The test app closes its windows on Quit, so this one is not closed here.
	win := showSettingsFor(s, func() { s.settingsWin = nil })

	sw := &settings{host: s}
	sw.win = win
	sw.build()

	backupDir := t.TempDir()
	for _, row := range sw.rows {
		if row.field.Key == "backup_dir" {
			row.entry.SetText(backupDir)
		}
	}
	sw.save()

	if s.cfg.BackupDir != backupDir {
		t.Errorf("startup cfg.BackupDir = %q, want %q", s.cfg.BackupDir, backupDir)
	}
	loaded, err := config.Load(s.cfgPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.BackupDir != backupDir {
		t.Errorf("settings.ini backup_dir = %q, want %q", loaded.BackupDir, backupDir)
	}
}

// The two hosts differ in what a save actually changes. From the selection
// window nothing has read a setting yet, so every saved value is live and there
// is no warning at all; from an open Center the camera and store are already
// running on the old values, so the restart warning stays.
func TestSettingsRestartNoteIsHostSpecific(t *testing.T) {
	s, _ := newTestStartupWithConfig(t, config.Default(), "Alpha")
	if fromStartup := s.settingsRestartNote(); fromStartup != "" {
		t.Errorf("selection window note = %q, want no note: nothing there needs a restart",
			fromStartup)
	}

	a := &App{}
	if fromApp := a.settingsRestartNote(); !strings.Contains(fromApp, "re-open") {
		t.Errorf("main window note = %q, want it to ask for a restart", fromApp)
	}
}

// A host with no warning gets no widget, so editing a value from the selection
// window cannot light up a note that was never built.
func TestSettingsFromStartupHasNoRestartNote(t *testing.T) {
	host, _ := newTestStartupWithConfig(t, config.Default(), "Alpha")

	s := &settings{host: host}
	s.win = host.fyneApp.NewWindow("Settings")
	s.win.SetContent(s.build())

	if s.restartNote != nil {
		t.Fatalf("restart note built for a host with no warning: %q", s.restartNote.Text)
	}

	// Editing must not panic on the absent note.
	s.rows[0].entry.SetText("99")
	if !s.dirty() {
		t.Error("editing a row did not mark the window dirty")
	}
}

// Steps that finish faster than the minimum are held so the message can be
// read; the message itself is delivered at once, only the *next* one waits.
func TestPacedHoldsFastStepsForTheMinimum(t *testing.T) {
	const min = 50 * time.Millisecond

	var at []time.Duration
	start := time.Now()
	wrapped, wait := paced(func(int, int, string) { at = append(at, time.Since(start)) }, min)

	// Three back-to-back steps, as fast as a small Center actually snapshots.
	wrapped(1, 3, "one")
	wrapped(2, 3, "two")
	wrapped(3, 3, "three")
	wait()

	if len(at) != 3 {
		t.Fatalf("got %d messages, want 3", len(at))
	}
	// The first is immediate: the user should see step 1 the moment it starts.
	if at[0] > min {
		t.Errorf("first message delivered after %v, want immediately", at[0])
	}
	// Each later message waits out the previous one's minimum.
	for i := 1; i < len(at); i++ {
		gap := at[i] - at[i-1]
		if gap < min {
			t.Errorf("message %d came %v after the previous, want at least %v", i+1, gap, min)
		}
	}
	// wait() holds the final message for its own minimum before the summary.
	if total := time.Since(start); total < 3*min {
		t.Errorf("run took %v, want at least %v so every message was readable", total, 3*min)
	}
}

// A step slower than the minimum is not padded further: pacing makes fast steps
// readable, it does not make a backup slower than the work it is doing.
func TestPacedDoesNotDelaySlowSteps(t *testing.T) {
	const min = 20 * time.Millisecond

	wrapped, wait := paced(func(int, int, string) {}, min)
	wrapped(1, 2, "one")
	time.Sleep(3 * min) // the step's own work outlasts the minimum

	start := time.Now()
	wrapped(2, 2, "two")
	if waited := time.Since(start); waited > min {
		t.Errorf("second message waited %v, want no delay after a slow step", waited)
	}

	start = time.Now()
	time.Sleep(3 * min)
	wait()
	if waited := time.Since(start); waited > 4*min {
		t.Errorf("wait() blocked for %v after a slow final step, want none", waited)
	}
}

// A nil callback is safe: the pacing still applies, so a caller that only wants
// the delay does not have to supply a no-op.
func TestPacedToleratesNilCallback(t *testing.T) {
	wrapped, wait := paced(nil, time.Millisecond)
	wrapped(1, 1, "only")
	wait()
}

// A backup's verification tail is paced at the shorter verify interval: those
// steps are numerous and quick, and pacing them like snapshot steps would make
// a backup feel far slower than the work it does.
func TestPacedByUsesPerStepMinimums(t *testing.T) {
	const slow, fast = 60 * time.Millisecond, 10 * time.Millisecond

	var at []time.Duration
	start := time.Now()
	wrapped, wait := pacedBy(
		func(int, int, string) { at = append(at, time.Since(start)) },
		func(step int) time.Duration {
			if step >= 3 { // steps 3+ stand in for the verification tail
				return fast
			}
			return slow
		})

	for i := 1; i <= 4; i++ {
		wrapped(i, 4, "step")
	}
	wait()

	if len(at) != 4 {
		t.Fatalf("got %d messages, want 4", len(at))
	}
	// Step 2 waits out step 1's slow minimum.
	if gap := at[1] - at[0]; gap < slow {
		t.Errorf("gap after step 1 = %v, want at least %v", gap, slow)
	}
	// Step 4 waits only step 3's fast minimum, not the slow one.
	gap := at[3] - at[2]
	if gap < fast {
		t.Errorf("gap after step 3 = %v, want at least %v", gap, fast)
	}
	if gap >= slow {
		t.Errorf("gap after step 3 = %v, want the fast minimum (%v), not the slow one (%v)",
			gap, fast, slow)
	}
}

// backupNeeded fires on the two lapses that matter -- data with no backup at
// all, and a backup that has gone stale -- and stays quiet when there is
// nothing to back up or backups are switched off by choice.
func TestBackupNeeded(t *testing.T) {
	now := time.Date(2026, 3, 4, 15, 6, 0, 0, time.Local)
	dir := "/some/dir"

	cases := []struct {
		name       string
		backupDir  string
		hasData    bool
		at         time.Time
		ok         bool
		wantReason string
	}{
		{"data but never backed up", dir, true, time.Time{}, false, "No backup found"},
		{"stale backup", dir, true, now.Add(-8 * 24 * time.Hour), true, "Over 7d since last backup"},
		{"exactly at the interval", dir, true, now.Add(-backupInterval), true, "Over 7d since last backup"},
		{"recent backup", dir, true, now.Add(-24 * time.Hour), true, ""},
		{"just inside the interval", dir, true, now.Add(-backupInterval + time.Minute), true, ""},
		// A fresh install has Centers but no databases: nothing to lose yet.
		{"no data yet", dir, false, time.Time{}, false, ""},
		// Backups off is a setting, not a lapse, however old the data.
		{"backups off", "", true, time.Time{}, false, ""},
		{"backups off with stale backup", "", true, now.Add(-30 * 24 * time.Hour), true, ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reason, need := backupNeeded(tc.backupDir, tc.hasData, tc.at, tc.ok, now)
			if need != (tc.wantReason != "") {
				t.Fatalf("need = %v, want %v (reason %q)", need, tc.wantReason != "", reason)
			}
			if reason != tc.wantReason {
				t.Errorf("reason = %q, want %q", reason, tc.wantReason)
			}
		})
	}
}

// The warning names the rule that fired, so the user knows whether they have
// never backed up or merely let one go stale.
func TestBackupNeededText(t *testing.T) {
	got := backupNeededText("No backup found")
	if got != "Backup is needed (No backup found)" {
		t.Errorf("text = %q", got)
	}
}

// A Center directory on its own is not data: the database file is. Otherwise a
// fresh install would demand a backup of nothing.
func TestHasCenterData(t *testing.T) {
	s, dir := newTestStartupWithConfig(t, config.Default(), "Alpha")
	if s.hasCenterData() {
		t.Error("hasCenterData with no database file = true, want false")
	}

	db := filepath.Join(dir, "Alpha", center.DBFileName)
	if err := os.WriteFile(db, []byte("x"), 0o644); err != nil {
		t.Fatalf("seed db: %v", err)
	}
	s.reload()
	if !s.hasCenterData() {
		t.Error("hasCenterData with a database file = false, want true")
	}
}

// A due backup takes over the row: full-size error-coloured warning in place of
// the caption-sized date, so it is not skimmed past.
func TestBackupBarWarnsWhenBackupNeeded(t *testing.T) {
	backupDir := t.TempDir()
	cfg := config.Default()
	cfg.BackupDir = backupDir

	s, dir := newTestStartupWithConfig(t, cfg, "Alpha")
	if err := os.WriteFile(filepath.Join(dir, "Alpha", center.DBFileName), []byte("x"), 0o644); err != nil {
		t.Fatalf("seed db: %v", err)
	}
	s.reload()

	if got := s.backups.label.Text; got != "Backup is needed (No backup found)" {
		t.Errorf("label = %q, want the no-backup warning", got)
	}
	if s.backups.label.TextSize == theme.CaptionTextSize() {
		t.Error("warning is caption-sized, want normal size")
	}
	if s.backups.label.Color != theme.Color(theme.ColorNameError) {
		t.Error("warning is not the error colour")
	}

	// A fresh archive clears it, and the row goes back to the quiet caption.
	name := backup.ArchivePrefix + time.Now().Format("2006-01-02-150405") + ".zip"
	if err := os.WriteFile(filepath.Join(backupDir, name), []byte("x"), 0o644); err != nil {
		t.Fatalf("seed archive: %v", err)
	}
	s.backups.refresh()

	if got := s.backups.label.Text; !strings.HasPrefix(got, "Last backup: ") {
		t.Errorf("label = %q, want the last-backup line once a backup exists", got)
	}
	if s.backups.label.TextSize != theme.CaptionTextSize() {
		t.Error("cleared row is not caption-sized")
	}
	if s.backups.label.Color != theme.Color(theme.ColorNameForeground) {
		t.Error("cleared row is not the foreground colour")
	}
}

// A second backup on a day that already has one is worth confirming; the first
// of the day, or a run against an empty directory, is not.
func TestBackedUpToday(t *testing.T) {
	now := time.Date(2026, 3, 4, 15, 6, 0, 0, time.Local)

	cases := []struct {
		name string
		at   time.Time
		ok   bool
		want bool
	}{
		{"earlier today", now.Add(-2 * time.Hour), true, true},
		{"first thing this morning", time.Date(2026, 3, 4, 0, 1, 0, 0, time.Local), true, true},
		{"last thing tonight", time.Date(2026, 3, 4, 23, 59, 0, 0, time.Local), true, true},
		// Calendar day, not a rolling 24 hours: late last night is yesterday.
		{"late yesterday", time.Date(2026, 3, 3, 23, 0, 0, 0, time.Local), true, false},
		{"a week ago", now.Add(-7 * 24 * time.Hour), true, false},
		{"no backup at all", time.Time{}, false, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := backedUpToday(tc.at, tc.ok, now); got != tc.want {
				t.Errorf("backedUpToday = %v, want %v", got, tc.want)
			}
		})
	}
}

// newTestBackupBarWithArchive builds a selection window whose backup directory
// holds one archive stamped at `at`, with the confirmation dialog replaced by a
// recorder. The returned pointers report whether the user was asked, and
// whether the backup itself was reached.
//
// The real dialog is stubbed and start() is replaced, so no backup ever runs:
// a real one writes into the temp directory from its own goroutine and races
// t.TempDir()'s cleanup.
func newTestBackupBarWithArchive(t *testing.T, at time.Time, answer bool) (asked, started *bool) {
	t.Helper()

	backupDir := t.TempDir()
	if !at.IsZero() {
		name := backup.ArchivePrefix + at.Format("2006-01-02-150405") + ".zip"
		if err := os.WriteFile(filepath.Join(backupDir, name), []byte("x"), 0o644); err != nil {
			t.Fatalf("seed archive: %v", err)
		}
	}

	cfg := config.Default()
	cfg.BackupDir = backupDir
	s, _ := newTestStartupWithConfig(t, cfg, "Alpha")

	asked, started = new(bool), new(bool)
	s.backups.confirm = func(onConfirm func()) {
		*asked = true
		if answer {
			onConfirm()
		}
	}
	// Stand in for start(), so the decision is observed without a real backup.
	s.backups.start = func() { *started = true }

	s.backups.run()
	return asked, started
}

// Pressing Back Up Now with today's archive already present asks first, and
// goes ahead only once the user confirms.
func TestBackupRunConfirmsWhenAlreadyBackedUpToday(t *testing.T) {
	asked, started := newTestBackupBarWithArchive(t, time.Now(), true)
	if !*asked {
		t.Error("no confirmation shown for a second backup on the same day")
	}
	if !*started {
		t.Error("confirmed backup did not start")
	}
}

// Cancelling the confirmation leaves the backup untaken.
func TestBackupRunCancelledConfirmationDoesNotBackUp(t *testing.T) {
	asked, started := newTestBackupBarWithArchive(t, time.Now(), false)
	if !*asked {
		t.Error("no confirmation shown for a second backup on the same day")
	}
	if *started {
		t.Error("backup ran even though the confirmation was cancelled")
	}
}

// With yesterday's archive the only one there, the backup runs straight away
// rather than asking.
func TestBackupRunDoesNotConfirmWhenLastBackupWasEarlier(t *testing.T) {
	asked, started := newTestBackupBarWithArchive(t, time.Now().Add(-24*time.Hour), false)
	if *asked {
		t.Error("confirmation shown when the last backup was not today")
	}
	if !*started {
		t.Error("backup did not start; want it to run without confirmation")
	}
}

// An empty backup directory has nothing to confirm against.
func TestBackupRunDoesNotConfirmWithNoExistingBackup(t *testing.T) {
	asked, started := newTestBackupBarWithArchive(t, time.Time{}, false)
	if *asked {
		t.Error("confirmation shown with no existing backup")
	}
	if !*started {
		t.Error("backup did not start; want it to run without confirmation")
	}
}

// The real confirmation dialog builds and carries the exact wording, with both
// buttons present. Driven through the real dialog rather than the stub, so a
// change to the message or the buttons is caught here.
func TestConfirmSecondBackupDialog(t *testing.T) {
	cfg := config.Default()
	cfg.BackupDir = t.TempDir()
	s, _ := newTestStartupWithConfig(t, cfg, "Alpha")

	confirmed := false
	s.backups.confirmSecondBackup(func() { confirmed = true })

	if s.win.Canvas().Overlays().Top() == nil {
		t.Fatal("no dialog shown")
	}

	var texts []string
	var buttons []*widget.Button
	for _, top := range s.win.Canvas().Overlays().List() {
		for _, o := range test.LaidOutObjects(top) {
			switch v := o.(type) {
			case *widget.Label:
				texts = append(texts, v.Text)
			case *widget.Button:
				buttons = append(buttons, v)
			}
		}
	}

	const want = "A backup was already created today. Make another backup?"
	if !slices.Contains(texts, want) {
		t.Errorf("dialog text = %q, want it to contain %q", texts, want)
	}

	var backUp *widget.Button
	var names []string
	for _, b := range buttons {
		names = append(names, b.Text)
		if b.Text == "Back Up Now" {
			backUp = b
		}
	}
	if !slices.Contains(names, "Cancel") {
		t.Errorf("buttons = %q, want a Cancel button", names)
	}
	if backUp == nil {
		t.Fatalf("buttons = %q, want a Back Up Now button", names)
	}

	backUp.OnTapped()
	if !confirmed {
		t.Error("tapping Back Up Now did not confirm")
	}
}
