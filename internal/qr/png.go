package qr

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

// Card layout, in pixels. One QR code per image: a white card with the code
// centered horizontally below a fixed top margin, and the student's name
// centered underneath it.
const (
	CardWidth  = 512
	CardHeight = 640
	qrSide     = 480 // the QR code itself, square
	qrTop      = 60  // margin from the top edge to the top of the code
	nameGap    = 18  // space between the bottom of the code and the name baseline area
	nameSize   = 30  // name font size, in points at 72dpi (so: pixels)
)

// Student is one QR image to generate: the name pair to encode, plus the Label
// drawn under the code. The caller supplies the label so the image matches
// however names are shown elsewhere in the app ("Last, First").
type Student struct {
	First string
	Last  string
	Label string
}

// GenerateImages writes one PNG per student into dir, creating dir if needed,
// and returns the paths written in the order the students were given.
func GenerateImages(students []Student, dir string) ([]string, error) {
	return GenerateImagesProgress(students, dir, nil)
}

// GenerateImagesProgress is like GenerateImages but reports progress. The
// optional callback is invoked as images are finished, with the number done and
// the total; it may be called from multiple goroutines, so it must be safe for
// concurrent use (or nil to skip reporting).
//
// Rendering a card (QR encode + text + PNG) is pure CPU, so the work is spread
// across all cores.
func GenerateImagesProgress(students []Student, dir string, progress func(done, total int)) ([]string, error) {
	if len(students) == 0 {
		return nil, errors.New("no students to generate")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}

	total := len(students)
	paths := make([]string, total)
	// The file is named after the student, so it is recognisable in the folder.
	// Two students can still land on the same name (the roster holds the name
	// twice, or two names sanitize alike); only those get a numeric suffix, so
	// one write never lands on top of another's.
	// Compared case-insensitively, since the filesystem may be too.
	taken := make(map[string]bool, total)
	for i, st := range students {
		base := fileBase(st)
		name := base
		for n := 2; taken[strings.ToLower(name)]; n++ {
			name = fmt.Sprintf("%s-%d", base, n)
		}
		taken[strings.ToLower(name)] = true
		paths[i] = filepath.Join(dir, name+".png")
	}

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
			if err := writeCard(students[i], paths[i]); err != nil {
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
	return paths, nil
}

// writeCard renders one student's card and writes it to path as a PNG. The file
// is written whole via a temporary file and renamed into place, so an
// interrupted run leaves no half-written PNG for the user to open.
func writeCard(st Student, path string) error {
	img, err := Card(st)
	if err != nil {
		return err
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, buf.Bytes(), 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

// Card renders one student's QR card: a white CardWidth x CardHeight image with
// the QR code at the top and the label centered below it.
func Card(st Student) (image.Image, error) {
	code, err := Image(st.First, st.Last, qrSide)
	if err != nil {
		return nil, err
	}

	card := image.NewRGBA(image.Rect(0, 0, CardWidth, CardHeight))
	draw.Draw(card, card.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)

	// go-qrcode sizes its image to the nearest multiple of the module count, so
	// the returned image can be a little larger or smaller than the size asked
	// for; scale it into the exact qrSide box so the layout is fixed.
	qrRect := image.Rect((CardWidth-qrSide)/2, qrTop, (CardWidth-qrSide)/2+qrSide, qrTop+qrSide)
	drawScaled(card, qrRect, code)

	if err := drawLabel(card, st.Label, qrRect.Max.Y+nameGap); err != nil {
		return nil, err
	}
	return card, nil
}

// drawScaled copies src into dst's rect using nearest-neighbour sampling. QR
// modules are solid blocks, so nearest-neighbour keeps their edges crisp --
// smoothing them would only make the code harder for a scanner to read.
func drawScaled(dst *image.RGBA, rect image.Rectangle, src image.Image) {
	sb := src.Bounds()
	dw, dh := rect.Dx(), rect.Dy()
	for y := 0; y < dh; y++ {
		sy := sb.Min.Y + y*sb.Dy()/dh
		for x := 0; x < dw; x++ {
			sx := sb.Min.X + x*sb.Dx()/dw
			dst.Set(rect.Min.X+x, rect.Min.Y+y, src.At(sx, sy))
		}
	}
}

// labelSupersample is how many times larger the name is rendered before being
// scaled back down. The card is only 512px wide, so a 30px caption has little
// room for detail; rasterizing it large and box-filtering it down gives each
// output pixel a proper coverage value instead of the font rasterizer's own
// coarse antialiasing, which is what made the text look soft.
const labelSupersample = 4

// drawLabel draws text centered horizontally, with the top of its line at top.
// A name too wide for the card is shrunk until it fits rather than being cut
// off: the name is what tells staff whose code this is.
//
// The text is rasterized at labelSupersample times its final size into a
// coverage mask and then averaged down, so the glyph edges are smooth. The QR
// code is never touched by this: its edges must stay pixel-exact for scanners.
func drawLabel(dst *image.RGBA, text string, top int) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	const sidePadding = 12
	maxWidth := CardWidth - 2*sidePadding

	face, err := fittedFace(text, maxWidth)
	if err != nil {
		return err
	}
	defer face.Close()

	mask, err := renderLabelMask(text, face)
	if err != nil {
		return err
	}

	// Center the downsampled mask horizontally; its top is the line's top.
	b := mask.Bounds()
	at := image.Pt((CardWidth-b.Dx())/2, top)
	draw.DrawMask(dst, b.Add(at), image.NewUniform(color.Black), image.Point{},
		mask, b.Min, draw.Over)
	return nil
}

// fittedFace returns the largest face at or below nameSize whose rendering of
// text fits within maxWidth pixels. The face is built at supersampled size,
// since that is what the mask is rasterized with.
func fittedFace(text string, maxWidth int) (font.Face, error) {
	limit := fixed.I(maxWidth * labelSupersample)
	for size := nameSize; ; size -= 2 {
		face, err := labelFace(size * labelSupersample)
		if err != nil {
			return nil, err
		}
		if size <= 8 || font.MeasureString(face, text) <= limit {
			return face, nil
		}
		face.Close()
	}
}

// renderLabelMask rasterizes text with face (a supersampled face) and averages
// the result down by labelSupersample, returning an alpha mask sized in final
// card pixels.
func renderLabelMask(text string, face font.Face) (*image.Alpha, error) {
	m := face.Metrics()
	w := font.MeasureString(face, text).Ceil()
	h := (m.Ascent + m.Descent).Ceil()
	if w <= 0 || h <= 0 {
		return image.NewAlpha(image.Rectangle{}), nil
	}

	big := image.NewAlpha(image.Rect(0, 0, w, h))
	d := &font.Drawer{
		Dst:  big,
		Src:  image.NewUniform(color.Alpha{A: 0xff}),
		Face: face,
		Dot:  fixed.Point26_6{X: 0, Y: m.Ascent},
	}
	d.DrawString(text)

	return downsampleAlpha(big, labelSupersample), nil
}

// labelGamma darkens the downsampled coverage. Averaging n x n samples is
// linear in area, which renders stems about a pixel-edge lighter than the
// hinted rasterizer does at this size; a gamma below 1 pushes partial coverage
// up and restores the font's intended weight. 0.8 was picked by comparing
// rendered captions -- lower starts to bloom the glyph edges outward.
const labelGamma = 0.8

// downsampleAlpha box-filters an alpha image down by an integer factor: each
// output pixel is the mean coverage of the n x n source pixels behind it,
// adjusted by labelGamma.
func downsampleAlpha(src *image.Alpha, n int) *image.Alpha {
	b := src.Bounds()
	out := image.NewAlpha(image.Rect(0, 0, (b.Dx()+n-1)/n, (b.Dy()+n-1)/n))
	for y := out.Bounds().Min.Y; y < out.Bounds().Max.Y; y++ {
		for x := out.Bounds().Min.X; x < out.Bounds().Max.X; x++ {
			var sum int
			for dy := 0; dy < n; dy++ {
				for dx := 0; dx < n; dx++ {
					sx, sy := b.Min.X+x*n+dx, b.Min.Y+y*n+dy
					if sx < b.Max.X && sy < b.Max.Y {
						sum += int(src.AlphaAt(sx, sy).A)
					}
				}
			}
			mean := float64(sum) / float64(n*n*0xff)
			out.SetAlpha(x, y, color.Alpha{A: uint8(math.Round(0xff * math.Pow(mean, labelGamma)))})
		}
	}
	return out
}

// labelFace builds a font face for the name caption at the given pixel size.
// The Go regular font is embedded in the binary, so the same image is produced
// on every machine regardless of what fonts are installed.
func labelFace(size int) (font.Face, error) {
	f, err := opentype.Parse(goregular.TTF)
	if err != nil {
		return nil, err
	}
	return opentype.NewFace(f, &opentype.FaceOptions{
		Size: float64(size),
		DPI:  72, // points == pixels
		// Full hinting snaps stems to the (supersampled) pixel grid, which
		// survives the downsample as uneven stem weights. Vertical hinting
		// keeps the baseline crisp without distorting the horizontal shapes
		// the box filter is about to average.
		Hinting: font.HintingVertical,
	})
}

// fileBase is the file-name stem for a student's card: the label with anything
// a filesystem objects to replaced, so a name with a slash or a colon in it
// still produces a writable path.
func fileBase(st Student) string {
	name := strings.TrimSpace(st.Label)
	if name == "" {
		name = strings.TrimSpace(st.Last + " " + st.First)
	}
	if name == "" {
		name = "student"
	}
	var b strings.Builder
	for _, r := range name {
		switch {
		case r < 0x20, r == 0x7f:
			// control characters: drop
		case strings.ContainsRune(`/\:*?"<>|`, r):
			b.WriteByte('-')
		default:
			b.WriteRune(r)
		}
	}
	// Trailing dots and spaces are not addressable on Windows.
	out := strings.TrimRight(b.String(), ". ")
	if out == "" {
		return "student"
	}
	return out
}
