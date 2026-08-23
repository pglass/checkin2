package qr

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"testing"
)

func TestPayloadRoundTrip(t *testing.T) {
	p := NewPayload("John", "Smith")
	b, err := p.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != `{"Version":2,"FirstName":"John","LastName":"Smith"}` {
		t.Fatalf("unexpected JSON: %s", b)
	}
	got, err := ParsePayload(string(b))
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != 2 || got.FirstName != "John" || got.LastName != "Smith" {
		t.Fatalf("round-trip mismatch: %+v", got)
	}
}

// A v1 payload carries a single joined Name, which cannot be split back into
// two columns; it parses but reports the old version, and the scan path
// rejects it on the version check.
func TestV1PayloadIsNotCurrentVersion(t *testing.T) {
	got, err := ParsePayload(`{"Version":1,"Name":"John Smith"}`)
	if err != nil {
		t.Fatal(err)
	}
	if got.Version == Version {
		t.Error("a v1 payload must not pass as the current version")
	}
	if got.FirstName != "" || got.LastName != "" {
		t.Errorf("v1 payload yielded name parts: %+v", got)
	}
}

func TestGeneratePDF(t *testing.T) {
	students := make([]Student, 25) // spans multiple pages (12/page)
	for i := range students {
		last := "Family " + string(rune('A'+i%26))
		students[i] = Student{
			First: "Student",
			Last:  last,
			Label: last + ", Student",
		}
	}
	path := filepath.Join(t.TempDir(), "codes.pdf")
	if err := GeneratePDF(students, path); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Size() < 1000 {
		t.Fatalf("PDF suspiciously small: %d bytes", fi.Size())
	}
}

// Letter portrait, in mm -- the page GeneratePDF lays out on.
const (
	testPageW = 215.9
	testPageH = 279.4
)

// Every cell, including the bottom row, must fit above the page's bottom
// margin. fpdf's own bottom margin (~20mm, independent of SetMargins) is what
// a too-tall cell would collide with: it would push the caption onto the next
// page and leave following codes on the wrong page.
func TestGridCellsFitOnPage(t *testing.T) {
	g := newGrid(testPageW, testPageH)

	// The tightest constraint fpdf would apply if auto page breaks were on.
	const fpdfBottomMargin = 20.01
	limit := testPageH - fpdfBottomMargin

	for i := 0; i < g.perPage; i++ {
		s := g.slot(i)
		if got := g.bottom(s); got > limit {
			t.Errorf("slot %d bottom = %.2fmm, want <= %.2fmm; "+
				"the caption would overflow onto the next page", i, got, limit)
		}
		if s.QRY < pdfMargin {
			t.Errorf("slot %d QRY = %.2fmm, above the top margin %.2fmm", i, s.QRY, pdfMargin)
		}
	}
}

// A cell's contents stay inside the cell, so neighbouring codes never collide.
func TestGridSlotStaysInsideItsCell(t *testing.T) {
	g := newGrid(testPageW, testPageH)

	for i := 0; i < g.perPage; i++ {
		s := g.slot(i)
		cellTop := s.QRY
		if used := g.bottom(s) - cellTop; used > g.cellH {
			t.Errorf("slot %d uses %.2fmm of a %.2fmm cell", i, used, g.cellH)
		}
		if s.QRX < s.CellX {
			t.Errorf("slot %d QR starts left of its cell", i)
		}
		if right, cellRight := s.QRX+g.qrSide, s.CellX+g.cellW; right > cellRight+0.01 {
			t.Errorf("slot %d QR right edge %.2f exceeds cell right %.2f", i, right, cellRight)
		}
	}
}

// Slots fill left-to-right, top-to-bottom, and wrap to a new page after
// perPage entries -- the first slot of page 2 sits back at the top-left.
func TestGridSlotOrderAndWrap(t *testing.T) {
	g := newGrid(testPageW, testPageH)

	first, second := g.slot(0), g.slot(1)
	if second.CellX <= first.CellX {
		t.Error("slot 1 should sit to the right of slot 0")
	}
	if second.QRY != first.QRY {
		t.Error("slots 0 and 1 should share a row")
	}

	rowBelow := g.slot(pdfCols)
	if rowBelow.QRY <= first.QRY {
		t.Error("slot after a full row should start a new row")
	}
	if rowBelow.CellX != first.CellX {
		t.Error("a new row should start back at the left column")
	}

	if wrapped := g.slot(g.perPage); wrapped != first {
		t.Errorf("slot %d = %+v, want the page's first slot %+v", g.perPage, wrapped, first)
	}
}

// The generated PDF uses one page per perPage students -- no blank or
// half-used pages from captions spilling over.
func TestGeneratePDFPageCount(t *testing.T) {
	perPage := newGrid(testPageW, testPageH).perPage

	tests := []struct {
		students, wantPages int
	}{
		{1, 1},
		{perPage, 1},       // exactly full
		{perPage + 1, 2},   // one onto the next page
		{2*perPage + 2, 3}, //
	}
	for _, tc := range tests {
		students := make([]Student, tc.students)
		for i := range students {
			students[i] = Student{First: "First", Last: "Last", Label: "Last, First"}
		}
		path := filepath.Join(t.TempDir(), "codes.pdf")
		if err := GeneratePDF(students, path); err != nil {
			t.Fatalf("%d students: %v", tc.students, err)
		}
		if got := pdfPageCount(t, path); got != tc.wantPages {
			t.Errorf("%d students produced %d pages, want %d", tc.students, got, tc.wantPages)
		}
	}
}

// pdfPageCount reads the page count out of a generated PDF's page tree.
func pdfPageCount(t *testing.T, path string) int {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// fpdf writes an uncompressed "/Type /Pages ... /Count N" node.
	re := regexp.MustCompile(`/Type\s*/Pages\b[^>]*?/Count\s+(\d+)`)
	m := re.FindSubmatch(data)
	if m == nil {
		re = regexp.MustCompile(`/Count\s+(\d+)`)
		m = re.FindSubmatch(data)
	}
	if m == nil {
		t.Fatalf("no page count found in %s", path)
	}
	n, err := strconv.Atoi(string(m[1]))
	if err != nil {
		t.Fatal(err)
	}
	return n
}
