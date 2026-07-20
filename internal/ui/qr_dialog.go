package ui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"sync/atomic"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/pglass/checkin/internal/qr"
	"github.com/pglass/checkin/internal/store"
)

// sortMode controls the ordering of the multi-select student list.
type sortMode int

const (
	sortNameAsc  sortMode = iota // A → Z
	sortNameDesc                 // Z → A
	sortRecent                   // most recently added first (ID descending)
)

var sortModeLabels = []string{"Name (A–Z)", "Name (Z–A)", "Recently Added"}

// showGenerateQRDialog opens the Generate QR PDF picker in its own window, or
// raises the existing one if already open (so a forgotten window can't spawn a
// duplicate). The main window stays interactive throughout.
//
// The list uses widget.List (which virtualizes rows) and keeps selection in a
// map keyed by student ID, so it stays responsive with thousands of students —
// building one checkbox per student up front does not scale.
func (a *App) showGenerateQRDialog() {
	if a.qrWin != nil {
		a.qrWin.RequestFocus() // already open: bring it to the front
		return
	}

	rows, err := loadRows(a.ctx, a.store)
	if err != nil {
		dialog.ShowError(err, a.win)
		return
	}
	if len(rows) == 0 {
		dialog.ShowInformation("Generate QR PDF", "No students to generate.", a.win)
		return
	}

	sel := newStudentSelect(rows)
	w := a.fyneApp.NewWindow("Generate QR PDF")

	generate := func() {
		names := sel.selectedNames()
		if len(names) == 0 {
			dialog.ShowInformation("Generate QR PDF", "No students selected.", w)
			return
		}
		a.runGeneration(w, names)
	}

	buttons := container.NewHBox(
		layout.NewSpacer(),
		widget.NewButton("Cancel", func() { w.Close() }),
		newPrimaryButton("Generate", generate),
	)
	// List fills the center of the window natively and tracks resize; a real
	// window needs no fixed size or min-size wrapper.
	content := container.NewBorder(sel.header(), buttons, nil, nil, sel.list)

	w.SetContent(withWindowMargin(content))
	w.Resize(fyne.NewSize(480, 560))
	w.SetOnClosed(func() { a.qrWin = nil })
	a.qrWin = w
	w.Show()
}

// minProgressDisplay is the shortest time the progress bar takes to fill, so
// even a tiny job shows the bar animating rather than flashing by.
const minProgressDisplay = 2500 * time.Millisecond

// runGeneration swaps the window to a progress view and generates the PDF on a
// background goroutine, so the UI never freezes. The bar tracks real render
// progress (rate-limited so it takes at least minProgressDisplay); once the
// images are rendered, a spinner + "Opening PDF…" covers the PDF stitching and
// launching the viewer.
func (a *App) runGeneration(w fyne.Window, names []string) {
	total := len(names)

	msg := widget.NewLabel(fmt.Sprintf("Generating PDF for %d QR Codes…", total))
	msg.Alignment = fyne.TextAlignCenter
	bar := widget.NewProgressBar() // 0..1, shows percentage text by default

	spinner := widget.NewActivity()
	opening := widget.NewLabel("Opening PDF…")
	openingRow := container.NewHBox(layout.NewSpacer(), spinner, opening, layout.NewSpacer())
	openingRow.Hide() // shown once rendering completes

	body := container.NewVBox(layout.NewSpacer(), msg, bar, openingRow, layout.NewSpacer())
	w.SetContent(withWindowMargin(body))

	var (
		renderFrac atomic.Value // float64 in [0,1], written by the generator
		renderDone atomic.Bool  // all images rendered (stitching/opening next)
		genDone    atomic.Bool  // whole GeneratePDFProgress returned
		genErr     atomic.Value // error
		path       atomic.Value // string
	)
	renderFrac.Store(0.0)
	start := time.Now()

	// Background: render + assemble PDF. Pure work, no UI calls here.
	go func() {
		out := filepath.Join(os.TempDir(),
			"student-qr-"+time.Now().Format("20060102-150405")+".pdf")
		err := qr.GeneratePDFProgress(names, out, func(done, tot int) {
			renderFrac.Store(float64(done) / float64(tot))
			if done >= tot {
				renderDone.Store(true)
			}
		})
		if err == nil {
			path.Store(out)
		} else {
			genErr.Store(err)
		}
		genDone.Store(true)
	}()

	// UI ticker: advance the bar to 100%, rate-limited by minProgressDisplay so a
	// tiny job still animates. Once the bar is full (render finished and the
	// minimum time elapsed), reveal the spinner and wait for the PDF to finish.
	go func() {
		tick := time.NewTicker(50 * time.Millisecond)
		defer tick.Stop()
		spinnerShown := false
		for range tick.C {
			elapsed := time.Since(start)
			rf, _ := renderFrac.Load().(float64)
			byTime := float64(elapsed) / float64(minProgressDisplay)
			target := minf64(rf, byTime)

			barFull := renderDone.Load() && elapsed >= minProgressDisplay
			if barFull {
				target = 1.0
			}
			t := target
			fyne.Do(func() { bar.SetValue(t) })

			if barFull && !spinnerShown {
				spinnerShown = true
				fyne.Do(func() {
					spinner.Start()
					openingRow.Show()
				})
			}
			// Done only when the bar is full AND the whole job (stitch+write) is
			// complete; the spinner covers any gap between the two.
			if barFull && genDone.Load() {
				break
			}
		}
		fyne.Do(func() { spinner.Stop() })

		if err, _ := genErr.Load().(error); err != nil {
			fyne.Do(func() {
				dialog.ShowError(err, w) // closing the error dialog leaves the window; reopen from the menu to retry
			})
			return
		}
		p, _ := path.Load().(string)
		fyne.Do(func() {
			if err := openWithSystemViewer(p); err != nil {
				dialog.ShowInformation("QR PDF generated",
					"Saved to:\n"+p+"\n(Could not auto-open: "+err.Error()+")", w)
			}
			w.Close() // one-shot: close after generating
		})
	}()
}

func minf64(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

// newPrimaryButton returns a high-importance button (used for the main action).
func newPrimaryButton(label string, fn func()) *widget.Button {
	b := widget.NewButton(label, fn)
	b.Importance = widget.HighImportance
	return b
}

// sortDir is the sort indicator shown on a column header.
type sortDir int

const (
	sortNone       sortDir = iota // not the active sort column
	sortAscending                 // ▲
	sortDescending                // ▼
)

// sortHeader is a clickable column header. When it is the active sort column its
// label is bold and an up/down arrow shows the direction; clicking fires onTap.
// On hover it underlines the label and shows a pointer cursor, signalling that
// the header is interactive (modern column-sort convention).
type sortHeader struct {
	widget.BaseWidget
	label   *canvas.Text
	arrow   *widget.Icon
	onTap   func()
	dir     sortDir
	hovered bool
}

func newSortHeader(text string, onTap func()) *sortHeader {
	h := &sortHeader{
		label: canvas.NewText(text, theme.Color(theme.ColorNameForeground)),
		arrow: widget.NewIcon(nil),
		onTap: onTap,
	}
	h.label.TextSize = theme.TextSize()
	h.ExtendBaseWidget(h)
	return h
}

// setSort updates the bold state and arrow icon for the given direction.
func (h *sortHeader) setSort(dir sortDir) {
	h.dir = dir
	switch dir {
	case sortAscending:
		h.arrow.SetResource(theme.MenuDropUpIcon())
	case sortDescending:
		h.arrow.SetResource(theme.MenuDropDownIcon())
	default:
		h.arrow.SetResource(nil)
	}
	h.applyStyle()
}

// applyStyle recomputes the label's bold/underline from the active-sort and
// hover state. Bold marks the active sort column; underline marks hover.
func (h *sortHeader) applyStyle() {
	h.label.TextStyle = fyne.TextStyle{
		Bold:      h.dir != sortNone,
		Underline: h.hovered,
	}
	h.label.Refresh()
}

func (h *sortHeader) Tapped(_ *fyne.PointEvent) {
	if h.onTap != nil {
		h.onTap()
	}
}

// MouseIn / MouseMoved / MouseOut implement desktop.Hoverable for the underline.
func (h *sortHeader) MouseIn(_ *desktop.MouseEvent) {
	h.hovered = true
	h.applyStyle()
}
func (h *sortHeader) MouseMoved(_ *desktop.MouseEvent) {}
func (h *sortHeader) MouseOut() {
	h.hovered = false
	h.applyStyle()
}

// Cursor implements desktop.Cursorable: a pointer cursor over the header.
func (h *sortHeader) Cursor() desktop.Cursor { return desktop.PointerCursor }

func (h *sortHeader) CreateRenderer() fyne.WidgetRenderer {
	c := container.NewHBox(h.label, h.arrow)
	return widget.NewSimpleRenderer(c)
}

var (
	_ fyne.Tappable      = (*sortHeader)(nil)
	_ desktop.Hoverable  = (*sortHeader)(nil)
	_ desktop.Cursorable = (*sortHeader)(nil)
)

// studentSelect is a virtualized, sortable, multi-select student list. Selection
// is stored by student ID (not per-widget) so it survives sorting and scales.
//
// It uses widget.List (not widget.Table): the list virtualizes rows for 10k+
// students, and a custom header row above it gives full control over the
// clickable/sortable "Name" column and the select-all checkbox — neither of
// which widget.Table's header cells support cleanly.
type studentSelect struct {
	rows     []store.StudentRow // current display order
	selected map[int64]bool     // student ID -> selected
	sort     sortMode

	list       *widget.List
	count      *widget.Label  // "N selected"
	selectAll  *widget.Check  // header select-all
	nameHeader *sortHeader    // clickable "Name" column header
	sortSel    *widget.Select // sort-mode dropdown (kept in sync with header)
}

func newStudentSelect(rows []store.StudentRow) *studentSelect {
	s := &studentSelect{
		rows:     append([]store.StudentRow(nil), rows...),
		selected: make(map[int64]bool, len(rows)),
		sort:     sortRecent,
	}
	// Default to all selected (matches the previous behavior).
	for _, r := range rows {
		s.selected[r.ID] = true
	}
	s.applySort()

	// Each row: a checkbox pinned left, the name filling the rest. Splitting the
	// checkbox from the name (rather than using the check's own label) lets the
	// header's select-all checkbox align directly above this column.
	s.list = widget.NewList(
		func() int { return len(s.rows) },
		func() fyne.CanvasObject {
			check := widget.NewCheck("", nil)
			name := widget.NewLabel("")
			return container.NewBorder(nil, nil, check, nil, name)
		},
		func(i widget.ListItemID, o fyne.CanvasObject) {
			border := o.(*fyne.Container)
			check := border.Objects[1].(*widget.Check)
			name := border.Objects[0].(*widget.Label)
			r := s.rows[i]
			name.SetText(r.Name)
			check.OnChanged = nil // avoid firing while we set state
			check.SetChecked(s.selected[r.ID])
			check.OnChanged = func(v bool) {
				s.selected[r.ID] = v
				s.updateCount()
				s.refreshSelectAll()
			}
		},
	)
	// Rows are not meant to be selectable (only their checkbox matters); undo any
	// row selection immediately so no blue highlight lingers.
	s.list.OnSelected = func(id widget.ListItemID) { s.list.Unselect(id) }
	// No per-row dividers in this picker (the main view keeps its own).
	s.list.HideSeparators = true

	s.count = widget.NewLabel("")
	s.updateCount()
	return s
}

// header builds the column header row: a select-all checkbox aligned over the
// row checkboxes, and a clickable "Name" header that toggles name sort.
func (s *studentSelect) header() fyne.CanvasObject {
	s.selectAll = widget.NewCheck("", func(v bool) { s.setAll(v) })
	s.refreshSelectAll()

	s.nameHeader = newSortHeader("Name", func() {
		// Clicking Name toggles ascending/descending; from any other sort it
		// starts ascending.
		if s.sort == sortNameAsc {
			s.setSort(sortNameDesc)
		} else {
			s.setSort(sortNameAsc)
		}
	})

	s.sortSel = widget.NewSelect(sortModeLabels, func(label string) {
		for m, l := range sortModeLabels {
			if l == label {
				s.setSort(sortMode(m))
				break
			}
		}
	})
	s.updateHeader() // set initial arrow + dropdown selection

	// Mirror the row layout (check pinned left, name filling) so the header
	// aligns with the columns below it.
	headerRow := container.NewBorder(nil, nil, s.selectAll, nil, s.nameHeader)
	sortBar := container.NewBorder(nil, nil, nil,
		container.NewHBox(widget.NewLabel("Sort:"), s.sortSel), s.count)

	// A single separator under the header row gives it presence and divides it
	// from the scrolling list. The list's own per-row dividers are turned off via
	// HideSeparators, so this is the only line in the picker.
	return container.NewVBox(sortBar, headerRow, widget.NewSeparator())
}

// setSort switches the active sort, re-sorts, and syncs both the header arrow
// and the dropdown so the two controls never disagree.
func (s *studentSelect) setSort(m sortMode) {
	s.sort = m
	s.applySort()
	s.updateHeader()
	s.list.Refresh()
	s.list.ScrollToTop()
}

// updateHeader syncs the Name header's bold/arrow state and the sort dropdown to
// the current sort mode.
func (s *studentSelect) updateHeader() {
	switch s.sort {
	case sortNameAsc:
		s.nameHeader.setSort(sortAscending)
	case sortNameDesc:
		s.nameHeader.setSort(sortDescending)
	default:
		s.nameHeader.setSort(sortNone)
	}
	if s.sortSel != nil {
		s.sortSel.OnChanged = nil
		s.sortSel.SetSelectedIndex(int(s.sort))
		s.sortSel.OnChanged = func(label string) {
			for m, l := range sortModeLabels {
				if l == label {
					s.setSort(sortMode(m))
					break
				}
			}
		}
	}
}

// refreshSelectAll sets the header checkbox to reflect the current selection:
// checked when all selected, unchecked otherwise.
func (s *studentSelect) refreshSelectAll() {
	if s.selectAll == nil {
		return
	}
	all := true
	for _, r := range s.rows {
		if !s.selected[r.ID] {
			all = false
			break
		}
	}
	s.selectAll.OnChanged = nil
	s.selectAll.SetChecked(all)
	s.selectAll.OnChanged = func(v bool) { s.setAll(v) }
}

// applySort reorders s.rows according to the current sort mode.
func (s *studentSelect) applySort() {
	switch s.sort {
	case sortNameAsc:
		sort.Slice(s.rows, func(i, j int) bool { return s.rows[i].Name < s.rows[j].Name })
	case sortNameDesc:
		sort.Slice(s.rows, func(i, j int) bool { return s.rows[i].Name > s.rows[j].Name })
	case sortRecent:
		sort.Slice(s.rows, func(i, j int) bool { return s.rows[i].ID > s.rows[j].ID })
	}
}

// setAll selects or deselects every student and refreshes the visible rows.
func (s *studentSelect) setAll(v bool) {
	for _, r := range s.rows {
		s.selected[r.ID] = v
	}
	s.list.Refresh()
	s.updateCount()
	s.refreshSelectAll()
}

func (s *studentSelect) updateCount() {
	n := 0
	for _, v := range s.selected {
		if v {
			n++
		}
	}
	s.count.SetText(fmt.Sprintf("%d of %d selected", n, len(s.rows)))
}

// selectedNames returns the names of selected students in current sort order.
func (s *studentSelect) selectedNames() []string {
	var names []string
	for _, r := range s.rows {
		if s.selected[r.ID] {
			names = append(names, r.Name)
		}
	}
	return names
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
