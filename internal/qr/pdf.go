package qr

import (
	"bytes"
	"fmt"
	"image/png"
	"runtime"
	"sync"
	"sync/atomic"

	"github.com/go-pdf/fpdf"
)

// PDF layout: codes arranged in a grid, student name centered underneath each.
const (
	pdfCols    = 3
	pdfRows    = 4
	pdfMargin  = 12.0 // mm
	pdfNameH   = 6.0  // mm reserved for the name under each code
	pdfNameGap = 1.0  // mm between a code and its caption
	// pdfBottomMargin is the space kept clear at the foot of the page. fpdf's
	// own page-break margin defaults to about 20mm regardless of SetMargins,
	// and printers reserve a similar strip, so the grid is sized against this
	// rather than pdfMargin: a bottom-row caption that crossed it used to be
	// pushed onto the next page, stranding the following codes a page later.
	pdfBottomMargin = 20.5
	qrPixelSize     = 512 // render resolution per code
)

// Student is one entry on a QR sheet: the name pair to encode, plus the Label
// printed under the code. The caller supplies the label so the sheet matches
// however names are shown elsewhere in the app ("Last, First").
type Student struct {
	First string
	Last  string
	Label string
}

// GeneratePDF writes a printable PDF of QR codes (one per student) to path,
// laid out in a grid with the label under each code.
func GeneratePDF(students []Student, path string) error {
	return GeneratePDFProgress(students, path, nil)
}

// GeneratePDFProgress is like GeneratePDF but reports progress. The optional
// progress callback is invoked as QR images are rendered, with the number done
// and the total. It may be called from multiple goroutines, so it must be safe
// for concurrent use (or nil to skip reporting).
//
// The rendering of each QR image (encode + PNG) is the expensive part and is
// pure CPU, so it is parallelized across all cores; assembling the PDF from the
// finished PNGs is fast and stays serial.
func GeneratePDFProgress(students []Student, path string, progress func(done, total int)) error {
	pngs, err := renderPNGs(students, progress)
	if err != nil {
		return err
	}

	pdf := fpdf.New("P", "mm", "Letter", "")
	pdf.SetMargins(pdfMargin, pdfMargin, pdfMargin)
	// Every element is placed at an absolute position and pages are added by the
	// loop below, so fpdf must not insert its own breaks. Its default bottom
	// margin (about 20mm) is independent of SetMargins, and a bottom-row caption
	// crosses it: fpdf would push that caption to a fresh page and leave the
	// cursor there, so the next code landed on the wrong page and the sheet
	// drifted one page further with every overflow.
	pdf.SetAutoPageBreak(false, 0)

	pageW, pageH := pdf.GetPageSize()
	grid := newGrid(pageW, pageH)

	for i, st := range students {
		if i%grid.perPage == 0 {
			pdf.AddPage()
		}
		slot := grid.slot(i)

		imgName := fmt.Sprintf("qr%d", i)
		pdf.RegisterImageOptionsReader(imgName, fpdf.ImageOptions{ImageType: "PNG"}, bytes.NewReader(pngs[i]))

		pdf.ImageOptions(imgName, slot.QRX, slot.QRY, grid.qrSide, grid.qrSide,
			false, fpdf.ImageOptions{ImageType: "PNG"}, 0, "")

		pdf.SetFont("Helvetica", "", 10)
		pdf.SetXY(slot.CellX, slot.CaptionY)
		pdf.CellFormat(grid.cellW, pdfNameH, tr(pdf, st.Label), "", 0, "C", false, 0, "")
	}

	if pdf.Err() {
		return pdf.Error()
	}
	return pdf.OutputFileAndClose(path)
}

// renderPNGs renders one PNG-encoded QR image per name, in parallel across all
// CPUs, returning them in the original order. progress (if non-nil) is called
// once per completed image with a running count.
// grid is the page geometry for one sheet: where each code and caption goes.
// Split out from the drawing loop so the placement rules can be checked
// directly, without reading back a generated PDF.
type grid struct {
	pageH   float64
	cellW   float64
	cellH   float64
	qrSide  float64
	perPage int
}

// slotPos is one cell's placement, in mm from the page's top-left corner.
type slotPos struct {
	CellX    float64 // left edge of the cell
	QRX, QRY float64 // top-left of the QR image
	CaptionY float64 // top of the caption line
}

func newGrid(pageW, pageH float64) grid {
	cellW := (pageW - 2*pdfMargin) / float64(pdfCols)
	cellH := (pageH - pdfMargin - pdfBottomMargin) / float64(pdfRows)
	return grid{
		pageH:   pageH,
		cellW:   cellW,
		cellH:   cellH,
		qrSide:  minf(cellW, cellH-pdfNameH-pdfNameGap) * 0.9,
		perPage: pdfCols * pdfRows,
	}
}

// slot returns the placement of the i-th student, counting across rows and
// wrapping to a new page every perPage entries.
func (g grid) slot(i int) slotPos {
	n := i % g.perPage
	col := n % pdfCols
	row := n / pdfCols

	cellX := pdfMargin + float64(col)*g.cellW
	cellY := pdfMargin + float64(row)*g.cellH

	return slotPos{
		CellX:    cellX,
		QRX:      cellX + (g.cellW-g.qrSide)/2,
		QRY:      cellY,
		CaptionY: cellY + g.qrSide + pdfNameGap,
	}
}

// bottom is the y of the lowest ink in a cell: the bottom of its caption.
func (g grid) bottom(s slotPos) float64 { return s.CaptionY + pdfNameH }

func renderPNGs(students []Student, progress func(done, total int)) ([][]byte, error) {
	total := len(students)
	out := make([][]byte, total)

	workers := runtime.NumCPU()
	if workers > total {
		workers = total
	}
	if workers < 1 {
		workers = 1
	}

	var (
		next     int64 = -1 // shared work index, advanced atomically
		done     int64      // completed count for progress
		wg       sync.WaitGroup
		mu       sync.Mutex
		firstErr error
	)

	worker := func() {
		defer wg.Done()
		for {
			i := int(atomic.AddInt64(&next, 1))
			if i >= total {
				return
			}
			img, err := Image(students[i].First, students[i].Last, qrPixelSize)
			if err == nil {
				var buf bytes.Buffer
				err = png.Encode(&buf, img)
				if err == nil {
					out[i] = buf.Bytes()
				}
			}
			if err != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = err
				}
				mu.Unlock()
			}
			if progress != nil {
				progress(int(atomic.AddInt64(&done, 1)), total)
			}
		}
	}

	wg.Add(workers)
	for w := 0; w < workers; w++ {
		go worker()
	}
	wg.Wait()

	if firstErr != nil {
		return nil, firstErr
	}
	return out, nil
}

func minf(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

// tr transliterates a UTF-8 string to the PDF's core-font encoding.
func tr(pdf *fpdf.Fpdf, s string) string {
	return pdf.UnicodeTranslatorFromDescriptor("")(s)
}
