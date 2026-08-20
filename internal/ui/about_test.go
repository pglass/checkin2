package ui

import (
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
)

// About is the last item of the File menu, after a separator, and there is no
// separate About menu.
func TestMainMenuHasAboutAtBottomOfFile(t *testing.T) {
	fa := test.NewApp()
	defer fa.Quit()

	a := &App{fyneApp: fa, camDevice: deviceNone}
	a.win = fa.NewWindow("main")
	a.about = about{fyneApp: fa, parent: a.win}

	m := a.buildMenu()
	if len(m.Items) != 1 {
		var names []string
		for _, menu := range m.Items {
			names = append(names, menu.Label)
		}
		t.Fatalf("menus = %v, want only [File]", names)
	}
	file := m.Items[0]
	if file.Label != "File" {
		t.Fatalf("menu label = %q, want File", file.Label)
	}

	last := file.Items[len(file.Items)-1]
	if last.Label != "About" {
		t.Fatalf("last File item = %q, want About", last.Label)
	}
	if !file.Items[len(file.Items)-2].IsSeparator {
		t.Error("About should be preceded by a separator")
	}
}

// The About window shows the version and the license text in one view, and
// reopening raises the existing window rather than making a second one.
func TestAboutWindowCombinesVersionAndLicenses(t *testing.T) {
	fa := test.NewApp()
	defer fa.Quit()

	parent := fa.NewWindow("main")
	ab := &about{fyneApp: fa, parent: parent}

	ab.showWindow()
	if ab.win == nil {
		t.Fatal("About window not opened")
	}
	first := ab.win

	joined := allText(ab.win.Content())
	if !strings.Contains(joined, "Version") {
		t.Error("About window does not show the version")
	}
	// A distinctive line from the embedded notices.
	if !strings.Contains(joined, strings.Fields(licenseText)[0]) {
		t.Error("About window does not show the license text")
	}

	ab.showWindow()
	if ab.win != first {
		t.Error("reopening About created a second window")
	}

	ab.win.Close()
	if ab.win != nil {
		t.Error("About window reference not cleared on close")
	}
}

// allText concatenates the text of every label and scroll content in a tree,
// so a test can assert on what the window actually displays.
func allText(o fyne.CanvasObject) string {
	var b strings.Builder
	var walk func(fyne.CanvasObject)
	walk = func(o fyne.CanvasObject) {
		switch v := o.(type) {
		case *fyne.Container:
			for _, child := range v.Objects {
				walk(child)
			}
		case *container.Scroll:
			walk(v.Content)
		case *widget.Label:
			b.WriteString(v.Text)
			b.WriteString("\n")
		}
	}
	walk(o)
	return b.String()
}
