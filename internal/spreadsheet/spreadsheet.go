// Package spreadsheet reads student data out of Excel workbooks.
//
// Only the first worksheet is read: an import is a flat list of students, and
// picking a sheet is one more decision to put in front of the user for a case
// that has not come up. Blank rows are dropped, the header row is guessed (see
// detectHeader) and can be overridden with WithHeaderRow, and everything below
// it is data.
package spreadsheet

import (
	"errors"
	"fmt"
	"strings"
	"unicode"

	"github.com/xuri/excelize/v2"
)

// Extensions are the file suffixes Open accepts, for wiring into a file picker.
// These are the zip-based OOXML formats excelize reads; the legacy binary .xls
// is not among them.
var Extensions = []string{".xlsx", ".xlsm", ".xltx", ".xltm"}

// ErrNoData is returned when the workbook has no usable header row.
var ErrNoData = errors.New("spreadsheet has no header row")

// Name is one student's name as read from two spreadsheet columns.
type Name struct {
	First string
	Last  string
}

// Sheet is one worksheet flattened into a header row plus data rows.
type Sheet struct {
	// Headers are the column titles, in column order. Blank columns are given
	// a placeholder name so every column stays addressable by index.
	Headers []string
	// Rows are the data rows below the header. Rows are padded to len(Headers)
	// so Row[i] always lines up with Headers[i]; excelize truncates trailing
	// empty cells, so unpadded rows would be short and index lookups would panic.
	Rows [][]string

	// HeaderRow is the 1-based worksheet row the headers were read from, as
	// shown in Excel's row gutter. Reported so the UI can display and adjust it.
	HeaderRow int

	// raw is every non-blank row of the worksheet, kept so the header row can
	// be moved without re-reading the file.
	raw [][]string
	// rawNums are the 1-based worksheet row numbers of raw, parallel to it.
	// Blank rows are dropped from raw, so index arithmetic would not recover
	// the original numbering.
	rawNums []int
}

// NumRows is the count of non-blank rows in the worksheet, header included.
func (s *Sheet) NumRows() int { return len(s.raw) }

// Open reads the first worksheet of the workbook at path.
func Open(path string) (*Sheet, error) {
	f, err := excelize.OpenFile(path)
	if err != nil {
		return nil, fmt.Errorf("open spreadsheet: %w", err)
	}
	defer f.Close()

	sheets := f.GetSheetList()
	if len(sheets) == 0 {
		return nil, ErrNoData
	}
	rows, err := f.GetRows(sheets[0])
	if err != nil {
		return nil, fmt.Errorf("read sheet %q: %w", sheets[0], err)
	}
	return fromRows(rows)
}

// fromRows turns raw worksheet rows into a Sheet, choosing the header row with
// detectHeader. Split out from Open so the header rules can be tested without a
// real file on disk.
func fromRows(rows [][]string) (*Sheet, error) {
	// Blank rows carry no data and only complicate the row indexing, so they
	// are dropped up front — including interior ones, which appear as styled
	// empty rows in exports from Numbers and Google Sheets. rawNums keeps each
	// surviving row's original worksheet number for display.
	var raw [][]string
	var rawNums []int
	for i, r := range rows {
		if isBlank(r) {
			continue
		}
		raw = append(raw, r)
		rawNums = append(rawNums, i+1)
	}
	if len(raw) == 0 {
		return nil, ErrNoData
	}

	s := &Sheet{raw: raw, rawNums: rawNums}
	return s.WithHeaderRow(rawNums[detectHeader(raw)])
}

// maxHeaderRow bounds how far down a header can be found, as a count of
// non-blank rows. A header is either the first row or — when something like a
// title or table caption sits above it — the second; past that a file is
// unusual enough that guessing does more harm than asking.
const maxHeaderRow = 2

// detectHeader returns the index into rows of the most likely header row,
// always 0 or 1.
//
// Recognizable name columns are the strongest evidence available, so a row
// containing them wins outright. Failing that, a lone cell in the first row is
// read as a caption — Apple Numbers exports a table's *name* that way, above
// the real header — and the row beneath it is taken instead. A genuine
// one-column sheet has a second row that is also one cell wide, so it is not
// caught by that rule. Otherwise the first row is the header.
func detectHeader(rows [][]string) int {
	limit := min(len(rows), maxHeaderRow)

	// Direct evidence first: a row that names name-columns is the header
	// whatever its shape.
	for i := range limit {
		if _, _, ok := matchNameColumns(rows[i]); ok {
			return i
		}
	}

	// Structural fallback: a lone leading cell is a caption, not a header.
	if len(rows) >= 2 && countNonEmpty(rows[0]) == 1 && countNonEmpty(rows[1]) > 1 {
		return 1
	}
	return 0
}

// firstNameHeaders, lastNameHeaders, and fullNameHeaders are the column titles
// recognized as holding student names, in normalized form (see normalizeHeader).
// Order matters: earlier entries win, so the more specific title is preferred
// when a sheet has several plausible columns.
var (
	firstNameHeaders = []string{"firstname", "first", "givenname", "given", "forename"}
	lastNameHeaders  = []string{"lastname", "last", "surname", "familyname", "family"}
	fullNameHeaders  = []string{"studentname", "fullname", "name"}
)

// MatchNameColumns finds the columns holding student names.
//
// It returns the index of the column to use as the first name and, when the
// sheet splits names across two columns, the index of the last name column
// (-1 when there is none). ok is false when no column is recognizable, leaving
// the choice to the user.
func (s *Sheet) MatchNameColumns() (first, last int, ok bool) {
	return matchNameColumns(s.Headers)
}

// matchNameColumns implements MatchNameColumns against a raw header row, so
// detectHeader can test a candidate row before committing to it.
func matchNameColumns(headers []string) (first, last int, ok bool) {
	firstIdx := indexOfHeader(headers, firstNameHeaders)
	lastIdx := indexOfHeader(headers, lastNameHeaders)

	// A first/last pair is the most specific reading. A lone "First Name" also
	// counts: the last name column is simply absent.
	if firstIdx >= 0 {
		return firstIdx, lastIdx, true
	}

	// A lone last-name column is checked before the generic full-name titles:
	// "Last Name" is the more specific match, and a sheet with both it and a
	// plain "Name" column should not have "Name" win on column order alone.
	if lastIdx >= 0 {
		return lastIdx, -1, true
	}

	// No first/last columns, but a whole-name column works on its own.
	if fullIdx := indexOfHeader(headers, fullNameHeaders); fullIdx >= 0 {
		return fullIdx, -1, true
	}
	return -1, -1, false
}

// indexOfHeader returns the index of the first header matching any of want,
// preferring earlier entries in want over earlier columns: "Last Name" should
// beat "Name" even when "Name" appears in an earlier column.
func indexOfHeader(headers []string, want []string) int {
	for _, w := range want {
		for i, h := range headers {
			if normalizeHeader(h) == w {
				return i
			}
		}
	}
	return -1
}

// normalizeHeader reduces a column title to letters and digits, lowercased, so
// "First Name", "first_name", and "FirstName" all compare equal.
func normalizeHeader(h string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(h) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// WithHeaderRow re-reads the sheet using the given 1-based worksheet row as the
// header. Rows above it are discarded and rows below become the data. The
// returned Sheet shares the underlying raw rows with the receiver.
func (s *Sheet) WithHeaderRow(row int) (*Sheet, error) {
	idx := -1
	for i, n := range s.HeaderRowOptions() {
		if n == row {
			idx = i
			break
		}
	}
	if idx < 0 {
		return nil, fmt.Errorf("row %d is not a usable header row", row)
	}

	headers := make([]string, len(s.raw[idx]))
	for i, h := range s.raw[idx] {
		h = strings.TrimSpace(h)
		if h == "" {
			// Unnamed column: still selectable, and labeled so two blanks are
			// distinguishable in the dropdown.
			h = fmt.Sprintf("(Column %s)", columnName(i))
		}
		headers[i] = h
	}

	out := make([][]string, 0, len(s.raw)-idx-1)
	for _, r := range s.raw[idx+1:] {
		padded := make([]string, len(headers))
		for i := range headers {
			if i < len(r) {
				padded[i] = strings.TrimSpace(r[i])
			}
		}
		out = append(out, padded)
	}
	return &Sheet{
		Headers:   headers,
		Rows:      out,
		HeaderRow: row,
		raw:       s.raw,
		rawNums:   s.rawNums,
	}, nil
}

// WithoutHeaderRow re-reads the sheet treating every row as data, for a file
// whose first row is already a student rather than column titles. Columns are
// named by their spreadsheet letter, since there are no titles to use.
func (s *Sheet) WithoutHeaderRow() *Sheet {
	width := 0
	for _, r := range s.raw {
		width = max(width, len(r))
	}

	headers := make([]string, width)
	for i := range headers {
		headers[i] = "Column " + columnName(i)
	}

	out := make([][]string, 0, len(s.raw))
	for _, r := range s.raw {
		padded := make([]string, width)
		for i := range headers {
			if i < len(r) {
				padded[i] = strings.TrimSpace(r[i])
			}
		}
		out = append(out, padded)
	}
	return &Sheet{
		Headers: headers,
		Rows:    out,
		// HeaderRow 0 means "no header row": worksheet rows are 1-based, so it
		// cannot collide with a real row number.
		HeaderRow: 0,
		raw:       s.raw,
		rawNums:   s.rawNums,
	}
}

// HeaderRowOptions lists the worksheet row numbers that can serve as the header,
// as 1-based numbers matching Excel's row gutter. Only the first maxHeaderRow
// non-blank rows are offered: a header further down than that is not a case
// worth presenting, and a long list of rows obscures the two that matter.
func (s *Sheet) HeaderRowOptions() []int {
	return s.rawNums[:min(len(s.rawNums), maxHeaderRow)]
}

// PreviewRow renders the non-blank row at index i as a short comma-joined
// string, for showing the user what a candidate header row contains.
func (s *Sheet) PreviewRow(i int) string {
	if i < 0 || i >= len(s.raw) {
		return ""
	}
	var cells []string
	for _, c := range s.raw[i] {
		if c = strings.TrimSpace(c); c != "" {
			cells = append(cells, c)
		}
	}
	return strings.Join(cells, ", ")
}

func countNonEmpty(row []string) int {
	n := 0
	for _, c := range row {
		if strings.TrimSpace(c) != "" {
			n++
		}
	}
	return n
}

// columnName renders a zero-based column index as its spreadsheet letter (0 =
// "A"), so placeholder headers match what the user sees in Excel.
func columnName(i int) string {
	name, err := excelize.ColumnNumberToName(i + 1)
	if err != nil {
		return fmt.Sprintf("%d", i+1)
	}
	return name
}

func isBlank(row []string) bool {
	for _, c := range row {
		if strings.TrimSpace(c) != "" {
			return false
		}
	}
	return true
}

// Names builds the student names for the given column selections.
//
// Both column indexes are required: a student is identified by the (last,
// first) pair, so neither part can be inferred from the other. Rows missing
// either part are skipped — a blank cell is not a student.
func (s *Sheet) Names(firstIdx, lastIdx int) []Name {
	if firstIdx < 0 || firstIdx >= len(s.Headers) {
		return nil
	}
	if lastIdx < 0 || lastIdx >= len(s.Headers) {
		return nil
	}
	names := make([]Name, 0, len(s.Rows))
	for _, r := range s.Rows {
		n := Name{
			First: strings.TrimSpace(r[firstIdx]),
			Last:  strings.TrimSpace(r[lastIdx]),
		}
		// Both parts identify a student, so a row missing either one carries no
		// importable name and is skipped rather than imported half-blank.
		if n.First == "" || n.Last == "" {
			continue
		}
		names = append(names, n)
	}
	return names
}
