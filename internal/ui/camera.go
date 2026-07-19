package ui

import (
	"context"
	"database/sql"
	"errors"
	"image"
	"log/slog"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"

	"github.com/pglass/checkin/internal/camera"
	"github.com/pglass/checkin/internal/qr"
	"github.com/pglass/checkin/internal/store"
)

// startCamera opens the webcam (if present), drains preview frames into the
// preview image, and routes scan events onto the UI goroutine via fyne.Do.
func (a *App) startCamera(ctx context.Context) {
	if !camera.Available(0) {
		slog.Warn("no webcam detected; running without camera")
		return
	}
	slog.Info("webcam detected; starting camera")
	cam := camera.New()
	a.cam = cam

	go func() {
		if err := cam.Run(ctx, 0); err != nil {
			slog.Error("camera stopped", "error", err)
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
	if a.preview == nil {
		return
	}
	fyne.Do(func() {
		a.preview.Image = img
		a.preview.Refresh()
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
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				a.showAddStudentDialogPrefill(p.Name, "Student not found. Add them?")
			}
			return
		}
		// The check-in/out dialog reads current status from the store, so the
		// row's In/Out fields are not needed here.
		a.showCheckInOutDialog(store.StudentRow{ID: st.ID, Name: st.Name})
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
