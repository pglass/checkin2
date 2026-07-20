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
	pdfCols     = 3
	pdfRows     = 4
	pdfMargin   = 12.0 // mm
	pdfNameH    = 6.0  // mm reserved for the name under each code
	qrPixelSize = 512  // render resolution per code
)

// GeneratePDF writes a printable PDF of QR codes (one per name) to path,
// laid out in a grid with the name under each code.
func GeneratePDF(names []string, path string) error {
	return GeneratePDFProgress(names, path, nil)
}

// GeneratePDFProgress is like GeneratePDF but reports progress. The optional
// progress callback is invoked as QR images are rendered, with the number done
// and the total. It may be called from multiple goroutines, so it must be safe
// for concurrent use (or nil to skip reporting).
//
// The rendering of each QR image (encode + PNG) is the expensive part and is
// pure CPU, so it is parallelized across all cores; assembling the PDF from the
// finished PNGs is fast and stays serial.
func GeneratePDFProgress(names []string, path string, progress func(done, total int)) error {
	pngs, err := renderPNGs(names, progress)
	if err != nil {
		return err
	}

	pdf := fpdf.New("P", "mm", "Letter", "")
	pdf.SetMargins(pdfMargin, pdfMargin, pdfMargin)

	pageW, pageH := pdf.GetPageSize()
	usableW := pageW - 2*pdfMargin
	usableH := pageH - 2*pdfMargin
	cellW := usableW / float64(pdfCols)
	cellH := usableH / float64(pdfRows)
	qrSide := minf(cellW, cellH-pdfNameH) * 0.9

	perPage := pdfCols * pdfRows
	for i, name := range names {
		if i%perPage == 0 {
			pdf.AddPage()
		}
		slot := i % perPage
		col := slot % pdfCols
		row := slot / pdfCols

		cellX := pdfMargin + float64(col)*cellW
		cellY := pdfMargin + float64(row)*cellH

		imgName := fmt.Sprintf("qr%d", i)
		pdf.RegisterImageOptionsReader(imgName, fpdf.ImageOptions{ImageType: "PNG"}, bytes.NewReader(pngs[i]))

		qrX := cellX + (cellW-qrSide)/2
		qrY := cellY
		pdf.ImageOptions(imgName, qrX, qrY, qrSide, qrSide, false, fpdf.ImageOptions{ImageType: "PNG"}, 0, "")

		pdf.SetFont("Helvetica", "", 10)
		pdf.SetXY(cellX, qrY+qrSide+1)
		pdf.CellFormat(cellW, pdfNameH, tr(pdf, name), "", 0, "C", false, 0, "")
	}

	if pdf.Err() {
		return pdf.Error()
	}
	return pdf.OutputFileAndClose(path)
}

// renderPNGs renders one PNG-encoded QR image per name, in parallel across all
// CPUs, returning them in the original order. progress (if non-nil) is called
// once per completed image with a running count.
func renderPNGs(names []string, progress func(done, total int)) ([][]byte, error) {
	total := len(names)
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
			img, err := Image(names[i], qrPixelSize)
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
