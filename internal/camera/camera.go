// Package camera captures webcam frames, detects QR codes, and emits scan
// events. It runs a background goroutine and is safe to stop via context.
//
// Capture comes from pion/mediadevices, which enumerates devices by name and
// hands back frames as image.Image. Detection uses OpenCV (via gocv): its QR
// detector corrects perspective from the symbol contour, so codes held at an
// angle still decode, which the pure-Go decoders do not manage.
package camera

import (
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pion/mediadevices/pkg/driver"
	_ "github.com/pion/mediadevices/pkg/driver/camera" // registers camera devices
	"github.com/pion/mediadevices/pkg/prop"
	"gocv.io/x/gocv"
)

// defaultCooldown is how long a given payload is ignored after a scan, so we
// don't re-trigger the same student's dialog immediately.
const defaultCooldown = 8 * time.Second

// defaultFPS paces the capture/detect loop when no rate is configured. QR
// detection is CPU-heavy, so we run it well below the camera's native rate:
// fast enough to catch a held-up QR code, cheap enough to idle quietly.
const defaultFPS = 6

// readRetryDelay is the pause after a failed frame read before trying again.
const readRetryDelay = 30 * time.Millisecond

// Appearance of the outline drawn around a detected QR code in the preview.
var (
	boxColor     = color.RGBA{R: 0, G: 255, B: 0, A: 255}
	boxThickness = 3
)

// ScanEvent is emitted when a QR code is decoded (subject to cooldown).
type ScanEvent struct {
	Payload string
	At      time.Time
}

// Resolution is a frame width/height in pixels.
type Resolution struct{ Width, Height int }

// Device is a video capture device as reported by the OS.
type Device struct {
	Index int    // position in the list; what Run takes as deviceID
	Name  string // human-readable, e.g. "MacBook Pro Camera"
	Label string // stable unique ID, survives replug
}

// List enumerates the connected video capture devices.
func List() []Device {
	drivers := driver.GetManager().Query(driver.FilterVideoRecorder())
	out := make([]Device, 0, len(drivers))
	for i, d := range drivers {
		info := d.Info()
		name := info.Name
		if name == "" {
			name = fmt.Sprintf("Camera %d", i)
		}
		out = append(out, Device{Index: i, Name: name, Label: info.Label})
	}
	return out
}

// Camera manages a webcam capture + QR detection loop.
type Camera struct {
	cooldown time.Duration
	interval time.Duration // time between loop iterations (derived from fps)
	req      Resolution    // capture resolution requested from the driver

	mu       sync.Mutex
	lastSeen map[string]time.Time
	frame    image.Image // latest annotated frame for preview
	actual   Resolution  // actual delivered frame size (0 until first read)

	// previewing is set by the UI while the camera window is open. When false,
	// the loop skips the mirrored copy and channel push (the main per-frame cost
	// besides detection), since nobody is watching.
	previewing atomic.Bool

	Frames chan image.Image // latest-frame preview (buffered, size 1)
	Scans  chan ScanEvent   // decoded payloads passing cooldown
}

// SetPreviewing tells the camera whether a preview window is showing frames.
// While false, annotated frames are not produced, cutting idle CPU.
func (c *Camera) SetPreviewing(on bool) { c.previewing.Store(on) }

// Requested returns the capture resolution asked of the driver.
func (c *Camera) Requested() Resolution { return c.req }

// Actual returns the delivered frame size, or a zero Resolution before the
// first frame has been read.
func (c *Camera) Actual() Resolution {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.actual
}

// New creates a Camera running the loop at fps frames per second, requesting the
// given capture resolution, and ignoring a repeated scan for cooldown. Does not
// open the device yet. fps <= 0 falls back to defaultFPS; cooldown <= 0 falls
// back to defaultCooldown; a non-positive width or height requests no specific
// size (the driver's default).
func New(fps, reqWidth, reqHeight int, cooldown time.Duration) *Camera {
	if fps <= 0 {
		fps = defaultFPS
	}
	if cooldown <= 0 {
		cooldown = defaultCooldown
	}
	return &Camera{
		cooldown: cooldown,
		interval: time.Second / time.Duration(fps),
		req:      Resolution{Width: reqWidth, Height: reqHeight},
		lastSeen: map[string]time.Time{},
		Frames:   make(chan image.Image, 1),
		Scans:    make(chan ScanEvent, 8),
	}
}

// Run opens the device and loops until ctx is cancelled. It returns an error
// only if the camera cannot be opened; a missing camera is not fatal to the app.
func (c *Camera) Run(ctx context.Context, deviceID int) error {
	d, err := openDevice(deviceID)
	if err != nil {
		return err
	}
	defer d.Close()

	p, err := c.chooseFormat(d)
	if err != nil {
		return err
	}
	reader, err := d.(driver.VideoRecorder).VideoRecord(p)
	if err != nil {
		return err
	}
	slog.Info("webcam detected; camera started",
		"device", deviceID, "name", d.Info().Name, "label", d.Info().Label,
		"width", p.Width, "height", p.Height, "format", p.FrameFormat)

	detector := gocv.NewQRCodeDetector()
	defer detector.Close()
	points := gocv.NewMat()
	defer points.Close()
	straight := gocv.NewMat()
	defer straight.Close()

	logged := false // log the actual delivered frame size once
	for {
		if ctx.Err() != nil {
			return nil
		}

		src, release, err := reader.Read()
		if err != nil {
			// Transient read failure; brief pause and retry.
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(readRetryDelay):
			}
			continue
		}

		if !logged {
			// Actual delivered size; drivers may snap to their nearest supported
			// resolution and ignore the requested one.
			b := src.Bounds()
			c.mu.Lock()
			c.actual = Resolution{Width: b.Dx(), Height: b.Dy()}
			c.mu.Unlock()
			slog.Info("camera frame size", "width", b.Dx(), "height", b.Dy())
			logged = true
		}

		c.processFrame(src, &points, &straight, &detector)
		// src's buffer belongs to the driver and is reused after release, so
		// nothing may retain it past this point. processFrame copies what the
		// preview needs.
		release()

		select {
		case <-ctx.Done():
			return nil
		case <-time.After(c.interval):
		}
	}
}

// openDevice resolves deviceID against the enumerated capture devices and opens
// it. deviceID indexes the same list List returns.
func openDevice(deviceID int) (driver.Driver, error) {
	drivers := driver.GetManager().Query(driver.FilterVideoRecorder())
	if len(drivers) == 0 {
		return nil, errors.New("no video capture device found")
	}
	if deviceID < 0 || deviceID >= len(drivers) {
		return nil, fmt.Errorf("device %d out of range (%d connected)", deviceID, len(drivers))
	}
	d := drivers[deviceID]
	if err := d.Open(); err != nil {
		return nil, fmt.Errorf("opening device %d: %w", deviceID, err)
	}
	return d, nil
}

// chooseFormat picks one of the device's own advertised capture modes,
// preferring the configured resolution. The mode must come from the device:
// requesting a pixel format it does not produce (asking I420 of an NV12 camera,
// say) makes mediadevices misread the chroma plane, which shows up as smeared
// colour and a sheared frame rather than as an error.
func (c *Camera) chooseFormat(d driver.Driver) (prop.Media, error) {
	props := d.Properties()
	if len(props) == 0 {
		return prop.Media{}, errors.New("device advertises no capture properties")
	}
	if c.req.Width > 0 && c.req.Height > 0 {
		for _, p := range props {
			if p.Width == c.req.Width && p.Height == c.req.Height {
				return p, nil
			}
		}
		slog.Info("requested capture size unavailable; using device default",
			"width", c.req.Width, "height", c.req.Height)
	}
	return props[0], nil
}

// processFrame detects a QR code in src and, when the preview is open,
// publishes a mirrored copy with the detected outline drawn on it. A decoded
// payload is emitted as a ScanEvent if it passes the cooldown.
//
// src is only valid for the duration of the call; anything retained is copied.
func (c *Camera) processFrame(src image.Image, points, straight *gocv.Mat, detector *gocv.QRCodeDetector) {
	// OpenCV wants a Mat. The generic conversion path writes BGR, which is what
	// the detector expects, and detection runs on the unmirrored frame so the
	// coordinates it reports match the source.
	mat, err := gocv.ImageToMatRGB(src)
	if err != nil {
		return
	}
	defer mat.Close()

	payload := detector.DetectAndDecode(mat, points, straight)
	found := payload != "" && !points.Empty()

	if c.previewing.Load() {
		// A preview that tracks the viewer's own movement is what people expect
		// from a webcam, so the copy is mirrored left/right. The outline is
		// drawn after mirroring, with its x coordinates flipped to match.
		out := mirrorRGBA(src)
		if found {
			drawBox(out, corners(points))
		}
		c.publishImage(out)
	}

	if found && c.allow(payload) {
		c.emit(ScanEvent{Payload: payload, At: time.Now()})
	}
}

// corners reads the detected QR polygon out of OpenCV's point matrix, which is
// a 1xN CV_32FC2 of corner coordinates in the source frame.
func corners(points *gocv.Mat) []image.Point {
	if points.Empty() || points.Rows()*points.Cols() < 4 {
		return nil
	}
	out := make([]image.Point, 0, points.Cols())
	for i := range points.Cols() {
		v := points.GetVecfAt(0, i)
		out = append(out, image.Pt(int(v[0]), int(v[1])))
	}
	return out
}

// drawBox outlines the detected QR polygon on the mirrored preview frame. The
// corners are in source coordinates, so each x is flipped to match the mirror.
func drawBox(dst *image.RGBA, pts []image.Point) {
	if len(pts) < 2 {
		return
	}
	w := dst.Bounds().Dx()
	flip := func(p image.Point) image.Point { return image.Pt(w-1-p.X, p.Y) }
	for i, p := range pts {
		drawLine(dst, flip(p), flip(pts[(i+1)%len(pts)]), boxColor)
	}
}

// drawLine plots a line with Bresenham's algorithm, thickened to boxThickness
// so the outline stays visible when the preview is scaled down.
func drawLine(dst *image.RGBA, a, b image.Point, col color.RGBA) {
	dx := abs(b.X - a.X)
	dy := -abs(b.Y - a.Y)
	sx, sy := step(a.X, b.X), step(a.Y, b.Y)
	err := dx + dy

	for {
		plot(dst, a.X, a.Y, col)
		if a == b {
			return
		}
		e2 := 2 * err
		if e2 >= dy {
			err += dy
			a.X += sx
		}
		if e2 <= dx {
			err += dx
			a.Y += sy
		}
	}
}

// plot paints a boxThickness-square centred on (x, y), clipped to dst.
func plot(dst *image.RGBA, x, y int, col color.RGBA) {
	r := boxThickness / 2
	for oy := -r; oy <= r; oy++ {
		for ox := -r; ox <= r; ox++ {
			px, py := x+ox, y+oy
			if (image.Point{X: px, Y: py}).In(dst.Bounds()) {
				dst.SetRGBA(px, py, col)
			}
		}
	}
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func step(from, to int) int {
	if from < to {
		return 1
	}
	return -1
}

// mirrorRGBA copies img into a new RGBA, flipped left/right.
func mirrorRGBA(img image.Image) *image.RGBA {
	b := img.Bounds()
	out := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	for y := range b.Dy() {
		for x := range b.Dx() {
			out.Set(b.Dx()-1-x, y, img.At(b.Min.X+x, b.Min.Y+y))
		}
	}
	return out
}

func (c *Camera) publishImage(out image.Image) {
	c.mu.Lock()
	c.frame = out
	c.mu.Unlock()
	// Non-blocking latest-frame delivery.
	select {
	case c.Frames <- out:
	default:
		select {
		case <-c.Frames:
		default:
		}
		select {
		case c.Frames <- out:
		default:
		}
	}
}

// LatestFrame returns the most recent annotated frame, or nil if none yet.
// Used to paint the preview immediately when its window opens, rather than
// waiting for the next frame to arrive on the channel.
func (c *Camera) LatestFrame() image.Image {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.frame
}

func (c *Camera) emit(e ScanEvent) {
	select {
	case c.Scans <- e:
	default:
	}
}

// allow reports whether payload is outside its cooldown window, recording the
// time if so.
func (c *Camera) allow(payload string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	if last, ok := c.lastSeen[payload]; ok && now.Sub(last) < c.cooldown {
		return false
	}
	c.lastSeen[payload] = now
	return true
}
