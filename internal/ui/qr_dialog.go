package ui

import (
	"fmt"
	"image"
	"os/exec"
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

// showGenerateQRDialog opens the Generate QR Code picker in its own window, or
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
		dialog.ShowInformation("Generate QR Code", "No students to generate.", a.win)
		return
	}

	sel := newStudentSelect(rows)
	w := a.fyneApp.NewWindow("Generate QR Code")

	generate := func() {
		students := sel.selectedForQR()
		if len(students) == 0 {
			dialog.ShowInformation("Generate QR Code", "No students selected.", w)
			return
		}
		a.runGeneration(w, students)
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

// scanHint sits under a shown QR code, telling the user what to do with it.
const scanHint = "To get this code on your phone, scan the code using your phone camera."

// generateQRForStudent shows the QR code for one student straight from the
// main list's row button, skipping the picker. This is the common case -- one
// student, one code -- and the picker exists for the batch case.
//
// Unlike the picker's batch path (runGeneration), nothing is written to disk
// and no system app is opened: the card is rendered in memory and shown in a
// Fyne window. A single code is something the user wants to look at, usually to
// scan it off the screen there and then, so a file on the Desktop and a
// detoured image viewer are both overhead. The picker still writes files, which
// is the point of generating a batch.
//
// It opens its own window rather than reusing a.qrWin: that window is the
// picker, and borrowing it would wipe a selection the user was part-way
// through making.
func (a *App) generateQRForStudent(row store.StudentRow) {
	st := qr.Student{
		First: row.Name.First,
		Last:  row.Name.Last,
		Label: row.Name.Display(),
	}

	// One code window at a time. The button is on every row, so opening a new
	// window per click would bury the screen; the open one is retitled and
	// refilled with the student just asked for, and raised in case it is behind
	// the main window.
	w := a.codeWin
	if w == nil {
		w = a.fyneApp.NewWindow("")
		w.Resize(fyne.NewSize(420, 520))
		w.SetOnClosed(func() { a.codeWin = nil })
		a.codeWin = w
	}
	w.SetTitle("QR Code — " + row.Name.Display())
	w.Show()
	w.RequestFocus()
	a.runSingleGeneration(w, st, a.cfg.QRBaseURL)
}

// runSingleGeneration renders one card in the background and swaps the window
// to show it. The progress bar is kept (the render is quick, but the window
// should never be blank) with only a token minimum display time, so it does not
// stand between the user and the code they asked for.
func (a *App) runSingleGeneration(w fyne.Window, st qr.Student, baseURL string) {
	msg := widget.NewLabel("Generating QR Code…")
	msg.Alignment = fyne.TextAlignCenter
	bar := widget.NewProgressBarInfinite()
	body := container.NewVBox(layout.NewSpacer(), msg, bar, layout.NewSpacer())
	w.SetContent(withWindowMargin(body))

	// Clicking a second student while the first is still rendering leaves two
	// goroutines racing for one window, and the slower would win whichever was
	// asked for last -- showing one student's code under another's name. Each
	// run claims a number and a stale one discards its result.
	a.codeGen++
	gen := a.codeGen

	start := time.Now()
	go func() {
		card, err := qr.Card(baseURL, st)

		// A token floor so the bar is seen rather than flashing by; short
		// enough that it is not a wait.
		if d := minSingleProgressDisplay - time.Since(start); d > 0 {
			time.Sleep(d)
		}

		fyne.Do(func() {
			if gen != a.codeGen {
				return // superseded by a later click
			}
			if err != nil {
				dialog.ShowError(err, w)
				return
			}
			w.SetContent(withWindowMargin(qrCardView(card)))
		})
	}()
}

// minSingleProgressDisplay is how long the single-code progress bar is shown at
// minimum. Just enough to register as a step rather than a flicker.
const minSingleProgressDisplay = 100 * time.Millisecond

// qrCardView lays out a rendered card above the scan hint.
func qrCardView(card image.Image) fyne.CanvasObject {
	img := canvas.NewImageFromImage(card)
	img.FillMode = canvas.ImageFillContain
	// The card is drawn at a fixed aspect; give it a minimum so a small window
	// still shows a scannable code rather than shrinking it to nothing.
	img.SetMinSize(fyne.NewSize(qrViewMinSide, qrViewMinSide*float32(qr.CardHeight)/float32(qr.CardWidth)))

	hint := widget.NewLabel(scanHint)
	hint.Alignment = fyne.TextAlignCenter
	hint.Wrapping = fyne.TextWrapWord

	return container.NewBorder(nil, hint, nil, nil, img)
}

// qrViewMinSide is the narrowest the shown card may be drawn, in points. Below
// this a phone camera starts to struggle with the code on screen.
const qrViewMinSide = 280

// minProgressDisplay is the shortest time the progress bar takes to fill, so
// even a tiny job shows the bar animating rather than flashing by.
const minProgressDisplay = 2500 * time.Millisecond

// runGeneration swaps the window to a progress view and writes the PNG images
// on a background goroutine, so the UI never freezes. The bar tracks real
// render progress (rate-limited so it takes at least minProgressDisplay); once
// the images are written, a spinner covers opening the result.
//
// What gets opened afterwards depends on the size of the job: one code opens
// the image itself, which is what the user asked for; several open the folder,
// since throwing a dozen image windows at them would not be useful.
func (a *App) runGeneration(w fyne.Window, students []qr.Student) {
	total := len(students)
	// Read on the UI thread; the generator goroutine must not touch a.cfg.
	baseURL := a.cfg.QRBaseURL

	dir, dirErr := qr.CenterDir(a.centerName)
	if dirErr != nil {
		dialog.ShowError(dirErr, w)
		return
	}

	noun := "QR Codes"
	opensFile := total == 1
	if opensFile {
		noun = "QR Code"
	}
	msg := widget.NewLabel(fmt.Sprintf("Generating %d %s…", total, noun))
	msg.Alignment = fyne.TextAlignCenter
	bar := widget.NewProgressBar() // 0..1, shows percentage text by default

	spinner := widget.NewActivity()
	openingText := "Opening folder…"
	if opensFile {
		openingText = "Opening image…"
	}
	opening := widget.NewLabel(openingText)
	openingRow := container.NewHBox(layout.NewSpacer(), spinner, opening, layout.NewSpacer())
	openingRow.Hide() // shown once rendering completes

	body := container.NewVBox(layout.NewSpacer(), msg, bar, openingRow, layout.NewSpacer())
	w.SetContent(withWindowMargin(body))

	var (
		renderFrac atomic.Value // float64 in [0,1], written by the generator
		renderDone atomic.Bool  // all images rendered
		genDone    atomic.Bool  // the whole generation returned
		genErr     atomic.Value // error
		paths      atomic.Value // []string, the files written
	)
	renderFrac.Store(0.0)
	start := time.Now()

	// Background: render and write the PNGs. Pure work, no UI calls here.
	go func() {
		out, err := qr.GenerateImagesProgress(baseURL, students, dir, func(done, tot int) {
			renderFrac.Store(float64(done) / float64(tot))
			if done >= tot {
				renderDone.Store(true)
			}
		})
		if err == nil {
			paths.Store(out)
		} else {
			genErr.Store(err)
		}
		genDone.Store(true)
	}()

	// UI ticker: advance the bar to 100%, rate-limited by minProgressDisplay so a
	// tiny job still animates. Once the bar is full (render finished and the
	// minimum time elapsed), reveal the spinner and wait for the writes to end.
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
			// Done only when the bar is full AND every file is written; the
			// spinner covers any gap between the two.
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
		written, _ := paths.Load().([]string)
		// One code: open the image. Several: open the folder holding them.
		open, target := openDirectory, dir
		if opensFile && len(written) == 1 {
			open, target = openWithSystemViewer, written[0]
		}
		label := "Saved to:\n" + target
		fyne.Do(func() {
			if err := open(target); err != nil {
				dialog.ShowInformation("QR Codes generated",
					label+"\n(Could not auto-open: "+err.Error()+")", w)
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

// sortSelectWidth returns a width for the sort dropdown that fits its widest
// option plus the dropdown arrow and internal padding. widget.Select reserves
// the arrow inside its content box and pads both sides, so its own MinSize
// leaves too little for the text and clips it; we add generous room for the
// arrow icon plus padding on both sides.
func sortSelectWidth() float32 {
	var widest float32
	for _, l := range sortModeLabels {
		w := fyne.MeasureText(l, theme.TextSize(), fyne.TextStyle{}).Width
		if w > widest {
			widest = w
		}
	}
	// widest text + arrow icon + inner padding (both sides) + outer padding.
	return widest + theme.IconInlineSize() + 4*theme.InnerPadding() + 2*theme.Padding()
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

	maxSelected int    // 0 = unlimited; else cap the number selectable
	onChange    func() // fired when the selection changes (may be nil)
	onCapHit    func() // fired when a check is refused due to the cap (may be nil)

	list       *widget.List
	count      *widget.Label  // "N selected"
	selectAll  *widget.Check  // header select-all
	nameHeader *sortHeader    // clickable "Name" column header
	sortSel    *widget.Select // sort-mode dropdown (kept in sync with header)
}

// newStudentSelect builds the QR-style picker: all students selected by default,
// no selection cap. Kept as the simple entry point for the QR window.
func newStudentSelect(rows []store.StudentRow) *studentSelect {
	// Alphabetical by (Last, First): the picker is usually used to find one
	// named student, and that is the order every other list in the app uses.
	return newStudentSelectWithOptions(rows, true, 0, sortNameAsc, nil)
}

// newStudentSelectWithOptions builds a picker with configurable defaults:
//   - defaultAllSelected: start with every student selected (QR) vs none (History).
//   - maxSelected: 0 = unlimited; else refuse to check beyond the cap.
//   - initialSort: the ordering the list opens in; the user can change it.
//   - onChange: called whenever the selection changes (nil = ignore).
func newStudentSelectWithOptions(rows []store.StudentRow, defaultAllSelected bool, maxSelected int, initialSort sortMode, onChange func()) *studentSelect {
	s := &studentSelect{
		rows:        append([]store.StudentRow(nil), rows...),
		selected:    make(map[int64]bool, len(rows)),
		sort:        initialSort,
		maxSelected: maxSelected,
		onChange:    onChange,
	}
	if defaultAllSelected {
		for _, r := range rows {
			s.selected[r.ID] = true
		}
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
			name.SetText(r.Name.Display())
			check.OnChanged = nil // avoid firing while we set state
			check.SetChecked(s.selected[r.ID])
			check.OnChanged = func(v bool) {
				// Enforce the selection cap: refuse to check beyond it.
				if v && s.maxSelected > 0 && s.countSelected() >= s.maxSelected {
					check.SetChecked(false)
					s.notifyCap()
					return
				}
				s.selected[r.ID] = v
				s.updateCount()
				s.refreshSelectAll()
				s.fireChange()
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

// countSelected returns the number of currently selected students.
func (s *studentSelect) countSelected() int {
	n := 0
	for _, v := range s.selected {
		if v {
			n++
		}
	}
	return n
}

func (s *studentSelect) fireChange() {
	if s.onChange != nil {
		s.onChange()
	}
}

// notifyCap fires the cap-hit hook (if set) so the host can show an inline note.
func (s *studentSelect) notifyCap() {
	if s.onCapHit != nil {
		s.onCapHit()
	}
}

// header builds the column header row: a select-all checkbox aligned over the
// row checkboxes, and a clickable "Name" header that toggles name sort.
func (s *studentSelect) header() fyne.CanvasObject {
	s.selectAll = widget.NewCheck("", func(v bool) { s.setAll(v) })
	// Select-all would blow a selection cap, so disable it when one is set.
	if s.maxSelected > 0 {
		s.selectAll.Disable()
	}
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

	// widget.Select under-measures its width, clipping the longest option
	// ("Recently Added"). Pin the width to the widest label plus room for the
	// dropdown arrow and padding so nothing is cut off.
	sortSized := container.NewGridWrap(fyne.NewSize(sortSelectWidth(), s.sortSel.MinSize().Height), s.sortSel)

	// Mirror the row layout (check pinned left, name filling) so the header
	// aligns with the columns below it.
	headerRow := container.NewBorder(nil, nil, s.selectAll, nil, s.nameHeader)
	sortBar := container.NewBorder(nil, nil, nil,
		container.NewHBox(widget.NewLabel("Sort:"), sortSized), s.count)

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
		sort.Slice(s.rows, func(i, j int) bool { return s.rows[i].Name.Less(s.rows[j].Name) })
	case sortNameDesc:
		sort.Slice(s.rows, func(i, j int) bool { return s.rows[j].Name.Less(s.rows[i].Name) })
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
	s.fireChange()
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

// selectedNames returns the display names ("Last, First") of the selected
// students, in current sort order, for summaries shown to the user.
func (s *studentSelect) selectedNames() []string {
	var names []string
	for _, r := range s.rows {
		if s.selected[r.ID] {
			names = append(names, r.Name.Display())
		}
	}
	return names
}

// selectedForQR returns the selected students as QR sheet entries, labelled the
// same way they appear in the picker.
func (s *studentSelect) selectedForQR() []qr.Student {
	var out []qr.Student
	for _, r := range s.rows {
		if s.selected[r.ID] {
			out = append(out, qr.Student{
				First: r.Name.First,
				Last:  r.Name.Last,
				Label: r.Name.Display(),
			})
		}
	}
	return out
}

// selectedIDs returns the IDs of selected students in current sort order.
func (s *studentSelect) selectedIDs() []int64 {
	var ids []int64
	for _, r := range s.rows {
		if s.selected[r.ID] {
			ids = append(ids, r.ID)
		}
	}
	return ids
}

// openDirectory opens a folder in the system file browser (Finder, Explorer,
// or the desktop's default file manager).
func openDirectory(path string) error {
	if runtime.GOOS == "windows" {
		// explorer.exe returns a non-zero exit status even when it succeeds, so
		// the command is started and not waited on -- the same as every other
		// platform here.
		return exec.Command("explorer", path).Start()
	}
	return openWithSystemViewer(path)
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
