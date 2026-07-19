package ui

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"

	"github.com/pglass/checkin/internal/qr"
)

// showGenerateQRDialog lets the operator pick All students or a subset, then
// writes a printable PDF and opens it in the system viewer.
func (a *App) showGenerateQRDialog() {
	rows, err := loadRows(a.ctx, a.store)
	if err != nil {
		dialog.ShowError(err, a.win)
		return
	}
	if len(rows) == 0 {
		dialog.ShowInformation("Generate QR PDF", "No students to generate.", a.win)
		return
	}

	checks := make([]*widget.Check, len(rows))
	items := make([]*widget.Check, 0, len(rows))
	for i, r := range rows {
		c := widget.NewCheck(r.Name, nil)
		c.SetChecked(true)
		checks[i] = c
		items = append(items, c)
	}

	selectAll := widget.NewCheck("Select all", func(v bool) {
		for _, c := range checks {
			c.SetChecked(v)
		}
	})
	selectAll.SetChecked(true)

	listBox := container.NewVBox()
	for _, c := range items {
		listBox.Add(c)
	}
	scroll := container.NewVScroll(listBox)
	scroll.SetMinSize(scroll.MinSize().AddWidthHeight(0, 240))

	content := container.NewBorder(selectAll, nil, nil, nil, scroll)

	dialog.ShowCustomConfirm("Generate QR PDF", "Generate", "Cancel", content, func(ok bool) {
		if !ok {
			return
		}
		var names []string
		for i, c := range checks {
			if c.Checked {
				names = append(names, rows[i].Name)
			}
		}
		if len(names) == 0 {
			dialog.ShowInformation("Generate QR PDF", "No students selected.", a.win)
			return
		}
		a.generateAndOpenPDF(names)
	}, a.win)
}

func (a *App) generateAndOpenPDF(names []string) {
	path := filepath.Join(os.TempDir(),
		"student-qr-"+time.Now().Format("20060102-150405")+".pdf")
	if err := qr.GeneratePDF(names, path); err != nil {
		dialog.ShowError(err, a.win)
		return
	}
	if err := openWithSystemViewer(path); err != nil {
		dialog.ShowInformation("QR PDF generated",
			"Saved to:\n"+path+"\n(Could not auto-open: "+err.Error()+")", a.win)
	}
}

// openWithSystemViewer opens a file with the OS default application.
func openWithSystemViewer(path string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", path).Start()
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", path).Start()
	default:
		return exec.Command("xdg-open", path).Start()
	}
}
