package ui

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"image"
	"log/slog"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"

	"github.com/pglass/checkin/internal/camera"
	"github.com/pglass/checkin/internal/qr"
	"github.com/pglass/checkin/internal/store"
)

// startCamera opens the webcam (if present) in the background, draining preview
// frames into the preview image and routing scan events onto the UI goroutine
// via fyne.Do. Opening a webcam can take seconds (OS init + permission), so it
// must not run on the startup path or it stalls the first paint.
func (a *App) startCamera(ctx context.Context) {
	// Default to the first available camera; with none connected, stay stopped.
	devices := camera.List()
	if len(devices) == 0 {
		slog.Info("no webcam detected; running without camera")
		a.camDevice = deviceNone
		return
	}
	a.startCameraDevice(ctx, devices[0].Index)
}

// deviceNone is the "no camera" selection: capture is stopped and no device is
// held open.
const deviceNone = -1

// stopCamera cancels the running camera (if any) and releases the device. Safe
// to call when nothing is running.
//
// Cancelling only starts the release: Camera.Run closes the device in a defer
// that runs after its loop observes the cancellation, so the driver may still
// be held briefly after this returns. Reopening the same device immediately can
// therefore fail with "driver is already opened" -- see startCameraDevice.
func (a *App) stopCamera() {
	if a.camCancel != nil {
		a.camCancel()
		a.camCancel = nil
	}
	a.cam = nil
	a.camDevice = deviceNone
	// Blank the preview so a stale last frame doesn't look like a live feed.
	if a.preview != nil {
		a.preview.Image = image.NewRGBA(image.Rect(0, 0, 1, 1))
		a.preview.Refresh()
	}
	a.updateResLabel()
}

// startCameraDevice starts capture on the given device index. Calling it again
// stops the previous camera first, so it doubles as the device switcher.
// deviceNone stops capture entirely.
func (a *App) startCameraDevice(parent context.Context, deviceID int) {
	a.stopCamera()
	if deviceID == deviceNone {
		slog.Info("camera stopped")
		return
	}

	ctx, cancel := context.WithCancel(parent)
	a.camCancel = cancel
	a.camDevice = deviceID

	cam := camera.New(a.cfg.CameraFPS, a.cfg.CameraRequestWidth, a.cfg.CameraRequestHeight, a.cfg.QRScanCooldown)
	a.cam = cam
	// The preview window may already be open when switching devices.
	cam.SetPreviewing(a.preview != nil)

	go func() {
		// Run is the sole camera open; a prior Available() probe would open the
		// device twice and block startup, so presence is reported from here.
		err := cam.Run(ctx, deviceID)
		switch {
		case err == nil || ctx.Err() != nil:
			// Clean stop (context cancelled on shutdown).
		default:
			slog.Warn("no webcam detected; running without camera", "error", err)
		}
	}()

	// Preview frames -> canvas image (UI thread).
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case img := <-cam.Frames:
				a.updatePreview(img)
			}
		}
	}()

	// Scan events -> routed to the right dialog (UI thread).
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case ev := <-cam.Scans:
				a.routeScan(ev)
			}
		}
	}()
}

func (a *App) updatePreview(img image.Image) {
	fyne.Do(func() {
		// preview is nil while the camera window is closed; frames are dropped.
		if a.preview == nil {
			return
		}
		// Return the frame we're replacing to the camera's buffer pool. This runs
		// on the UI thread after the swap, so the old frame is no longer rendered.
		old := a.preview.Image
		a.preview.Image = img
		a.preview.Refresh()
		if a.cam != nil {
			a.cam.RecyclePreview(old)
		}
		// Actual resolution is known once frames arrive; keep the status bar current.
		a.updateResLabel()
	})
}

// routeScan parses a scanned payload and opens the check-in/out dialog for a
// known student, or an Add dialog (prefilled, "not found") for an unknown one.
func (a *App) routeScan(ev camera.ScanEvent) {
	p, err := qr.ParsePayload(ev.Payload)
	if err != nil || p.Version != qr.Version || p.Name == "" {
		// Detected a QR code we can't use: log the raw string for diagnosis.
		slog.Debug("QR code detected but not parseable", "raw", ev.Payload)
		return
	}
	slog.Debug("QR code detected", "json", ev.Payload, "name", p.Name)

	st, err := a.store.StudentByName(a.ctx, p.Name)
	fyne.Do(func() {
		// At most one scan-triggered popup at a time: ignore scans that arrive
		// while one is still showing.
		if a.popupOpen {
			slog.Debug("QR code ignored; popup already showing", "name", p.Name)
			return
		}
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				// An unknown code always prompts, confirmation setting or not:
				// there is no student to check in, and silently dropping the
				// scan would look like the camera failed to read the code.
				a.showAddStudentDialogPrefill(p.Name,
					fmt.Sprintf("Scanned %q but student is not found in this center. Add them?", p.Name))
			}
			return
		}
		// Status is read from the store, so the row's In/Out fields are not
		// needed here.
		row := store.StudentRow{ID: st.ID, Name: st.Name}

		// With confirmation off, apply the scan immediately. applyCheckInOut
		// returns false only when the student has already checked in and out
		// today, which has no next action -- fall back to the dialog, which
		// says so, rather than leaving the scan with no visible effect.
		if !a.cfg.ConfirmScan {
			if a.applyCheckInOut(row) {
				slog.Info("scan applied without confirmation", "name", row.Name)
				return
			}
		}
		a.showCheckInOutDialog(row)
	})
}

// newPreview creates the (initially blank) camera preview canvas.
func newPreview() *canvas.Image {
	blank := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img := canvas.NewImageFromImage(blank)
	img.FillMode = canvas.ImageFillContain
	img.SetMinSize(fyne.NewSize(320, 240))
	return img
}
