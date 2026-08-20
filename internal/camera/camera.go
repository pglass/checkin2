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
	"image/draw"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pion/mediadevices/pkg/driver"
	_ "github.com/pion/mediadevices/pkg/driver/camera" // registers camera devices
	"github.com/pion/mediadevices/pkg/io/video"
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

	mu        sync.Mutex
	lastSeen  map[string]time.Time
	actual    Resolution    // actual delivered frame size (0 until first read)
	lastBox   []image.Point // corners of the most recent detection (source coords)
	lastBoxAt time.Time     // when lastBox was recorded; preview draws it while fresh

	// previewing is set by the UI while the camera window is open. When false,
	// the drainer skips the mirrored copy and channel push (the main per-frame
	// cost besides detection), since nobody is watching.
	previewing atomic.Bool

	// bufPool recycles frame buffers so the full-rate drainer and preview do not
	// allocate a new image per frame. Buffers are owned by exactly one goroutine
	// at a time and returned here when done. See getBuf/putBuf.
	bufPool chan *image.RGBA

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
		bufPool:  make(chan *image.RGBA, poolSize),
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
	// Close failing leaves the driver wrapper stuck in a non-closed state, which
	// blocks every later Open, so it is logged rather than silently dropped.
	// openDevice recovers from it, but the log is the only evidence it happened.
	defer func() {
		if cerr := d.Close(); cerr != nil {
			slog.Warn("closing camera device failed", "device", deviceID, "error", cerr)
		}
	}()

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

	// Decoupled capture with two roles:
	//   - drain (below): reads frames as fast as the driver delivers, never
	//     sleeping, so the driver's upstream queue stays empty and every frame is
	//     live. It hands the newest frame to the detector and produces the preview
	//     (throttled to the detection rate).
	//   - this goroutine (detect loop): pulls the newest frame at its own paced
	//     rate and runs QR detection, so heavy detection costs frame rate, not
	//     latency.
	// Buffers move by ownership through channels and the pool; no frame is
	// touched by two goroutines at once.
	detCh := make(chan *image.RGBA, 1) // newest frame awaiting detection (drop-old)
	go c.drain(ctx, reader, detCh)

	for {
		var buf *image.RGBA
		select {
		case <-ctx.Done():
			return nil
		case buf = <-detCh:
		}

		c.detect(buf, &points, &straight, &detector)
		c.putBuf(buf) // done reading; return for reuse

		// Pace detection to cap CPU. The drainer keeps detCh fresh in the
		// meantime, so the next frame we pull is still current.
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(c.interval):
		}
	}
}

// drain reads frames from the driver as fast as they arrive (never sleeping, so
// the driver's upstream queue stays empty and every frame is live). It hands the
// newest frame to the detector, dropping any the detector hasn't consumed, and
// builds the preview at the detection rate — rendering faster than we detect
// would only burn CPU on the mirror and UI upload. Every frame handed on is
// copied into a pooled buffer before the driver buffer is released, so nothing
// retains the driver's reused memory.
func (c *Camera) drain(ctx context.Context, reader video.Reader, detCh chan *image.RGBA) {
	logged := false           // log the actual delivered frame size once
	var lastPreview time.Time // throttles preview production to c.interval
	for {
		if ctx.Err() != nil {
			return
		}

		src, release, err := reader.Read()
		if err != nil {
			// Transient read failure; brief pause and retry.
			select {
			case <-ctx.Done():
				return
			case <-time.After(readRetryDelay):
			}
			continue
		}

		b := src.Bounds()
		if !logged {
			// Actual delivered size; drivers may snap to their nearest supported
			// resolution and ignore the requested one.
			c.mu.Lock()
			c.actual = Resolution{Width: b.Dx(), Height: b.Dy()}
			c.mu.Unlock()
			slog.Info("camera frame size", "width", b.Dx(), "height", b.Dy())
			logged = true
		}

		// Detection copy (pooled), kept full-rate so the detector always pulls the
		// freshest frame. Copy before release: the driver reuses src immediately.
		det := c.getBuf(b.Dx(), b.Dy())
		draw.Draw(det, det.Bounds(), src, b.Min, draw.Src)

		// Preview (pooled), throttled to the detection rate while a window is open.
		if c.previewing.Load() && time.Since(lastPreview) >= c.interval {
			c.publishPreview(c.buildPreview(src))
			lastPreview = time.Now()
		}
		release()

		c.pushDetection(detCh, det)
	}
}

// pushDetection places buf as the newest frame for the detector, dropping and
// recycling any frame the detector hasn't picked up yet.
func (c *Camera) pushDetection(detCh chan *image.RGBA, buf *image.RGBA) {
	select {
	case detCh <- buf:
	default:
		select {
		case old := <-detCh:
			c.putBuf(old)
		default:
		}
		select {
		case detCh <- buf:
		default:
			c.putBuf(buf) // detector took one meanwhile; recycle ours
		}
	}
}

// previewBoxTTL is how long a detected outline keeps being drawn on the preview
// after its last sighting. Detection runs slower than the preview, so the box
// must persist between detections; the TTL clears it soon after a code leaves.
const previewBoxTTL = 300 * time.Millisecond

// buildPreview returns a pooled, mirrored copy of src with the most recent
// detection outline drawn on it while that outline is still fresh.
func (c *Camera) buildPreview(src image.Image) *image.RGBA {
	b := src.Bounds()
	out := c.getBuf(b.Dx(), b.Dy())
	mirrorInto(out, src)

	c.mu.Lock()
	box := c.lastBox
	fresh := !c.lastBoxAt.IsZero() && time.Since(c.lastBoxAt) < previewBoxTTL
	c.mu.Unlock()
	if fresh {
		drawBox(out, box)
	}
	return out
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

	// The manager hands back the same driver wrapper every time, and the
	// wrapper's open/closed state lives in it, not here. Open() only accepts a
	// wrapper in StateClosed, and the wrapper only advances to StateClosed when
	// its Close() returns nil -- so one failed or not-yet-finished Close leaves
	// it stuck in StateRunning and every later Open fails with "invalid state:
	// driver is already opened" until the process restarts. That is what made
	// switching straight from one camera to another (or restarting capture with
	// new settings) fail while going via "None" appeared to work.
	//
	// Closing first is the reset: the transition to StateClosed is always
	// permitted, so this clears a stale state and is a no-op on the common path
	// where the device is already closed.
	resetDeviceState(d, deviceID)

	if err := d.Open(); err != nil {
		return nil, fmt.Errorf("opening device %d: %w", deviceID, err)
	}
	return d, nil
}

// resetDeviceState closes d if it is not already closed, so the following Open
// is made against a wrapper in StateClosed. Closing is always a permitted
// transition, so this is a no-op on the common path and a recovery on the
// stuck one described in openDevice.
func resetDeviceState(d driver.Driver, deviceID int) {
	st := d.Status()
	if st == driver.StateClosed {
		return
	}
	slog.Debug("device not closed; closing before reopen", "device", deviceID, "state", st)
	if err := d.Close(); err != nil {
		slog.Warn("closing stale device failed", "device", deviceID, "error", err)
	}
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

// detect runs QR detection on src (an unmirrored frame). It records the
// detected outline for the preview to draw and emits a ScanEvent for a decoded
// payload that passes the cooldown. The preview is produced separately by the
// drainer, so detection speed does not affect preview smoothness.
func (c *Camera) detect(src image.Image, points, straight *gocv.Mat, detector *gocv.QRCodeDetector) {
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

	// Publish the outline (in source coords) for the preview goroutine to draw.
	c.mu.Lock()
	if found {
		c.lastBox = corners(points)
		c.lastBoxAt = time.Now()
	} else {
		c.lastBoxAt = time.Time{} // clear stale outline once the code is gone
	}
	c.mu.Unlock()

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

// mirrorInto writes img into dst flipped left/right. dst must already be sized
// to img's bounds (getBuf ensures this); it reuses dst rather than allocating.
func mirrorInto(dst *image.RGBA, img image.Image) {
	b := img.Bounds()
	w := b.Dx()
	for y := range b.Dy() {
		for x := range w {
			dst.Set(w-1-x, y, img.At(b.Min.X+x, b.Min.Y+y))
		}
	}
}

// mirrorRGBA copies img into a new RGBA, flipped left/right.
func mirrorRGBA(img image.Image) *image.RGBA {
	b := img.Bounds()
	out := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	mirrorInto(out, img)
	return out
}

// publishPreview delivers buf as the newest preview frame, recycling any frame
// the UI has not yet consumed. Frames the UI does display are returned later via
// RecyclePreview.
func (c *Camera) publishPreview(buf *image.RGBA) {
	select {
	case c.Frames <- buf:
	default:
		select {
		case old := <-c.Frames:
			if o, ok := old.(*image.RGBA); ok {
				c.putBuf(o)
			}
		default:
		}
		select {
		case c.Frames <- buf:
		default:
			c.putBuf(buf) // UI took the slot meanwhile; recycle ours
		}
	}
}

// poolSize bounds the frame buffers kept for reuse: enough for the few in flight
// at once (being filled, queued for detection, queued for preview, held by the
// UI). Buffers beyond this are left to the garbage collector.
const poolSize = 8

// getBuf returns a w×h RGBA, reused from the pool when one of the right size is
// free, otherwise freshly allocated. Buffers of a stale size (after a resolution
// change) are discarded.
func (c *Camera) getBuf(w, h int) *image.RGBA {
	for {
		select {
		case b := <-c.bufPool:
			if b != nil && b.Rect.Dx() == w && b.Rect.Dy() == h {
				return b
			}
			// Wrong size or nil: drop it and try the next / allocate.
		default:
			return image.NewRGBA(image.Rect(0, 0, w, h))
		}
	}
}

// putBuf returns a buffer to the pool, or drops it if the pool is full.
func (c *Camera) putBuf(b *image.RGBA) {
	if b == nil {
		return
	}
	select {
	case c.bufPool <- b:
	default: // pool full; let the GC reclaim it
	}
}

// RecyclePreview returns a preview frame the UI has finished displaying to the
// buffer pool. Call it on the UI thread after the frame has been replaced on
// screen, so the buffer is guaranteed no longer to be rendered.
func (c *Camera) RecyclePreview(img image.Image) {
	if b, ok := img.(*image.RGBA); ok {
		c.putBuf(b)
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
