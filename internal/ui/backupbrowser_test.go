package ui

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"

	"github.com/pglass/checkin/internal/backup"
	"github.com/pglass/checkin/internal/center"
	"github.com/pglass/checkin/internal/config"
)

// seedArchives writes fake archives with the given timestamp stamps, returning
// the directory holding them.
func seedArchives(t *testing.T, stamps ...string) string {
	t.Helper()
	dir := t.TempDir()
	for _, stamp := range stamps {
		name := backup.ArchivePrefix + stamp + ".zip"
		if err := os.WriteFile(filepath.Join(dir, name), []byte("payload"), 0o644); err != nil {
			t.Fatalf("seed %s: %v", name, err)
		}
	}
	return dir
}

// newTestBrowser builds a browser window over dir without showing it.
func newTestBrowser(t *testing.T, dir string) *backupBrowser {
	t.Helper()
	fa := test.NewApp()
	t.Cleanup(fa.Quit)

	b := &backupBrowser{dir: dir}
	b.win = fa.NewWindow("Backups")
	b.build()
	b.reload()
	return b
}

// newTestBrowserDirs builds a browser over the internal dir with backup dirs
// selectable, as the startup window opens it, without showing it.
func newTestBrowserDirs(t *testing.T, internal string, dirs ...string) *backupBrowser {
	t.Helper()
	fa := test.NewApp()
	t.Cleanup(fa.Quit)

	b := &backupBrowser{dir: internal, internalDir: internal, dirs: browserDirs(internal, dirs)}
	b.win = fa.NewWindow("Backups")
	b.build()
	b.reload()
	return b
}

// The selector lists the internal directory first, marked, then each backup
// directory without repeats, then Browse…; it starts on the internal one.
func TestBrowserSelectorOptions(t *testing.T) {
	internal := seedArchives(t, "2026-01-01-000000")
	ext := t.TempDir()
	b := newTestBrowserDirs(t, internal, ext, "", internal, ext)

	want := []string{internal + internalSuffix, ext, browseOption}
	if got := b.selector.Options; !slices.Equal(got, want) {
		t.Errorf("options = %q, want %q", got, want)
	}
	if got := b.selector.Selected; got != internal+internalSuffix {
		t.Errorf("selected = %q, want the internal dir", got)
	}
	if b.dir != internal {
		t.Errorf("listing %q, want the internal dir", b.dir)
	}
}

// Choosing another directory lists its archives.
func TestBrowserSelectingDirListsIt(t *testing.T) {
	internal := seedArchives(t, "2026-01-01-000000")
	ext := seedArchives(t, "2024-01-01-000000", "2025-01-01-000000")
	b := newTestBrowserDirs(t, internal, ext)

	b.selector.SetSelected(ext)
	if b.dir != ext {
		t.Fatalf("listing %q, want %q", b.dir, ext)
	}
	if len(b.archives) != 2 {
		t.Errorf("archives = %d, want the 2 in %s", len(b.archives), ext)
	}

	b.selector.SetSelected(internal + internalSuffix)
	if b.dir != internal || len(b.archives) != 1 {
		t.Errorf("back on internal: dir %q with %d archives, want %q with 1", b.dir, len(b.archives), internal)
	}
}

// A directory picked with Browse… is added to the selector once and listed.
func TestBrowserAddDir(t *testing.T) {
	internal := seedArchives(t, "2026-01-01-000000")
	picked := seedArchives(t, "2023-01-01-000000", "2023-02-01-000000", "2023-03-01-000000")
	b := newTestBrowserDirs(t, internal)

	// Stands in for the folder picker's callback.
	b.addDir(picked)
	b.addDir(picked + string(filepath.Separator))

	want := []string{internal + internalSuffix, picked, browseOption}
	if got := b.selector.Options; !slices.Equal(got, want) {
		t.Errorf("options = %q, want %q", got, want)
	}
	if b.dir != picked || len(b.archives) != 3 {
		t.Errorf("listing %q with %d archives, want %q with 3", b.dir, len(b.archives), picked)
	}
}

// A backup directory that does not exist, like one on an unplugged drive, says
// so rather than claiming there are no backups yet.
func TestBrowserUnavailableDir(t *testing.T) {
	gone := filepath.Join(t.TempDir(), "NotMounted")
	b := newTestBrowserDirs(t, t.TempDir(), gone)

	b.selector.SetSelected(gone)
	if b.empty.Hidden || b.empty.Text != unavailableDirText(gone) {
		t.Errorf("empty text = %q (hidden %v), want the unavailable message", b.empty.Text, b.empty.Hidden)
	}
}

// Archives are listed newest first, which is the order the rows are rendered in.
func TestBrowserListsArchivesNewestFirst(t *testing.T) {
	dir := seedArchives(t, "2024-01-02-030405", "2026-05-06-070809", "2025-03-04-050607")
	b := newTestBrowser(t, dir)

	if len(b.archives) != 3 {
		t.Fatalf("archives = %d, want 3", len(b.archives))
	}
	wantYears := []int{2026, 2025, 2024}
	for i, want := range wantYears {
		if y := b.archives[i].CreatedAt.Year(); y != want {
			t.Errorf("row %d year = %d, want %d", i, y, want)
		}
	}

	// The rendered row shows that archive's own time, not a recycled one.
	row := renderBrowserRow(t, b, 0)
	label := findLabel(row)
	if label == nil {
		t.Fatal("row has no label")
	}
	if !strings.Contains(label.Text, "2026") {
		t.Errorf("first row text = %q, want the newest archive's date", label.Text)
	}
}

// Every row carries its own Verify and Restore buttons.
func TestBrowserRowHasVerifyAndRestore(t *testing.T) {
	dir := seedArchives(t, "2026-05-06-070809")
	b := newTestBrowser(t, dir)

	row := renderBrowserRow(t, b, 0)

	verify := findButton(row, "Verify")
	if verify == nil {
		t.Fatal("row has no Verify button")
	}
	if verify.Disabled() {
		t.Error("Verify is disabled, but verification is implemented")
	}
	if verify.OnTapped == nil {
		t.Error("Verify has no action bound")
	}

	restore := findButton(row, "Explore…")
	if restore == nil {
		t.Fatal("row has no Explore… button")
	}
	if restore.Disabled() {
		t.Error("Explore… is disabled, but exploring a backup is implemented")
	}
	if restore.OnTapped == nil {
		t.Error("Explore… has no action bound")
	}
}

// Rows are recycled by widget.List, so each row's Verify must act on the
// archive currently rendered in it, not the one it was first built with.
func TestBrowserVerifyButtonFollowsItsRow(t *testing.T) {
	dir := seedArchives(t, "2026-05-06-070809", "2024-01-02-030405")
	b := newTestBrowser(t, dir)

	// One row object, rendered at both indices in turn, as the list does when
	// scrolling.
	obj := b.list.CreateItem()

	b.list.UpdateItem(0, obj)
	first := findLabel(obj).Text

	b.list.UpdateItem(1, obj)
	second := findLabel(obj).Text

	if first == second {
		t.Fatalf("both rows rendered as %q; the row was not rebound", first)
	}
	if !strings.Contains(first, "2026") || !strings.Contains(second, "2024") {
		t.Errorf("rows rendered as %q then %q, want the newest first", first, second)
	}
}

// An empty backup directory explains itself rather than showing a blank panel.
func TestBrowserEmptyDirectoryExplainsItself(t *testing.T) {
	b := newTestBrowser(t, t.TempDir())

	if len(b.archives) != 0 {
		t.Fatalf("archives = %d, want none", len(b.archives))
	}
	if b.empty.Hidden {
		t.Error("empty-state message is hidden with no archives, want it shown")
	}
	if !b.list.Hidden {
		t.Error("list is shown with no archives, want it hidden")
	}
	if !strings.Contains(b.empty.Text, "No backups") {
		t.Errorf("empty text = %q, want it to say there are none", b.empty.Text)
	}
}

// With archives present the list is shown and the empty message is not.
func TestBrowserShowsListWhenArchivesExist(t *testing.T) {
	dir := seedArchives(t, "2026-05-06-070809")
	b := newTestBrowser(t, dir)

	if b.list.Hidden {
		t.Error("list is hidden with archives present, want it shown")
	}
	if !b.empty.Hidden {
		t.Error("empty-state message is shown with archives present, want it hidden")
	}
}

// The startup window's menubar carries a Backups menu with the browser item.
func TestStartupHasBackupsMenu(t *testing.T) {
	fa := test.NewApp()
	t.Cleanup(fa.Quit)

	dir := t.TempDir()
	ShowStartup(fa, dir, config.Default(), filepath.Join(dir, "settings.ini"),
		func(center.Center, fyne.Window) error { return nil })

	var menu *fyne.MainMenu
	for _, w := range fa.Driver().AllWindows() {
		if w.Title() == "Check-In" {
			menu = w.MainMenu()
		}
	}
	if menu == nil {
		t.Fatal("selection window has no main menu")
	}

	var backupsMenu *fyne.Menu
	for _, m := range menu.Items {
		if m.Label == "Backups" {
			backupsMenu = m
		}
	}
	if backupsMenu == nil {
		var labels []string
		for _, m := range menu.Items {
			labels = append(labels, m.Label)
		}
		t.Fatalf("menus = %v, want one called Backups", labels)
	}

	var items []string
	for _, it := range backupsMenu.Items {
		items = append(items, it.Label)
	}
	if len(items) != 1 || !strings.Contains(items[0], "Browse/Restore") {
		t.Errorf("Backups menu = %v, want a Browse/Restore item", items)
	}
}

// Sizes read at a glance without implying byte-level precision.
func TestFormatArchiveSize(t *testing.T) {
	for _, tc := range []struct {
		n    int64
		want string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{1024, "1.0 KB"},
		{1536, "1.5 KB"},
		{1024 * 1024, "1.0 MB"},
		{3 * 1024 * 1024 * 1024, "3.0 GB"},
	} {
		if got := formatArchiveSize(tc.n); got != tc.want {
			t.Errorf("formatArchiveSize(%d) = %q, want %q", tc.n, got, tc.want)
		}
	}
}

// renderBrowserRow renders row i the way widget.List does and returns it.
func renderBrowserRow(t *testing.T, b *backupBrowser, i int) fyne.CanvasObject {
	t.Helper()
	obj := b.list.CreateItem()
	b.list.UpdateItem(i, obj)
	return obj
}

// findLabel returns the first label anywhere under o.
func findLabel(o fyne.CanvasObject) *widget.Label {
	switch v := o.(type) {
	case *widget.Label:
		return v
	case *fyne.Container:
		for _, child := range v.Objects {
			if l := findLabel(child); l != nil {
				return l
			}
		}
	}
	return nil
}
