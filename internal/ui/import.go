package ui

import (
	"fmt"
	"image/color"
	"sort"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/storage"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/pglass/checkin/internal/spreadsheet"
)

// noColumn is the placeholder option for the optional Last Name selector. A
// widget.Select cannot hold a truly empty option (an empty string renders as an
// unselected control), so the "no column" choice is a named entry instead.
const noColumn = "(none)"

// importNewColor tints preview rows for students that will be added.
var importNewColor = color.NRGBA{R: 0x2E, G: 0x7D, B: 0x32, A: 0xFF} // green

// previewEntry is one parsed name plus whether importing would create it.
type previewEntry struct {
	name  string
	isNew bool
}

// importer holds the widgets and state for one Import window.
type importer struct {
	app *App
	win fyne.Window

	pathEntry   *widget.Entry
	hasHeader   *widget.Check
	headerSel   *widget.Select
	firstSel    *widget.Select
	lastSel     *widget.Select
	newLbl      *canvas.Text
	existingLbl *canvas.Text
	importBtn   *widget.Button

	// browser is the file picker while it is open, nil otherwise. Tracked so
	// window resizes can be forwarded to it.
	browser *dialog.FileDialog

	sheet   *spreadsheet.Sheet
	entries []previewEntry
	list    *widget.List

	// existing holds the lowercased names already in the database, so the
	// preview can mark rows without a query per row.
	existing map[string]bool
}

// showImportWindow opens the Import window, or raises the existing one.
func (a *App) showImportWindow() {
	if a.importWin != nil {
		a.importWin.RequestFocus()
		return
	}

	rows, err := loadRows(a.ctx, a.store)
	if err != nil {
		dialog.ShowError(err, a.win)
		return
	}

	im := &importer{app: a, existing: make(map[string]bool, len(rows))}
	for _, r := range rows {
		im.existing[normalizeName(r.Name)] = true
	}
	im.win = a.fyneApp.NewWindow("Import Students")
	im.build()

	split := container.NewHSplit(im.configPane(), im.previewPane())
	split.Offset = 0.45

	// The file picker is a modal popup in the canvas overlay, so it takes no
	// part in the window's layout and would keep its original size as the window
	// grows. An invisible notifier stacked behind the content reports every
	// window resize so the open picker can be resized to match.
	notifier := newResizeNotifier(im.onWindowResize)
	im.win.SetContent(container.NewStack(notifier, withWindowMargin(split)))
	im.win.Resize(fyne.NewSize(860, 560))
	im.win.SetOnClosed(func() { a.importWin = nil })
	a.importWin = im.win
	im.win.Show()
}

// build creates every widget before wiring callbacks, so a callback firing
// during setup can't dereference a half-built struct (same ordering rule as the
// History window).
func (im *importer) build() {
	im.pathEntry = widget.NewEntry()
	im.pathEntry.SetPlaceHolder("No file selected")
	// Read-only: the path is set by the file picker. Disable() would grey the
	// text out, so instead the entry just ignores typing.
	im.pathEntry.Disable()

	// Checked by default: nearly every roster has titles in its first row, and
	// the header row is detected on load.
	im.hasHeader = widget.NewCheck("Spreadsheet contains header row", nil)
	im.hasHeader.SetChecked(true)

	im.headerSel = widget.NewSelect(nil, nil)
	im.headerSel.PlaceHolder = "—"

	im.firstSel = widget.NewSelect(nil, nil)
	im.firstSel.PlaceHolder = "Select a column"
	im.lastSel = widget.NewSelect(nil, nil)
	im.lastSel.PlaceHolder = noColumn

	im.newLbl = canvas.NewText("", importNewColor)
	im.newLbl.TextSize = theme.TextSize()
	im.existingLbl = canvas.NewText("", theme.Color(theme.ColorNameDisabled))
	im.existingLbl.TextSize = theme.TextSize()

	im.importBtn = newPrimaryButton("Import", im.onImport)
	im.importBtn.Disable() // nothing to import until a column is chosen

	im.list = widget.NewList(
		func() int { return len(im.entries) },
		func() fyne.CanvasObject { return newPreviewRow() },
		func(i widget.ListItemID, o fyne.CanvasObject) {
			o.(*previewRow).update(im.entries[i])
		},
	)

	im.hasHeader.OnChanged = func(bool) { im.onHasHeaderChanged() }
	im.headerSel.OnChanged = func(string) { im.onHeaderRowChanged() }
	im.firstSel.OnChanged = func(string) { im.refreshPreview() }
	im.lastSel.OnChanged = func(string) { im.refreshPreview() }

	im.updateHeaderSelState()
}

// updateHeaderSelState greys out the header-row selector when the spreadsheet
// has no header row, since there is then no row to choose.
func (im *importer) updateHeaderSelState() {
	if im.hasHeader.Checked {
		im.headerSel.Enable()
	} else {
		im.headerSel.Disable()
	}
}

// onHasHeaderChanged re-reads the sheet with or without a header row.
//
// Unchecking promotes the former header row to data and names the columns by
// spreadsheet letter; checking restores the detected header. Either way the
// column selections are rebuilt, since the column titles have changed.
func (im *importer) onHasHeaderChanged() {
	im.updateHeaderSelState()
	if im.sheet == nil {
		return
	}

	if !im.hasHeader.Checked {
		im.sheet = im.sheet.WithoutHeaderRow()
		im.resetColumnSelectors()
		return
	}

	// Re-checking returns to the row the selector still shows, so the user's
	// earlier choice is not silently replaced by the detected default.
	row := im.selectedHeaderRow()
	if row < 0 {
		im.refreshPreview()
		return
	}
	sheet, err := im.sheet.WithHeaderRow(row)
	if err != nil {
		dialog.ShowError(err, im.win)
		return
	}
	im.sheet = sheet
	im.resetColumnSelectors()
}

// selectedHeaderRow is the worksheet row number the header selector is showing,
// or -1 if nothing is selected.
func (im *importer) selectedHeaderRow() int {
	idx := im.headerSel.SelectedIndex()
	nums := im.sheet.HeaderRowOptions()
	if idx < 0 || idx >= len(nums) {
		return -1
	}
	return nums[idx]
}

// configPane is the left pane: instructions, file picker, column selectors.
func (im *importer) configPane() fyne.CanvasObject {
	intro := widget.NewLabel(
		"Import students from a spreadsheet. Choose a file, then pick the " +
			"column holding each student's name. Use both First and Last Name " +
			"columns if the spreadsheet splits them.")
	intro.Wrapping = fyne.TextWrapWord

	browse := widget.NewButton("Choose File…", im.onBrowse)
	file := container.NewVBox(
		boldText("Select Spreadsheet File"),
		container.NewBorder(nil, nil, nil, browse, im.pathEntry),
	)

	// The header row is guessed on load (Numbers writes a table-caption row
	// above the real header), but no guess is right for every file, so both
	// whether there is a header and which row it is stay editable.
	columns := container.NewVBox(
		boldText("Select Columns"),
		im.hasHeader,
		widget.NewLabel("Header row:"),
		im.headerSel,
		widget.NewLabel("First Name (required):"),
		im.firstSel,
		widget.NewLabel("Last Name (optional):"),
		im.lastSel,
	)

	return container.NewVBox(
		intro,
		widget.NewSeparator(),
		file,
		widget.NewSeparator(),
		columns,
	)
}

// previewPane is the right pane: the counts, the parsed names, and Import.
func (im *importer) previewPane() fyne.CanvasObject {
	header := container.NewVBox(
		boldText("Preview"),
		im.newLbl,
		im.existingLbl,
	)
	buttons := container.NewHBox(
		layout.NewSpacer(),
		widget.NewButton("Cancel", func() { im.win.Close() }),
		im.importBtn,
	)
	return container.NewBorder(header, buttons, nil, nil, im.list)
}

// onBrowse opens the file picker over the Import window, sized to fill it.
//
// The picker is a modal popup in the canvas overlay: it does not participate in
// layout, so it neither fills the window on open nor follows it on resize
// unless told to. onWindowResize keeps it in step while it is open.
func (im *importer) onBrowse() {
	d := dialog.NewFileOpen(func(rc fyne.URIReadCloser, err error) {
		if err != nil {
			dialog.ShowError(err, im.win)
			return
		}
		if rc == nil {
			return // cancelled
		}
		// excelize opens the file by path itself, so this handle is not needed.
		path := rc.URI().Path()
		rc.Close()
		im.loadFile(path)
	}, im.win)
	d.SetFilter(storage.NewExtensionFileFilter(spreadsheet.Extensions))

	// Forget the picker once it closes, by either button, so a stale reference
	// is never resized. FileDialog.Resize dereferences the dialog's internal
	// window, which only exists between Show and close.
	d.SetOnClosed(func() { im.browser = nil })

	// Show before Resize: Resize reaches through the internal window that Show
	// creates, so resizing first panics.
	d.Show()
	im.browser = d
	d.Resize(im.win.Canvas().Size())
}

// onWindowResize keeps an open file picker sized to the window.
func (im *importer) onWindowResize(size fyne.Size) {
	if im.browser != nil {
		im.browser.Resize(size)
	}
}

// resizeNotifier is an invisible widget that reports its own resizes. Fyne's
// public API has no window-resize callback, but a window's content is resized
// to match the window, so a content widget can observe it.
type resizeNotifier struct {
	widget.BaseWidget
	onResize func(fyne.Size)
}

func newResizeNotifier(onResize func(fyne.Size)) *resizeNotifier {
	r := &resizeNotifier{onResize: onResize}
	r.ExtendBaseWidget(r)
	return r
}

// Resize reports every resize, including the pre-layout one that SetContent
// triggers synchronously. Callers decide which are meaningful.
func (r *resizeNotifier) Resize(size fyne.Size) {
	r.BaseWidget.Resize(size)
	if r.onResize != nil {
		r.onResize(size)
	}
}

func (r *resizeNotifier) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(canvas.NewRectangle(color.Transparent))
}

// loadFile reads the workbook and repopulates the column selectors. Any prior
// selection is dropped: column indexes from the previous file are meaningless
// against a new one.
func (im *importer) loadFile(path string) {
	sheet, err := spreadsheet.Open(path)
	if err != nil {
		dialog.ShowError(err, im.win)
		return
	}

	im.sheet = sheet
	im.pathEntry.SetText(path)

	// Header-row options are worksheet row numbers (blank rows excluded), each
	// labeled with its contents so the right one is recognizable without
	// opening the file.
	im.headerSel.OnChanged = nil
	im.headerSel.Options = im.headerRowOptions()
	im.headerSel.SetSelected(headerRowLabel(sheet.HeaderRow, sheet.PreviewRow(im.headerRowIndex(sheet.HeaderRow))))
	im.headerSel.Refresh()
	im.headerSel.OnChanged = func(string) { im.onHeaderRowChanged() }

	// The detected header applies only if the user says the file has one; with
	// the box unchecked, every row is data.
	if !im.hasHeader.Checked {
		im.sheet = sheet.WithoutHeaderRow()
	}

	im.resetColumnSelectors()
}

// resetColumnSelectors repopulates the column dropdowns for the current sheet
// and preselects the name columns when the headers identify them, so a
// recognizable spreadsheet needs no column picking at all. Columns that cannot
// be identified are left unset for the user to choose.
//
// Setting options fires OnChanged, which would run refreshPreview against
// half-updated state, so the callbacks are detached across the swap and the
// preview is refreshed once at the end.
func (im *importer) resetColumnSelectors() {
	im.firstSel.OnChanged, im.lastSel.OnChanged = nil, nil

	im.firstSel.Options = im.sheet.Headers
	im.lastSel.Options = append([]string{noColumn}, im.sheet.Headers...)

	first, last, ok := im.sheet.MatchNameColumns()
	switch {
	case ok && last >= 0:
		im.firstSel.SetSelected(im.sheet.Headers[first])
		im.lastSel.SetSelected(im.sheet.Headers[last])
	case ok:
		im.firstSel.SetSelected(im.sheet.Headers[first])
		im.lastSel.SetSelected(noColumn)
	default:
		im.firstSel.ClearSelected()
		im.lastSel.SetSelected(noColumn)
	}

	im.firstSel.Refresh()
	im.lastSel.Refresh()
	im.firstSel.OnChanged = func(string) { im.refreshPreview() }
	im.lastSel.OnChanged = func(string) { im.refreshPreview() }

	im.refreshPreview()
}

// headerRowOptions labels every candidate header row with its row number and a
// short preview of its contents.
func (im *importer) headerRowOptions() []string {
	nums := im.sheet.HeaderRowOptions()
	opts := make([]string, 0, len(nums))
	for i, n := range nums {
		opts = append(opts, headerRowLabel(n, im.sheet.PreviewRow(i)))
	}
	return opts
}

// headerRowIndex maps a 1-based worksheet row number back to its index among
// the non-blank rows.
func (im *importer) headerRowIndex(row int) int {
	for i, n := range im.sheet.HeaderRowOptions() {
		if n == row {
			return i
		}
	}
	return -1
}

// headerRowLabel renders one header-row choice, e.g. `Row 2: First Name, Last Name`.
func headerRowLabel(row int, preview string) string {
	if preview == "" {
		return fmt.Sprintf("Row %d", row)
	}
	const maxPreview = 60
	if len(preview) > maxPreview {
		preview = preview[:maxPreview] + "…"
	}
	return fmt.Sprintf("Row %d: %s", row, preview)
}

// onHeaderRowChanged re-reads the sheet against the newly chosen header row.
// The column selections are dropped: indexes from the old header do not carry
// over to a different set of columns.
func (im *importer) onHeaderRowChanged() {
	if im.sheet == nil {
		return
	}
	row := im.selectedHeaderRow()
	if row < 0 {
		return
	}

	sheet, err := im.sheet.WithHeaderRow(row)
	if err != nil {
		dialog.ShowError(err, im.win)
		return
	}
	im.sheet = sheet

	im.resetColumnSelectors()
}

// refreshPreview reparses names for the current column selections and repaints
// the preview list and counts.
func (im *importer) refreshPreview() {
	im.entries = nil

	if im.sheet == nil {
		im.setCounts(-1, -1)
		im.importBtn.Disable()
		im.list.Refresh()
		return
	}

	first := indexOf(im.sheet.Headers, im.firstSel.Selected)
	if first < 0 {
		// No First Name column yet: nothing can be parsed.
		im.setCounts(-1, -1)
		im.importBtn.Disable()
		im.list.Refresh()
		return
	}
	last := -1
	if im.lastSel.Selected != noColumn {
		last = indexOf(im.sheet.Headers, im.lastSel.Selected)
	}

	// seen dedupes within the spreadsheet itself: a name repeated in the file
	// is new only the first time, otherwise the import would attempt the same
	// insert twice and the second would fail the unique-name constraint.
	seen := make(map[string]bool)
	newCount := 0
	names := im.sheet.Names(first, last)
	im.entries = make([]previewEntry, 0, len(names))
	for _, n := range names {
		key := normalizeName(n)
		isNew := !im.existing[key] && !seen[key]
		seen[key] = true
		if isNew {
			newCount++
		}
		im.entries = append(im.entries, previewEntry{name: n, isNew: isNew})
	}

	// New students first so the rows that will actually change something are
	// visible without scrolling. Stable so each group keeps spreadsheet order.
	sort.SliceStable(im.entries, func(i, j int) bool {
		return im.entries[i].isNew && !im.entries[j].isNew
	})

	im.setCounts(newCount, len(im.entries)-newCount)
	if newCount > 0 {
		im.importBtn.Enable()
	} else {
		im.importBtn.Disable()
	}
	im.list.Refresh()
}

// setCounts writes the preview totals. A negative count means "not known yet"
// (no file, or no column chosen), which blanks the labels rather than showing
// a misleading zero.
func (im *importer) setCounts(newCount, existingCount int) {
	if newCount < 0 || existingCount < 0 {
		im.newLbl.Text = ""
		im.existingLbl.Text = ""
	} else {
		im.newLbl.Text = fmt.Sprintf("Found %d new students", newCount)
		im.existingLbl.Text = fmt.Sprintf("Found %d existing students", existingCount)
	}
	im.newLbl.Refresh()
	im.existingLbl.Refresh()
}

// onImport writes the new students to the database and closes the window.
func (im *importer) onImport() {
	var added int
	for _, e := range im.entries {
		if !e.isNew {
			continue
		}
		if _, err := im.app.store.AddStudent(im.app.ctx, e.name); err != nil {
			// Partial imports are kept: the students added so far are real, and
			// rolling them back would be more surprising than reporting where
			// it stopped.
			dialog.ShowError(fmt.Errorf("added %d students, then failed on %q: %w", added, e.name, err), im.win)
			im.app.refresh()
			return
		}
		added++
	}
	im.app.refresh()
	im.win.Close()
}

// normalizeName is the key used to compare a parsed name against the database.
// Case-insensitive so "john smith" does not import alongside an existing
// "John Smith"; the store's unique constraint would not catch that on its own.
func normalizeName(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

func indexOf(list []string, want string) int {
	for i, s := range list {
		if s == want {
			return i
		}
	}
	return -1
}

// previewRow is one row of the preview list, colored by whether the student is
// new. It mirrors studentRow's unpadded canvas.Text so the two lists share the
// same compact row height.
type previewRow struct {
	widget.BaseWidget
	name *canvas.Text
}

func newPreviewRow() *previewRow {
	t := canvas.NewText("", theme.Color(theme.ColorNameForeground))
	t.TextSize = theme.TextSize()
	r := &previewRow{name: t}
	r.ExtendBaseWidget(r)
	return r
}

func (r *previewRow) update(e previewEntry) {
	r.name.Text = e.name
	if e.isNew {
		r.name.Color = importNewColor
	} else {
		r.name.Color = theme.Color(theme.ColorNameDisabled) // grey: already present
	}
	r.name.Refresh()
}

func (r *previewRow) CreateRenderer() fyne.WidgetRenderer {
	return &rowRenderer{obj: container.NewStack(r.name)}
}
