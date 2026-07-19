package qr

import (
	"bytes"
	"fmt"
	"image/png"

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

		img, err := Image(name, qrPixelSize)
		if err != nil {
			return err
		}
		var buf bytes.Buffer
		if err := png.Encode(&buf, img); err != nil {
			return err
		}
		imgName := fmt.Sprintf("qr%d", i)
		pdf.RegisterImageOptionsReader(imgName, fpdf.ImageOptions{ImageType: "PNG"}, &buf)

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
