package camera

import (
	"testing"

	qrcode "github.com/skip2/go-qrcode"
	"gocv.io/x/gocv"
)

// TestQRCodeDetectorDecodes is a smoke test over the real OpenCV link: it
// generates a QR code and runs it through the same gocv detector the capture
// loop uses. Its real job is to guard the trimmed OpenCV build (see
// scripts/build-opencv-static.sh BUILD_LIST and third_party/gocv) -- if a future trim
// drops a module the QRCodeDetectorAruco/decoder needs, decoding breaks here
// rather than silently in the field where nothing would ever scan.
func TestQRCodeDetectorDecodes(t *testing.T) {
	const payload = "checkin://student/42"

	qr, err := qrcode.New(payload, qrcode.Medium)
	if err != nil {
		t.Fatalf("generating QR: %v", err)
	}
	// A generous size with go-qrcode's default quiet-zone border, so the
	// detector has clean edges to lock onto.
	img := qr.Image(512)

	mat, err := gocv.ImageToMatRGB(img)
	if err != nil {
		t.Fatalf("ImageToMatRGB: %v", err)
	}
	defer mat.Close()

	detector := gocv.NewQRCodeDetectorAruco()
	defer detector.Close()
	points := gocv.NewMat()
	defer points.Close()
	straight := gocv.NewMat()
	defer straight.Close()

	got := detector.DetectAndDecode(mat, &points, &straight)
	if got != payload {
		t.Fatalf("DetectAndDecode = %q, want %q (OpenCV QR decode may be broken "+
			"by a module trim)", got, payload)
	}
}
