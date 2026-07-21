// Package camera captures webcam frames, detects QR codes, and emits scan
// events. It runs a background goroutine and is safe to stop via context.
package camera

import (
	"context"
	"image"
	"image/color"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"gocv.io/x/gocv"
)

// defaultCooldown is how long a given payload is ignored after a scan, so we
// don't re-trigger the same student's dialog immediately.
const defaultCooldown = 8 * time.Second

// defaultFPS paces the capture/detect loop when no rate is configured. QR
// detection is CPU-heavy, so we run it well below the camera's native rate:
// fast enough to catch a held-up QR code, cheap enough to idle quietly.
const defaultFPS = 6

// ScanEvent is emitted when a QR code is decoded (subject to cooldown).
type ScanEvent struct {
	Payload string
	At      time.Time
}

// Resolution is a frame width/height in pixels.
type Resolution struct{ Width, Height int }

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
	// the loop skips the frame->image.Image conversion and channel push (the
	// main per-frame cost besides detection), since nobody is watching.
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
	vc, err := gocv.OpenVideoCapture(deviceID)
	if err != nil {
		return err
	}
	defer vc.Close()
	c.applyResolution(vc)
	slog.Info("webcam detected; camera started", "device", deviceID)

	img := gocv.NewMat()
	defer img.Close()
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
		if ok := vc.Read(&img); !ok || img.Empty() {
			// Transient read failure; brief pause and retry.
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(30 * time.Millisecond):
			}
			continue
		}

		if !logged {
			// Actual delivered size; drivers may snap to their nearest supported
			// resolution and ignore the requested one.
			w, h := img.Cols(), img.Rows()
			c.mu.Lock()
			c.actual = Resolution{Width: w, Height: h}
			c.mu.Unlock()
			slog.Info("camera frame size", "width", w, "height", h)
			logged = true
		}

		c.processFrame(&img, &points, &straight, &detector)

		select {
		case <-ctx.Done():
			return nil
		case <-time.After(c.interval):
		}
	}
}

// applyResolution asks the driver for the requested capture resolution. Cutting
// pixels is the biggest CPU win after fps, since QR detection cost scales with
// pixel count. Drivers snap to their nearest supported size, so the delivered
// frame may differ (logged from the first read in Run).
func (c *Camera) applyResolution(vc *gocv.VideoCapture) {
	if c.req.Width <= 0 || c.req.Height <= 0 {
		return // no specific size requested; leave the driver default
	}
	vc.Set(gocv.VideoCaptureFrameWidth, float64(c.req.Width))
	vc.Set(gocv.VideoCaptureFrameHeight, float64(c.req.Height))
	slog.Info("camera resolution requested", "width", c.req.Width, "height", c.req.Height)
}

// processFrame detects a QR, draws its bounding box on the frame, publishes the
// annotated frame for preview, and emits a ScanEvent if cooldown allows.
func (c *Camera) processFrame(img, points, straight *gocv.Mat, detector *gocv.QRCodeDetector) {
	payload := detector.DetectAndDecode(*img, points, straight)
	previewing := c.previewing.Load()
	if payload != "" && !points.Empty() {
		if previewing {
			drawBox(img, points)
		}
		if c.allow(payload) {
			c.emit(ScanEvent{Payload: payload, At: time.Now()})
		}
	}
	// Frame->image conversion + push is only useful with a viewer. Skipping it
	// when hidden avoids a full-frame alloc/copy every loop.
	if previewing {
		c.publishFrame(img)
	}
}

// drawBox draws the detected QR polygon onto the frame for visual feedback.
func drawBox(img, points *gocv.Mat) {
	green := color.RGBA{0, 255, 0, 0}
	n := points.Cols()
	if points.Rows()*points.Cols() < 4 {
		return
	}
	// points is a 1xN CV_32FC2 matrix of corner coords.
	corner := func(i int) image.Point {
		v := points.GetVecfAt(0, i)
		return image.Pt(int(v[0]), int(v[1]))
	}
	for i := 0; i < n; i++ {
		gocv.Line(img, corner(i), corner((i+1)%n), green, 3)
	}
}

func (c *Camera) publishFrame(img *gocv.Mat) {
	out, err := img.ToImage()
	if err != nil {
		return
	}
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
