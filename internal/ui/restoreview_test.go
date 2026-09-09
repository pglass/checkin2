package ui

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"

	"github.com/pglass/checkin/internal/backup"
	"github.com/pglass/checkin/internal/center"
	"github.com/pglass/checkin/internal/store"
)

// newTestRestoreView builds a browser plus its restore view over a real
// archive, returning both and the app dir a restore would write into.
func newTestRestoreView(t *testing.T, centers map[string]int) (*restoreView, *backupBrowser, string) {
	t.Helper()
	fa := test.NewApp()
	t.Cleanup(fa.Quit)

	srcDir, backupDir, appDir := t.TempDir(), t.TempDir(), t.TempDir()
	for name, n := range centers {
		c, err := center.Create(srcDir, name)
		if err != nil {
			t.Fatalf("Create %s: %v", name, err)
		}
		st, err := store.Open(c.DBPath())
		if err != nil {
			t.Fatalf("Open %s: %v", name, err)
		}
		for i := range n {
			if _, err := st.AddStudent(context.Background(),
				store.NewName("First"+string(rune('A'+i)), "Last"+name)); err != nil {
				t.Fatalf("AddStudent: %v", err)
			}
		}
		st.Close()
	}

	list, _ := center.List(srcDir)
	res, err := backup.Run(context.Background(), list, backupDir, "test", 7, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	b := &backupBrowser{dir: backupDir, appDir: appDir}
	b.win = fa.NewWindow("Backups")
	b.build()
	b.reload()

	man, err := backup.ReadManifest(res.Path)
	if err != nil {
		t.Fatalf("ReadManifest: %v", err)
	}
	r := newRestoreView(b, backup.Archive{Path: res.Path, CreatedAt: man.CreatedAt}, man)
	return r, b, appDir
}

// The confirmation names the Center that will be created, in the wording the
// user was promised -- the derived name is the one thing they cannot predict.
func TestRestoreConfirmTextNamesTheNewCenter(t *testing.T) {
	man := backup.Manifest{
		CreatedAt: time.Date(2026, 9, 8, 14, 5, 0, 0, time.Local),
		Centers:   []backup.CenterInfo{{Name: "Maple St"}},
	}
	got := restoreConfirmText(man.Centers[0], man)

	want := backup.RestoreName("Maple St", man.CreatedAt)
	if !strings.Contains(got, want) {
		t.Errorf("confirm text = %q, want it to name %q", got, want)
	}
	if !strings.HasPrefix(got, "This will add a center named ") {
		t.Errorf("confirm text = %q, want the agreed wording", got)
	}
}

// Each row names its Center and what is in it, so two backups of the same
// Center can be told apart.
func TestRestoreRowShowsCenterAndCounts(t *testing.T) {
	ci := backup.CenterInfo{Name: "Maple St", StudentCount: 12, LogCount: 340}

	got := restoreCenterLabel(ci)
	for _, want := range []string{"Maple St", "12 students", "340 log entries"} {
		if !strings.Contains(got, want) {
			t.Errorf("row text = %q, want it to contain %q", got, want)
		}
	}
}

// The backup's timestamp belongs to the header, not to every row: all rows in
// the view come from one archive, so repeating it per row says nothing.
func TestRestoreRowOmitsTimestamp(t *testing.T) {
	at := time.Date(2026, 9, 8, 14, 5, 0, 0, time.Local)
	ci := backup.CenterInfo{Name: "Maple St", StudentCount: 12, LogCount: 340}

	got := restoreCenterLabel(ci)
	for _, unwanted := range []string{at.Format(archiveListTimeFormat), "2026", "14:05", "2:05"} {
		if strings.Contains(got, unwanted) {
			t.Errorf("row text = %q, want no timestamp (%q)", got, unwanted)
		}
	}
}

// The header says which backup is on screen, in the agreed wording.
func TestRestoreViewHeaderText(t *testing.T) {
	r, _, _ := newTestRestoreView(t, map[string]int{"Alpha": 1})

	want := "Displaying centers in backup from " + r.man.CreatedAt.Format(archiveListTimeFormat)
	var texts []string
	for _, o := range test.LaidOutObjects(r.view) {
		if rt, ok := o.(*widget.RichText); ok {
			texts = append(texts, rt.String())
		}
	}
	if !slices.Contains(texts, want) {
		t.Errorf("header texts = %q, want %q", texts, want)
	}
}

// The restore view lists every Center in the backup, each with its own button.
func TestRestoreViewListsEveryCenter(t *testing.T) {
	r, _, _ := newTestRestoreView(t, map[string]int{"Alpha": 2, "Beta": 1})

	if len(r.man.Centers) != 2 {
		t.Fatalf("manifest has %d Centers, want 2", len(r.man.Centers))
	}

	list := findList(r.view)
	if list == nil {
		t.Fatal("restore view has no list")
	}
	if n := list.Length(); n != 2 {
		t.Fatalf("list has %d rows, want 2", n)
	}

	obj := list.CreateItem()
	list.UpdateItem(0, obj)
	if btn := findButton(obj, "Restore Center"); btn == nil {
		t.Error("row has no Restore Center button")
	} else if btn.OnTapped == nil {
		t.Error("Restore Center has no action bound")
	}
}

// The view has a Back button that returns to the archive list.
func TestRestoreViewBackReturnsToTheList(t *testing.T) {
	r, b, _ := newTestRestoreView(t, map[string]int{"Alpha": 1})

	b.win.SetContent(r.view)
	back := findButton(r.view, "Back")
	if back == nil {
		t.Fatal("restore view has no Back button")
	}

	back.OnTapped()
	if b.win.Content() != b.listView {
		t.Error("Back did not return to the archive list view")
	}
}

// Restoring through the view creates the Center and tells the caller, so the
// Center selection list behind the window can refresh.
func TestRestoreViewCreatesTheCenterAndNotifies(t *testing.T) {
	r, b, appDir := newTestRestoreView(t, map[string]int{"Alpha": 3})

	var notified bool
	b.onRestored = func() { notified = true }

	r.doRestore(r.man.Centers[0])

	centers, err := center.List(appDir)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(centers) != 1 {
		t.Fatalf("created %d Centers, want 1", len(centers))
	}
	want := backup.RestoreName("Alpha", r.man.CreatedAt)
	if centers[0].Name != want {
		t.Errorf("created %q, want %q", centers[0].Name, want)
	}
	if !notified {
		t.Error("onRestored was not called, so the Center list stays stale")
	}
}

// Restoring the same backup twice is refused rather than overwriting, and the
// second attempt creates nothing.
func TestRestoreViewSecondAttemptDoesNotOverwrite(t *testing.T) {
	r, _, appDir := newTestRestoreView(t, map[string]int{"Alpha": 2})

	r.doRestore(r.man.Centers[0])
	r.doRestore(r.man.Centers[0])

	centers, err := center.List(appDir)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(centers) != 1 {
		t.Errorf("after restoring twice there are %d Centers, want 1", len(centers))
	}
}

// findList returns the first widget.List anywhere under o.
func findList(o fyne.CanvasObject) *widget.List {
	switch v := o.(type) {
	case *widget.List:
		return v
	case *fyne.Container:
		for _, child := range v.Objects {
			if l := findList(child); l != nil {
				return l
			}
		}
	case *container.Scroll:
		return findList(v.Content)
	}
	return nil
}
