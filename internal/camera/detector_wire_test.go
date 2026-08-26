package camera

import (
	"testing"

	qrcode "github.com/skip2/go-qrcode"
	"gocv.io/x/gocv"
)

// TestDetectorReportsBox pins the contract the capture loop depends on: a
// decoded payload must arrive with a usable corner quadrangle.
//
// detect() computes `found := payload != "" && !points.Empty()` and gates BOTH
// the preview outline and the ScanEvent on it, so a detector that decodes but
// reports no points silently scans nothing. A trial WeChat detector shipped
// with exactly that bug, and a payload-only test did not catch it.
func TestDetectorReportsBox(t *testing.T) {
	detector := gocv.NewQRCodeDetectorAruco()
	defer detector.Close()

	const payload = "checkin://student/42"
	qr, err := qrcode.New(payload, qrcode.Medium)
	if err != nil {
		t.Fatal(err)
	}
	mat, err := gocv.ImageToMatRGB(qr.Image(512))
	if err != nil {
		t.Fatal(err)
	}
	defer mat.Close()

	points, straight := gocv.NewMat(), gocv.NewMat()
	defer points.Close()
	defer straight.Close()

	got := detector.DetectAndDecode(mat, &points, &straight)
	if got != payload {
		t.Fatalf("DetectAndDecode payload = %q, want %q", got, payload)
	}
	if points.Empty() {
		t.Fatal("decoded the payload but points is empty; detect() would emit " +
			"no ScanEvent (found = payload != \"\" && !points.Empty())")
	}
	if c := corners(&points); len(c) < 4 {
		t.Fatalf("corners() = %v, want 4 points for the preview outline", c)
	}
}
