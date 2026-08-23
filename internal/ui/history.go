package ui

import (
	"bufio"
	"fmt"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"

	"github.com/pglass/checkin/internal/store"
)

// historyUILimit caps how many rows the Retrieve view loads into the results
// pane. Larger result sets must be exported via Save.
const historyUILimit = 10000

// maxHistoryStudents bounds the student multi-select (keeps the IN-list small).
const maxHistoryStudents = 10

// formatHistoryLine renders one event as
// "2026-07-19 07:31:30 PM CST -- John Smith checked in by Jane Smith".
//
// The "by <adult>" suffix is dropped for rows with no authorized adult on
// record -- rows written before the field existed, or where none was entered.
func formatHistoryLine(r store.HistoryRow) string {
	verb := "checked in"
	if r.Action == store.ActionCheckedOut {
		verb = "checked out"
	}
	line := fmt.Sprintf("%s -- %s %s",
		r.Timestamp.Format("2006-01-02 03:04:05 PM MST"), r.StudentName, verb)
	if r.AuthorizedAdult != "" {
		line += " by " + r.AuthorizedAdult
	}
	return line
}

// history holds the widgets and state for one History window.
type history struct {
	app *App
	win fyne.Window

	startDate *widget.DateEntry
	endDate   *widget.DateEntry
	students  *studentSelect
	sortSel   *widget.Select
	summary   *widget.Label
	capNote   *widget.Label

	retrieveBtn *widget.Button
	saveBtn     *widget.Button

	results  *readOnlyEntry // read-only, selectable, normal colors
	countLbl *widget.Label
	overflow *widget.Label

	loading     *fyne.Container // centered spinner + "Loading…", shown while querying
	loadingSpin *widget.Activity
}

// showHistoryWindow opens the History window, or raises the existing one.
func (a *App) showHistoryWindow() {
	if a.historyWin != nil {
		a.historyWin.RequestFocus()
		return
	}

	rows, err := loadRows(a.ctx, a.store)
	if err != nil {
		dialog.ShowError(err, a.win)
		return
	}

	h := &history{app: a}
	h.win = a.fyneApp.NewWindow("History")
	h.build(rows)

	split := container.NewHSplit(h.queryPane(), h.resultsPane())
	split.Offset = 0.4 // query builder on the left, wider results on the right
	h.win.SetContent(withWindowMargin(split))
	h.win.Resize(fyne.NewSize(920, 620))
	h.win.SetOnClosed(func() { a.historyWin = nil })
	a.historyWin = h.win
	h.win.Show()
}

// build constructs the query-builder widgets and wires live-summary updates.
//
// All widgets that updateSummary() reads are created BEFORE any OnChanged
// callback is attached or any initial value is set — otherwise setting the
// sort dropdown's initial index fires OnChanged into a half-built struct and
// dereferences a nil widget.
func (h *history) build(rows []store.StudentRow) {
	// 1) Create every widget first (no callbacks, no initial values yet).
	h.startDate = widget.NewDateEntry()
	h.endDate = widget.NewDateEntry()
	h.capNote = widget.NewLabel("")
	h.students = newStudentSelectWithOptions(rows, false, maxHistoryStudents, nil)
	h.sortSel = widget.NewSelect([]string{"Event Time", "Student Name"}, nil)
	h.summary = widget.NewLabel("")
	h.summary.Wrapping = fyne.TextWrapWord
	h.retrieveBtn = newPrimaryButton("Retrieve", h.onRetrieve)
	h.saveBtn = widget.NewButton("Save History to File…", h.onSave)

	// Results pane: read-only but still selectable/copyable (and, unlike a
	// disabled Entry, rendered with normal foreground color so it stays readable).
	h.results = newReadOnlyEntry()
	h.results.Wrapping = fyne.TextWrapOff
	h.countLbl = widget.NewLabel("")
	h.overflow = widget.NewLabel("")
	h.overflow.Wrapping = fyne.TextWrapWord
	h.overflow.Hide()

	// Loading overlay: the same spinner used by the PDF progress view, centered
	// with a "Loading…" label, shown over the results while a query runs.
	h.loadingSpin = widget.NewActivity()
	h.loading = container.NewCenter(container.NewVBox(
		container.NewCenter(h.loadingSpin),
		widget.NewLabel("Loading…"),
	))
	h.loading.Hide()

	// 2) Set initial values (sortSel has no callback yet, so this is safe).
	h.sortSel.SetSelectedIndex(0)

	// 3) Now wire the live-summary callbacks; the struct is fully built.
	h.startDate.OnChanged = func(*time.Time) { h.updateSummary() }
	h.endDate.OnChanged = func(*time.Time) { h.updateSummary() }
	h.sortSel.OnChanged = func(string) { h.updateSummary() }
	h.students.onChange = h.updateSummary
	h.students.onCapHit = func() {
		h.capNote.SetText(fmt.Sprintf("Max %d students.", maxHistoryStudents))
	}

	h.updateSummary()
}

// queryPane is the query builder (left pane).
func (h *history) queryPane() fyne.CanvasObject {
	dates := container.NewVBox(
		widget.NewLabel("Start Date (optional):"),
		container.NewBorder(nil, nil, nil, widget.NewButton("Clear", func() { h.clearDate(h.startDate) }), h.startDate),
		widget.NewLabel("End Date (optional):"),
		container.NewBorder(nil, nil, nil, widget.NewButton("Clear", func() { h.clearDate(h.endDate) }), h.endDate),
	)

	studentsBox := container.NewBorder(
		widget.NewLabel("Students (none = all, max 10):"),
		h.capNote, nil, nil,
		h.students.list,
	)

	sortBox := container.NewBorder(nil, nil, widget.NewLabel("Sort by:"), nil, h.sortSel)

	buttons := container.NewGridWithColumns(2, h.retrieveBtn, h.saveBtn)

	// Students list gets the flexible middle; the rest is fixed top/bottom.
	top := container.NewVBox(dates, widget.NewSeparator())
	bottom := container.NewVBox(widget.NewSeparator(), sortBox,
		widget.NewLabel("Summary:"), h.summary, buttons)
	return container.NewBorder(top, bottom, nil, nil, studentsBox)
}

// resultsPane is the results view (right pane). The loading overlay is stacked
// over the results text so it covers the pane while a query runs.
func (h *history) resultsPane() fyne.CanvasObject {
	status := container.NewVBox(h.countLbl, h.overflow)
	body := container.NewStack(h.results, h.loading)
	return container.NewBorder(nil, status, nil, nil, body)
}

func (h *history) clearDate(de *widget.DateEntry) {
	de.SetText("")
	h.updateSummary()
}

// currentQuery reads the widgets into a store.HistoryQuery.
func (h *history) currentQuery() store.HistoryQuery {
	q := store.HistoryQuery{
		Start:        h.startDate.Date,
		End:          h.endDate.Date,
		StudentNames: h.students.selectedNames(),
	}
	if h.sortSel.SelectedIndex() == 1 {
		q.Sort = store.SortStudentName
	}
	return q
}

// updateSummary rebuilds the human-readable query description.
func (h *history) updateSummary() {
	var b strings.Builder
	b.WriteString("Will retrieve history")

	start, end := h.startDate.Date, h.endDate.Date
	if start == nil && end == nil {
		b.WriteString(" for all dates")
	} else {
		if start != nil {
			b.WriteString(" from Start Date: " + start.Format("01/02/2006"))
		}
		if end != nil {
			b.WriteString(" up to End Date: " + end.Format("01/02/2006"))
		}
	}

	names := h.students.selectedNames()
	if len(names) == 0 {
		b.WriteString(" for all students")
	} else {
		b.WriteString(" for Students: " + strings.Join(names, ", "))
	}

	field := "event time"
	if h.sortSel.SelectedIndex() == 1 {
		field = "student name"
	}
	b.WriteString(" sorted by " + field + " in ascending order.")

	h.summary.SetText(b.String())
}

// onRetrieve runs the query (capped) on a goroutine and fills the results pane.
func (h *history) onRetrieve() {
	q := h.currentQuery()
	h.setBusy(true)
	go func() {
		rows, total, err := h.app.store.History(h.app.ctx, q, historyUILimit)
		if err != nil {
			fyne.Do(func() {
				h.setBusy(false)
				dialog.ShowError(err, h.win)
			})
			return
		}
		var body strings.Builder
		body.Grow(len(rows) * 48)
		for _, r := range rows {
			body.WriteString(formatHistoryLine(r))
			body.WriteByte('\n')
		}
		fyne.Do(func() {
			h.setBusy(false)
			h.results.SetText(body.String())
			h.countLbl.SetText(fmt.Sprintf("Displaying %d records out of %d matches", len(rows), total))
			if total > historyUILimit {
				h.overflow.SetText(fmt.Sprintf(
					"Use 'Save History to File...' button to retrieve all %d results", total))
				h.overflow.Show()
			} else {
				h.overflow.Hide()
			}
		})
	}()
}

// onSave prompts for a file, then writes the full (unbounded) result set to it.
func (h *history) onSave() {
	q := h.currentQuery()
	d := dialog.NewFileSave(func(wc fyne.URIWriteCloser, err error) {
		if err != nil {
			dialog.ShowError(err, h.win)
			return
		}
		if wc == nil {
			return // cancelled
		}
		h.setBusy(true)
		go func() {
			// A very large limit means "all rows" to SQLite.
			rows, _, qerr := h.app.store.History(h.app.ctx, q, 1<<62)
			var werr error
			if qerr == nil {
				bw := bufio.NewWriter(wc)
				for _, r := range rows {
					if _, werr = bw.WriteString(formatHistoryLine(r) + "\n"); werr != nil {
						break
					}
				}
				if werr == nil {
					werr = bw.Flush()
				}
			}
			cerr := wc.Close()
			fyne.Do(func() {
				h.setBusy(false)
				if qerr != nil {
					dialog.ShowError(qerr, h.win)
					return
				}
				if werr != nil {
					dialog.ShowError(werr, h.win)
					return
				}
				if cerr != nil {
					dialog.ShowError(cerr, h.win)
					return
				}
				dialog.ShowInformation("History", fmt.Sprintf("Saved %d records.", len(rows)), h.win)
			})
		}()
	}, h.win)
	d.SetFileName("history-" + time.Now().Format("20060102-150405") + ".txt")
	d.Show()
}

// setBusy toggles the action buttons during a query so it can't double-run, and
// shows the loading overlay (spinner + "Loading…") over the results pane.
func (h *history) setBusy(busy bool) {
	if busy {
		h.retrieveBtn.Disable()
		h.saveBtn.Disable()
		h.loadingSpin.Start()
		h.loading.Show()
	} else {
		h.retrieveBtn.Enable()
		h.saveBtn.Enable()
		h.loadingSpin.Stop()
		h.loading.Hide()
	}
}

// readOnlyEntry is a multi-line Entry that cannot be edited by the user but
// remains focusable, selectable, and copyable. Unlike a disabled Entry it keeps
// the normal (not greyed) text color, so large result text stays readable.
// SetText still works because content is set programmatically, not typed.
type readOnlyEntry struct {
	widget.Entry
}

func newReadOnlyEntry() *readOnlyEntry {
	e := &readOnlyEntry{}
	e.MultiLine = true
	e.ExtendBaseWidget(e)
	return e
}

// TypedRune drops character input (no editing).
func (e *readOnlyEntry) TypedRune(_ rune) {}

// TypedKey allows cursor/selection navigation but drops editing keys.
func (e *readOnlyEntry) TypedKey(key *fyne.KeyEvent) {
	switch key.Name {
	case fyne.KeyBackspace, fyne.KeyDelete, fyne.KeyReturn, fyne.KeyEnter, fyne.KeyTab:
		return // ignore edits
	}
	e.Entry.TypedKey(key)
}

// TypedShortcut allows copy/select-all but blocks paste and cut.
func (e *readOnlyEntry) TypedShortcut(s fyne.Shortcut) {
	switch s.(type) {
	case *fyne.ShortcutPaste, *fyne.ShortcutCut:
		return // ignore edits
	}
	e.Entry.TypedShortcut(s)
}
