package spreadsheet

import (
	"errors"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/xuri/excelize/v2"
)

func TestFromRowsHeadersAndPadding(t *testing.T) {
	// Row 3 is short (excelize truncates trailing empty cells) and row 4 has an
	// interior blank; both must come back padded to the header width.
	got, err := fromRows([][]string{
		{"First", " Last ", "Grade"},
		{"John", "Smith", "4"},
		{"Ada"},
		{"Grace", "", "6"},
	})
	if err != nil {
		t.Fatalf("fromRows: %v", err)
	}
	if want := []string{"First", "Last", "Grade"}; !reflect.DeepEqual(got.Headers, want) {
		t.Errorf("headers = %q, want %q", got.Headers, want)
	}
	if got.HeaderRow != 1 {
		t.Errorf("HeaderRow = %d, want 1", got.HeaderRow)
	}
	want := [][]string{
		{"John", "Smith", "4"},
		{"Ada", "", ""},
		{"Grace", "", "6"},
	}
	if !reflect.DeepEqual(got.Rows, want) {
		t.Errorf("rows = %q, want %q", got.Rows, want)
	}
}

func TestFromRowsSkipsLeadingAndInteriorBlankRows(t *testing.T) {
	got, err := fromRows([][]string{
		{},
		{"  ", ""},
		{"Name"},
		{"John Smith"},
		{"   "},
		{"Ada Lovelace"},
	})
	if err != nil {
		t.Fatalf("fromRows: %v", err)
	}
	if want := []string{"Name"}; !reflect.DeepEqual(got.Headers, want) {
		t.Errorf("headers = %q, want %q", got.Headers, want)
	}
	// Blank rows are dropped, so the header is worksheet row 3, not row 1.
	if got.HeaderRow != 3 {
		t.Errorf("HeaderRow = %d, want 3", got.HeaderRow)
	}
	if want := [][]string{{"John Smith"}, {"Ada Lovelace"}}; !reflect.DeepEqual(got.Rows, want) {
		t.Errorf("rows = %q, want %q", got.Rows, want)
	}
}

func TestFromRowsNamesBlankColumns(t *testing.T) {
	got, err := fromRows([][]string{
		{"First", "", "  ", "Grade"},
		{"John", "x", "y", "4"},
	})
	if err != nil {
		t.Fatalf("fromRows: %v", err)
	}
	// Placeholders use spreadsheet letters so they match what Excel shows.
	want := []string{"First", "(Column B)", "(Column C)", "Grade"}
	if !reflect.DeepEqual(got.Headers, want) {
		t.Errorf("headers = %q, want %q", got.Headers, want)
	}
}

func TestFromRowsNoData(t *testing.T) {
	for _, rows := range [][][]string{nil, {}, {{}, {"", "  "}}} {
		if _, err := fromRows(rows); !errors.Is(err, ErrNoData) {
			t.Errorf("fromRows(%q) error = %v, want ErrNoData", rows, err)
		}
	}
}

// TestFromRowsSkipsNumbersCaptionRow covers the Apple Numbers export shape: the
// table's name lands alone in A1 and the real header sits in row 2.
func TestFromRowsSkipsNumbersCaptionRow(t *testing.T) {
	got, err := fromRows([][]string{
		{"Table 1", "", "", ""},
		{"First Name", "Last Name"},
		{"melissa", "kam"},
		{"paul", "glass"},
	})
	if err != nil {
		t.Fatalf("fromRows: %v", err)
	}
	if want := []string{"First Name", "Last Name"}; !reflect.DeepEqual(got.Headers, want) {
		t.Errorf("headers = %q, want %q", got.Headers, want)
	}
	if got.HeaderRow != 2 {
		t.Errorf("HeaderRow = %d, want 2", got.HeaderRow)
	}
	want := []Name{{First: "melissa", Last: "kam"}, {First: "paul", Last: "glass"}}
	if !reflect.DeepEqual(got.Names(0, 1), want) {
		t.Errorf("names = %+v, want %+v", got.Names(0, 1), want)
	}
}

// A genuine single-column sheet must not be mistaken for a caption row: the row
// under the header is also one cell wide, which is what distinguishes the two.
func TestFromRowsKeepsSingleColumnHeader(t *testing.T) {
	got, err := fromRows([][]string{
		{"Student Name"},
		{"John Smith"},
		{"Ada Lovelace"},
	})
	if err != nil {
		t.Fatalf("fromRows: %v", err)
	}
	if want := []string{"Student Name"}; !reflect.DeepEqual(got.Headers, want) {
		t.Errorf("headers = %q, want %q", got.Headers, want)
	}
	if got.HeaderRow != 1 {
		t.Errorf("HeaderRow = %d, want 1", got.HeaderRow)
	}
}

// A single-row sheet has no row below to compare against; it is the header.
func TestFromRowsSingleRow(t *testing.T) {
	got, err := fromRows([][]string{{"Only", "Header"}})
	if err != nil {
		t.Fatalf("fromRows: %v", err)
	}
	if got.HeaderRow != 1 || len(got.Rows) != 0 {
		t.Errorf("HeaderRow = %d, rows = %d; want 1 and 0", got.HeaderRow, len(got.Rows))
	}
}

func TestWithHeaderRow(t *testing.T) {
	sheet, err := fromRows([][]string{
		{"Table 1"},
		{"First Name", "Last Name"},
		{"melissa", "kam"},
	})
	if err != nil {
		t.Fatalf("fromRows: %v", err)
	}

	// Override back up to the caption row: it becomes a one-column header and
	// the former header row becomes data.
	got, err := sheet.WithHeaderRow(1)
	if err != nil {
		t.Fatalf("WithHeaderRow(1): %v", err)
	}
	if want := []string{"Table 1"}; !reflect.DeepEqual(got.Headers, want) {
		t.Errorf("headers = %q, want %q", got.Headers, want)
	}
	// One column can no longer produce a name: both parts are required.
	if got := got.Names(0, -1); got != nil {
		t.Errorf("Names(0,-1) = %+v, want nil", got)
	}
	// The receiver is unchanged: WithHeaderRow returns a new Sheet.
	if sheet.HeaderRow != 2 {
		t.Errorf("receiver HeaderRow = %d, want 2", sheet.HeaderRow)
	}

	if _, err := sheet.WithHeaderRow(99); err == nil {
		t.Error("WithHeaderRow(99): want error, got nil")
	}
}

// Blank rows are dropped, so the header-row numbers offered to the user skip
// them and stay aligned with the worksheet's own row numbering.
func TestHeaderRowOptionsSkipBlanks(t *testing.T) {
	sheet, err := fromRows([][]string{
		{""},
		{"Name"},
		{""},
		{"John Smith"},
	})
	if err != nil {
		t.Fatalf("fromRows: %v", err)
	}
	if want := []int{2, 4}; !reflect.DeepEqual(sheet.HeaderRowOptions(), want) {
		t.Errorf("HeaderRowOptions() = %v, want %v", sheet.HeaderRowOptions(), want)
	}
	if sheet.NumRows() != 2 {
		t.Errorf("NumRows() = %d, want 2", sheet.NumRows())
	}
	if got := sheet.PreviewRow(0); got != "Name" {
		t.Errorf("PreviewRow(0) = %q, want %q", got, "Name")
	}
	if got := sheet.PreviewRow(9); got != "" {
		t.Errorf("PreviewRow(9) = %q, want empty", got)
	}
}

func TestNames(t *testing.T) {
	sheet := &Sheet{
		Headers: []string{"First", "Last"},
		Rows: [][]string{
			{"John", "Smith"},
			{"Ada", ""},       // missing last name: not a complete student
			{"", "Hopper"},    // missing first name: likewise
			{"", ""},          // fully blank
			{" Grace ", "H."}, // parts are trimmed
		},
	}

	t.Run("both columns", func(t *testing.T) {
		want := []Name{{First: "John", Last: "Smith"}, {First: "Grace", Last: "H."}}
		if got := sheet.Names(0, 1); !reflect.DeepEqual(got, want) {
			t.Errorf("Names(0,1) = %+v, want %+v", got, want)
		}
	})

	t.Run("out of range first column", func(t *testing.T) {
		if got := sheet.Names(-1, 1); got != nil {
			t.Errorf("Names(-1,1) = %+v, want nil", got)
		}
		if got := sheet.Names(9, 1); got != nil {
			t.Errorf("Names(9,1) = %+v, want nil", got)
		}
	})

	// Both columns are required, so an unset or out-of-range last column
	// yields nothing rather than falling back to the first column alone.
	t.Run("missing or out of range last column", func(t *testing.T) {
		if got := sheet.Names(0, -1); got != nil {
			t.Errorf("Names(0,-1) = %+v, want nil", got)
		}
		if got := sheet.Names(0, 9); got != nil {
			t.Errorf("Names(0,9) = %+v, want nil", got)
		}
	})
}

// TestOpen exercises the real excelize path end to end, including that only the
// first worksheet is read.
func TestOpen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "students.xlsx")

	f := excelize.NewFile()
	defer f.Close()
	first := f.GetSheetName(0)
	for i, row := range [][]string{
		{"First Name", "Last Name"},
		{"John", "Smith"},
		{" Ada ", " Lovelace "},
	} {
		cell, err := excelize.CoordinatesToCellName(1, i+1)
		if err != nil {
			t.Fatal(err)
		}
		if err := f.SetSheetRow(first, cell, &row); err != nil {
			t.Fatal(err)
		}
	}
	// A second sheet that must be ignored.
	if _, err := f.NewSheet("Other"); err != nil {
		t.Fatal(err)
	}
	if err := f.SetCellValue("Other", "A1", "Ignored"); err != nil {
		t.Fatal(err)
	}
	if err := f.SaveAs(path); err != nil {
		t.Fatal(err)
	}

	sheet, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if want := []string{"First Name", "Last Name"}; !reflect.DeepEqual(sheet.Headers, want) {
		t.Errorf("headers = %q, want %q", sheet.Headers, want)
	}
	// Cell values are trimmed on read.
	want := []Name{{First: "John", Last: "Smith"}, {First: "Ada", Last: "Lovelace"}}
	if !reflect.DeepEqual(sheet.Names(0, 1), want) {
		t.Errorf("names = %+v, want %+v", sheet.Names(0, 1), want)
	}
}

func TestOpenMissingFile(t *testing.T) {
	if _, err := Open(filepath.Join(t.TempDir(), "nope.xlsx")); err == nil {
		t.Fatal("Open of a missing file: want error, got nil")
	}
}

func TestDetectHeaderByNameColumns(t *testing.T) {
	tests := []struct {
		name      string
		rows      [][]string
		wantRow   int
		wantFirst string
		wantLast  string
	}{
		{
			// Name columns in row 1 win outright, even though row 1 is also a
			// perfectly ordinary first row.
			name:      "row 1 headers",
			rows:      [][]string{{"First Name", "Last Name"}, {"john", "smith"}},
			wantRow:   1,
			wantFirst: "First Name",
			wantLast:  "Last Name",
		},
		{
			// A title row above the header: the caption rule would also land on
			// row 2 here, but the name match is what decides it.
			name:      "title row above headers",
			rows:      [][]string{{"Roster 2026", "", ""}, {"First Name", "Last Name"}, {"john", "smith"}},
			wantRow:   2,
			wantFirst: "First Name",
			wantLast:  "Last Name",
		},
		{
			// A multi-cell title row is not a caption, so only the name match
			// can find the header here.
			name:      "wide title row above headers",
			rows:      [][]string{{"Fall Roster", "Updated 2026"}, {"Given Name", "Surname"}, {"john", "smith"}},
			wantRow:   2,
			wantFirst: "Given Name",
			wantLast:  "Surname",
		},
		{
			name:      "single full name column",
			rows:      [][]string{{"Student Name", "Grade"}, {"John Smith", "4"}},
			wantRow:   1,
			wantFirst: "Student Name",
			wantLast:  "",
		},
		{
			name:      "punctuation and case are ignored",
			rows:      [][]string{{"FIRST_NAME", "last-name"}, {"john", "smith"}},
			wantRow:   1,
			wantFirst: "FIRST_NAME",
			wantLast:  "last-name",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := fromRows(tc.rows)
			if err != nil {
				t.Fatalf("fromRows: %v", err)
			}
			if got.HeaderRow != tc.wantRow {
				t.Errorf("HeaderRow = %d, want %d", got.HeaderRow, tc.wantRow)
			}
			first, last, ok := got.MatchNameColumns()
			if !ok {
				t.Fatal("MatchNameColumns: ok = false, want true")
			}
			if gotFirst := got.Headers[first]; gotFirst != tc.wantFirst {
				t.Errorf("first column = %q, want %q", gotFirst, tc.wantFirst)
			}
			gotLast := ""
			if last >= 0 {
				gotLast = got.Headers[last]
			}
			if gotLast != tc.wantLast {
				t.Errorf("last column = %q, want %q", gotLast, tc.wantLast)
			}
		})
	}
}

// A more specific title beats a generic one even when the generic column comes
// first, so "Name" does not shadow "Last Name".
func TestMatchNameColumnsPrefersSpecificTitles(t *testing.T) {
	s := &Sheet{Headers: []string{"Name", "Last Name"}}
	first, last, ok := s.MatchNameColumns()
	if !ok {
		t.Fatal("ok = false, want true")
	}
	// "Last Name" is the only first/last-style column, so it is taken as the
	// name column and there is no separate last column.
	if first != 1 || last != -1 {
		t.Errorf("first, last = %d, %d; want 1, -1", first, last)
	}
}

func TestMatchNameColumnsNoMatch(t *testing.T) {
	s := &Sheet{Headers: []string{"ID", "Grade", "Homeroom"}}
	if _, _, ok := s.MatchNameColumns(); ok {
		t.Error("ok = true, want false for headers with no name column")
	}
}

// With no name columns anywhere, detection falls back to the structural rules
// and leaves the columns for the user to pick.
func TestDetectHeaderFallsBackWithoutNameColumns(t *testing.T) {
	got, err := fromRows([][]string{
		{"Table 1"},
		{"Col A", "Col B"},
		{"x", "y"},
	})
	if err != nil {
		t.Fatalf("fromRows: %v", err)
	}
	if got.HeaderRow != 2 {
		t.Errorf("HeaderRow = %d, want 2 (caption rule)", got.HeaderRow)
	}
	if _, _, ok := got.MatchNameColumns(); ok {
		t.Error("ok = true, want false")
	}
}

// Only the first two non-blank rows are offered as header candidates.
func TestHeaderRowOptionsBoundedToTwo(t *testing.T) {
	got, err := fromRows([][]string{
		{"Table 1"},
		{"First Name", "Last Name"},
		{"john", "smith"},
		{"ada", "lovelace"},
	})
	if err != nil {
		t.Fatalf("fromRows: %v", err)
	}
	if want := []int{1, 2}; !reflect.DeepEqual(got.HeaderRowOptions(), want) {
		t.Errorf("HeaderRowOptions() = %v, want %v", got.HeaderRowOptions(), want)
	}
	// A row past the bound is not selectable as a header.
	if _, err := got.WithHeaderRow(3); err == nil {
		t.Error("WithHeaderRow(3): want error, got nil")
	}
}

// WithoutHeaderRow treats the first row as data and names columns by letter,
// for a file that has no titles at all.
func TestWithoutHeaderRow(t *testing.T) {
	sheet, err := fromRows([][]string{
		{"john", "smith"},
		{"ada", "lovelace"},
	})
	if err != nil {
		t.Fatalf("fromRows: %v", err)
	}

	got := sheet.WithoutHeaderRow()
	if want := []string{"Column A", "Column B"}; !reflect.DeepEqual(got.Headers, want) {
		t.Errorf("headers = %q, want %q", got.Headers, want)
	}
	// No row is consumed as a header: both rows are students.
	want := []Name{{First: "john", Last: "smith"}, {First: "ada", Last: "lovelace"}}
	if !reflect.DeepEqual(got.Names(0, 1), want) {
		t.Errorf("names = %+v, want %+v", got.Names(0, 1), want)
	}
	if got.HeaderRow != 0 {
		t.Errorf("HeaderRow = %d, want 0 (meaning no header)", got.HeaderRow)
	}
}

// Rows are ragged in practice; every row must be padded to the widest so column
// indexing cannot run off the end.
func TestWithoutHeaderRowPadsToWidestRow(t *testing.T) {
	sheet, err := fromRows([][]string{
		{"john"},
		{"ada", "lovelace", "extra"},
	})
	if err != nil {
		t.Fatalf("fromRows: %v", err)
	}

	got := sheet.WithoutHeaderRow()
	if len(got.Headers) != 3 {
		t.Fatalf("headers = %q, want 3 columns", got.Headers)
	}
	for i, r := range got.Rows {
		if len(r) != 3 {
			t.Errorf("row %d has %d cells, want 3", i, len(r))
		}
	}
	// The padded "john" row has no last name, so it is not an importable
	// student; only the complete row survives.
	want := []Name{{First: "ada", Last: "lovelace"}}
	if !reflect.DeepEqual(got.Names(0, 1), want) {
		t.Errorf("names = %+v, want %+v", got.Names(0, 1), want)
	}
}

// Toggling the header off and back on returns to the original reading.
func TestWithoutHeaderRowRoundTrip(t *testing.T) {
	sheet, err := fromRows([][]string{
		{"First Name", "Last Name"},
		{"john", "smith"},
	})
	if err != nil {
		t.Fatalf("fromRows: %v", err)
	}

	back, err := sheet.WithoutHeaderRow().WithHeaderRow(1)
	if err != nil {
		t.Fatalf("WithHeaderRow: %v", err)
	}
	if !reflect.DeepEqual(back.Headers, sheet.Headers) {
		t.Errorf("headers = %q, want %q", back.Headers, sheet.Headers)
	}
	if !reflect.DeepEqual(back.Rows, sheet.Rows) {
		t.Errorf("rows = %q, want %q", back.Rows, sheet.Rows)
	}
}
