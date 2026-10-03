package ui

import (
	_ "embed"

	"fyne.io/fyne/v2"
)

// qrCodeIconSVG is the QR glyph shown on the per-row "Get QR Code" button.
// Embedded for the same reason the licenses are: the single static exe must
// carry everything it draws, with no image files shipped alongside it.
//
// Source: Google Material Symbols "qr_code_2" (Apache License 2.0); see
// internal/ui/licenses.txt.
//
//go:embed icons/qr_code_2.svg
var qrCodeIconSVG []byte

// qrCodeIconResource is the embedded glyph as a Fyne resource. Built once,
// because widget.List recycles rows constantly and each one asks for it.
//
// SVG rather than PNG so the glyph is sharp at any scale: a button draws its
// icon at theme.IconInlineSize(), which is a size in points, and Fyne
// rasterizes an SVG at the real device pixel count for that size (see
// canvas.Image.renderSVG). A fixed-size PNG instead gets resampled to whatever
// the display's scale factor asks for -- 2x on a Retina screen -- which left
// the glyph's thin bars visibly fuzzy.
//
// The resource name's .svg extension is what Fyne's format sniffing keys on,
// so it must not be dropped.
var qrCodeIconResource = fyne.NewStaticResource("qr_code_2.svg", qrCodeIconSVG)
