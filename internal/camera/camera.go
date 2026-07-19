// Package camera captures webcam frames, detects QR codes, and emits scan
// events. It runs a background goroutine and is safe to stop via context.
package camera

import (
	"context"
	"image"
	"image/color"
	"log/slog"
	"sync"
	"time"

	"gocv.io/x/gocv"
)

// defaultCooldown is how long a given payload is ignored after a scan, so we
// don't re-trigger the same student's dialog immediately.
const defaultCooldown = 8 * time.Second

// ScanEvent is emitted when a QR code is decoded (subject to cooldown).
type ScanEvent struct {
	Payload string
	At      time.Time
}

// Camera manages a webcam capture + QR detection loop.
type Camera struct {
	cooldown time.Duration

	mu       sync.Mutex
	lastSeen map[string]time.Time
	frame    image.Image // latest annotated frame for preview

	Frames chan image.Image // latest-frame preview (buffered, size 1)
	Scans  chan ScanEvent   // decoded payloads passing cooldown
}

// New creates a Camera (does not open the device yet).
func New() *Camera {
	return &Camera{
		cooldown: defaultCooldown,
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
	slog.Info("webcam detected; camera started", "device", deviceID)

	img := gocv.NewMat()
	defer img.Close()
	detector := gocv.NewQRCodeDetector()
	defer detector.Close()
	points := gocv.NewMat()
	defer points.Close()
	straight := gocv.NewMat()
	defer straight.Close()

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

		c.processFrame(&img, &points, &straight, &detector)

		select {
		case <-ctx.Done():
			return nil
		case <-time.After(15 * time.Millisecond):
		}
	}
}

// processFrame detects a QR, draws its bounding box on the frame, publishes the
// annotated frame for preview, and emits a ScanEvent if cooldown allows.
func (c *Camera) processFrame(img, points, straight *gocv.Mat, detector *gocv.QRCodeDetector) {
	payload := detector.DetectAndDecode(*img, points, straight)
	if payload != "" && !points.Empty() {
		drawBox(img, points)
		if c.allow(payload) {
			c.emit(ScanEvent{Payload: payload, At: time.Now()})
		}
	}
	c.publishFrame(img)
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
