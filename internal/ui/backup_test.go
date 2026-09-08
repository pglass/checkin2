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

// The two hosts warn differently, because they differ in what a save actually
// changes: from the selection window the backup settings are live immediately,
// so telling the user to restart would be wrong.
func TestSettingsRestartNoteIsHostSpecific(t *testing.T) {
	s, _ := newTestStartupWithConfig(t, config.Default(), "Alpha")
	fromStartup := s.settingsRestartNote()
	if !strings.Contains(fromStartup, "immediately") {
		t.Errorf("selection window note = %q, want it to say backup settings apply immediately",
			fromStartup)
	}

	a := &App{}
	if fromApp := a.settingsRestartNote(); fromApp == fromStartup {
		t.Error("the main window and the selection window should not share one note")
	} else if !strings.Contains(fromApp, "re-open") {
		t.Errorf("main window note = %q, want it to ask for a restart", fromApp)
	}
}
