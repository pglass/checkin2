package camera

import (
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	qrcode "github.com/skip2/go-qrcode"
	"gocv.io/x/gocv"
)

// -qrdump writes each synthetic frame to a directory as PNG, so the inputs can
// be eyeballed rather than trusted. The frames are the whole basis of the
// comparison below, so being able to look at them matters.
//
//	go test ./internal/camera/ -run TestQRDumpFrames -qrdump ./qrframes
var qrDump = flag.String("qrdump", "", "directory to write synthetic QR benchmark frames to as PNG")

// Exercises cv::QRCodeDetectorAruco -- the detector the capture loop uses --
// over synthetic webcam-like frames: perspective, rotation, scale, lighting,
// and frames with no code at all. See the package doc in camera.go for why
// this detector was chosen over the alternatives.
//
// Two things are reported, and both matter. BenchmarkQRDetect gives per-call
// latency (CPU cost on the kiosk); TestQRDetectHitRate gives detection rate
// (whether it works at all). Latency alone is misleading: a detector that
// bails out early is fast and useless, and a miss is much cheaper than a hit.
// Read them together.

const benchPayload = "checkin://student/42"

// detector is the small slice of the gocv type the harness needs. Kept as an
// interface so another detector can be dropped in for a comparison run.
type detector interface {
	DetectAndDecode(input gocv.Mat, points *gocv.Mat, straight *gocv.Mat) string
	Close() error
}

func newAruco() detector { d := gocv.NewQRCodeDetectorAruco(); return &d }

var detectors = []struct {
	name string
	new  func() detector
}{
	{"Aruco", newAruco},
}

// --- frame synthesis --------------------------------------------------------
//
// Each case renders the QR into a larger frame the way a webcam would see it:
// the code occupies part of the field, with background, and is then degraded.
// Codes are generated at high resolution and scaled down, so scale cases test
// real resolution loss rather than nearest-neighbour blockiness.

type frameCase struct {
	name  string
	build func() (image.Image, error)
}

// baseQR renders the payload at the given pixel size.
func baseQR(size int) (image.Image, error) {
	qr, err := qrcode.New(benchPayload, qrcode.Medium)
	if err != nil {
		return nil, err
	}
	return qr.Image(size), nil
}

// place composites src into a frameW x frameH gray background, centred, after
// applying the affine transform t (in destination space, inverse-mapped).
// Sampling is bilinear so rotation and scaling produce realistic soft edges
// rather than aliasing artefacts that would flatter or punish a detector
// unfairly.
func place(src image.Image, frameW, frameH int, bg color.Gray, t func(x, y float64) (float64, float64)) image.Image {
	dst := image.NewRGBA(image.Rect(0, 0, frameW, frameH))
	draw.Draw(dst, dst.Bounds(), &image.Uniform{bg}, image.Point{}, draw.Src)
	sb := src.Bounds()
	for y := 0; y < frameH; y++ {
		for x := 0; x < frameW; x++ {
			sx, sy := t(float64(x), float64(y))
			sx += float64(sb.Dx()) / 2
			sy += float64(sb.Dy()) / 2
			if sx < 0 || sy < 0 || sx >= float64(sb.Dx()-1) || sy >= float64(sb.Dy()-1) {
				continue
			}
			dst.Set(x, y, bilinear(src, sx, sy))
		}
	}
	return dst
}

func bilinear(src image.Image, x, y float64) color.Color {
	x0, y0 := int(x), int(y)
	fx, fy := x-float64(x0), y-float64(y0)
	at := func(ix, iy int) float64 {
		r, _, _, _ := src.At(src.Bounds().Min.X+ix, src.Bounds().Min.Y+iy).RGBA()
		return float64(r >> 8)
	}
	v := at(x0, y0)*(1-fx)*(1-fy) + at(x0+1, y0)*fx*(1-fy) +
		at(x0, y0+1)*(1-fx)*fy + at(x0+1, y0+1)*fx*fy
	g := uint8(math.Round(v))
	return color.RGBA{g, g, g, 255}
}

// centered maps destination pixels to source coordinates about the frame centre.
func centered(frameW, frameH int, inv func(dx, dy float64) (float64, float64)) func(x, y float64) (float64, float64) {
	cx, cy := float64(frameW)/2, float64(frameH)/2
	return func(x, y float64) (float64, float64) {
		return inv(x-cx, y-cy)
	}
}

const frameW, frameH = 640, 480

func frameCases() []frameCase {
	return []frameCase{
		{"Ideal", func() (image.Image, error) {
			// Upright, ~300px of a 640x480 frame: the easy baseline.
			src, err := baseQR(600)
			if err != nil {
				return nil, err
			}
			return place(src, frameW, frameH, color.Gray{200}, centered(frameW, frameH,
				func(dx, dy float64) (float64, float64) { return dx * 2, dy * 2 })), nil
		}},
		{"Rotated30", func() (image.Image, error) {
			src, err := baseQR(600)
			if err != nil {
				return nil, err
			}
			const a = 30 * math.Pi / 180
			s, c := math.Sin(a), math.Cos(a)
			return place(src, frameW, frameH, color.Gray{200}, centered(frameW, frameH,
				func(dx, dy float64) (float64, float64) {
					// inverse rotation, then the same 2x downscale as Ideal
					return (dx*c + dy*s) * 2, (-dx*s + dy*c) * 2
				})), nil
		}},
		{"Perspective", func() (image.Image, error) {
			src, err := baseQR(600)
			if err != nil {
				return nil, err
			}
			// Tilt about the vertical axis: horizontal scale varies with y's
			// counterpart, approximating a badge held at an angle.
			return place(src, frameW, frameH, color.Gray{200}, centered(frameW, frameH,
				func(dx, dy float64) (float64, float64) {
					const k = 0.0016 // perspective strength
					w := 1 + k*dx
					return dx * 2 / w, dy * 2 / w
				})), nil
		}},
		{"Small", func() (image.Image, error) {
			// ~110px in the frame: a code held well back from the camera.
			src, err := baseQR(600)
			if err != nil {
				return nil, err
			}
			return place(src, frameW, frameH, color.Gray{200}, centered(frameW, frameH,
				func(dx, dy float64) (float64, float64) { return dx * 5.5, dy * 5.5 })), nil
		}},
		{"LowContrast", func() (image.Image, error) {
			src, err := baseQR(600)
			if err != nil {
				return nil, err
			}
			img := place(src, frameW, frameH, color.Gray{200}, centered(frameW, frameH,
				func(dx, dy float64) (float64, float64) { return dx * 2, dy * 2 }))
			return mapGray(img, func(v float64) float64 { return 90 + v*0.35 }), nil
		}},
		{"UnevenLight", func() (image.Image, error) {
			src, err := baseQR(600)
			if err != nil {
				return nil, err
			}
			img := place(src, frameW, frameH, color.Gray{200}, centered(frameW, frameH,
				func(dx, dy float64) (float64, float64) { return dx * 2, dy * 2 }))
			// Horizontal brightness ramp plus a bright glare lobe: the classic
			// failure mode for a single global threshold.
			b := img.Bounds()
			out := image.NewRGBA(b)
			for y := b.Min.Y; y < b.Max.Y; y++ {
				for x := b.Min.X; x < b.Max.X; x++ {
					r, _, _, _ := img.At(x, y).RGBA()
					v := float64(r >> 8)
					ramp := 0.55 + 0.9*float64(x)/float64(b.Dx())
					gx := float64(x-b.Dx()*3/4) / 90
					gy := float64(y-b.Dy()/3) / 90
					v = v*ramp + 70*math.Exp(-(gx*gx+gy*gy))
					g := uint8(clamp(v, 0, 255))
					out.Set(x, y, color.RGBA{g, g, g, 255})
				}
			}
			return out, nil
		}},

		// --- no-code frames -------------------------------------------------
		//
		// The kiosk sees these most of the time: it runs detection on every
		// paced frame whether or not anyone is holding a badge up. So the
		// no-code path, not the hit path, dominates steady-state CPU. Both
		// cases must decode to "" -- a non-empty decode here is a false
		// positive, which in the app would fire a spurious ScanEvent.
		{"EmptyScene", func() (image.Image, error) {
			// A plain room-ish gradient with soft blobs: nothing remotely like
			// a finder pattern. This is the floor -- how cheaply each detector
			// rejects an obviously empty frame.
			out := image.NewRGBA(image.Rect(0, 0, frameW, frameH))
			for y := 0; y < frameH; y++ {
				for x := 0; x < frameW; x++ {
					v := 120 + 40*math.Sin(float64(x)/180) + 25*math.Cos(float64(y)/120)
					g := uint8(clamp(v, 0, 255))
					out.Set(x, y, color.RGBA{g, g, g, 255})
				}
			}
			return out, nil
		}},
		{"NoCodeClutter", func() (image.Image, error) {
			// The harder and more honest no-code case: high-contrast black
			// rectangles, including concentric squares that mimic the 1:1:3:1:1
			// finder-pattern ratio. This is what a detector must do work to
			// reject, so it is closer to a real worst case than an empty frame
			// -- a noticeboard, window frames, or dark clothing on a light wall.
			out := image.NewRGBA(image.Rect(0, 0, frameW, frameH))
			draw.Draw(out, out.Bounds(), &image.Uniform{color.Gray{205}}, image.Point{}, draw.Src)
			black := &image.Uniform{color.Gray{20}}
			fill := func(x0, y0, x1, y1 int, c *image.Uniform) {
				draw.Draw(out, image.Rect(x0, y0, x1, y1), c, image.Point{}, draw.Src)
			}
			white := &image.Uniform{color.Gray{235}}
			// Three concentric-square decoys at different scales: finder-pattern
			// shaped, but with no timing pattern between them and no fourth
			// corner, so a correct detector rejects them.
			decoy := func(cx, cy, r int) {
				fill(cx-r, cy-r, cx+r, cy+r, black)
				fill(cx-r*5/7, cy-r*5/7, cx+r*5/7, cy+r*5/7, white)
				fill(cx-r*3/7, cy-r*3/7, cx+r*3/7, cy+r*3/7, black)
			}
			decoy(120, 110, 42)
			decoy(520, 130, 30)
			decoy(300, 380, 55)
			// Plus assorted hard-edged clutter.
			fill(200, 60, 420, 90, black)
			fill(60, 250, 110, 430, black)
			fill(430, 300, 600, 340, black)
			fill(240, 180, 280, 220, black)
			return out, nil
		}},
	}
}

// noCodeCases are the frames that contain no QR code, so a correct detector
// returns "". Kept as a set so the hit-rate test can invert its expectation
// rather than reporting every no-code frame as a MISS.
var noCodeCases = map[string]bool{"EmptyScene": true, "NoCodeClutter": true}

// judge scores one decode against what the case expects: the payload for a
// frame containing a code, the empty string for one that does not. It returns
// whether the detector was correct and a word for the log.
func judge(caseName, got string) (bool, string) {
	if noCodeCases[caseName] {
		if got == "" {
			return true, "rejected" // correct: nothing there to find
		}
		return false, "FALSE-POS"
	}
	if got == benchPayload {
		return true, "hit"
	}
	return false, "MISS"
}

func mapGray(img image.Image, f func(float64) float64) image.Image {
	b := img.Bounds()
	out := image.NewRGBA(b)
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			r, _, _, _ := img.At(x, y).RGBA()
			g := uint8(clamp(f(float64(r>>8)), 0, 255))
			out.Set(x, y, color.RGBA{g, g, g, 255})
		}
	}
	return out
}

func clamp(v, lo, hi float64) float64 { return math.Max(lo, math.Min(hi, v)) }

// --- frame dump -------------------------------------------------------------

// TestQRDumpFrames writes the synthetic frames to disk for visual inspection.
// It is a no-op unless -qrdump names a directory, so it stays out of the way
// of normal test runs.
//
//	go test ./internal/camera/ -run TestQRDumpFrames -qrdump ./qrframes
//
// Alongside each frame it writes a marked-up copy showing what each detector
// found: the located quadrangle drawn over the image, green for a correct
// decode. That turns "Plain misses Perspective" into something you can see.
func TestQRDumpFrames(t *testing.T) {
	if *qrDump == "" {
		t.Skip("set -qrdump <dir> to write frames")
	}
	if err := os.MkdirAll(*qrDump, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	for _, fc := range frameCases() {
		img, err := fc.build()
		if err != nil {
			t.Fatalf("%s: building frame: %v", fc.name, err)
		}
		writePNG(t, filepath.Join(*qrDump, fc.name+".png"), img)

		for _, d := range detectors {
			mat, err := gocv.ImageToMatRGB(img)
			if err != nil {
				t.Fatalf("ImageToMatRGB: %v", err)
			}
			det := d.new()
			points, straight := gocv.NewMat(), gocv.NewMat()

			got := det.DetectAndDecode(mat, &points, &straight)
			ok, word := judge(fc.name, got)

			// corners() is the same helper the capture loop uses to read the
			// points Mat, so the overlay reflects what production would see.
			var quad []image.Point
			if !points.Empty() {
				quad = corners(&points)
			}
			marked := annotate(img, quad, ok)
			name := fmt.Sprintf("%s.%s.%s.png", fc.name, d.name, strings.ToLower(word))
			writePNG(t, filepath.Join(*qrDump, name), marked)

			points.Close()
			straight.Close()
			det.Close()
			mat.Close()
		}
	}
	t.Logf("wrote frames to %s", *qrDump)
}

func writePNG(t *testing.T, path string, img image.Image) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create %s: %v", path, err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatalf("encode %s: %v", path, err)
	}
}

// annotate draws the detected quadrangle over a copy of img: green if the
// decode was correct, red otherwise. An empty quad means nothing was located,
// which is itself the interesting result.
func annotate(img image.Image, quad []image.Point, ok bool) image.Image {
	b := img.Bounds()
	out := image.NewRGBA(b)
	draw.Draw(out, b, img, b.Min, draw.Src)

	mark := color.RGBA{220, 40, 40, 255}
	if ok {
		mark = color.RGBA{40, 200, 60, 255}
	}
	for i := 0; i < len(quad); i++ {
		drawLine(out, quad[i], quad[(i+1)%len(quad)], mark)
	}
	return out
}

// --- hit rate ---------------------------------------------------------------

// TestQRDetectHitRate reports which cases each detector decodes correctly.
// This is the number that decides whether a detector is usable; the benchmark
// below only says how fast it reaches its answer.
func TestQRDetectHitRate(t *testing.T) {
	for _, d := range detectors {
		for _, fc := range frameCases() {
			img, err := fc.build()
			if err != nil {
				t.Fatalf("%s/%s: building frame: %v", d.name, fc.name, err)
			}
			mat, err := gocv.ImageToMatRGB(img)
			if err != nil {
				t.Fatalf("ImageToMatRGB: %v", err)
			}
			det := d.new()
			points, straight := gocv.NewMat(), gocv.NewMat()
			got := det.DetectAndDecode(mat, &points, &straight)
			ok, status := judge(fc.name, got)
			t.Logf("%-6s %-14s %s (decoded %q)", d.name, fc.name, status, got)
			if !ok && noCodeCases[fc.name] {
				t.Errorf("%s: false positive on a frame with no QR code: decoded %q",
					fc.name, got)
			}
			points.Close()
			straight.Close()
			det.Close()
			mat.Close()
		}
	}
}

// --- latency ----------------------------------------------------------------

// BenchmarkQRDetect measures per-call detect+decode latency. A detector is
// reconstructed per sub-benchmark but reused across iterations, matching the
// capture loop, which builds one detector and reuses it (internal/camera/camera.go).
//
// Mats are built once outside the timed loop: ImageToMatRGB is conversion cost
// the real loop pays too, but including it here would mask the difference
// being measured.
func BenchmarkQRDetect(b *testing.B) {
	for _, d := range detectors {
		for _, fc := range frameCases() {
			b.Run(fmt.Sprintf("%s/%s", d.name, fc.name), func(b *testing.B) {
				img, err := fc.build()
				if err != nil {
					b.Fatalf("building frame: %v", err)
				}
				mat, err := gocv.ImageToMatRGB(img)
				if err != nil {
					b.Fatalf("ImageToMatRGB: %v", err)
				}
				defer mat.Close()

				det := d.new()
				defer det.Close()
				points, straight := gocv.NewMat(), gocv.NewMat()
				defer points.Close()
				defer straight.Close()

				// Warm up, and record whether this case produced the correct
				// answer, so a fast time on a wrong answer is not read as a
				// fast detection. For no-code frames "correct" means an empty
				// decode, so ok=1 there means a clean rejection, not a find.
				correct, _ := judge(fc.name, det.DetectAndDecode(mat, &points, &straight))

				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					det.DetectAndDecode(mat, &points, &straight)
				}
				b.StopTimer()

				if correct {
					b.ReportMetric(1, "ok")
				} else {
					b.ReportMetric(0, "ok")
				}
			})
		}
	}
}
