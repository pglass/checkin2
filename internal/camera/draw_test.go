package camera

import (
	"image"
	"image/color"
	"testing"
)

// drawBox must flip x to match the mirrored preview: a code on the left of the
// source frame should be outlined on the right of the preview.
func TestDrawBoxMirrorsX(t *testing.T) {
	dst := image.NewRGBA(image.Rect(0, 0, 100, 100))
	// A square in the LEFT half of the source frame.
	pts := []image.Point{{10, 10}, {30, 10}, {30, 30}, {10, 30}}
	drawBox(dst, pts)

	green := func(x, y int) bool {
		r, g, b, _ := dst.At(x, y).RGBA()
		return r>>8 == 0 && g>>8 == 255 && b>>8 == 0
	}

	// Expect the outline on the RIGHT: x=10 maps to 100-1-10 = 89.
	if !green(89, 10) {
		t.Errorf("expected mirrored corner at (89,10)")
	}
	if !green(69, 30) {
		t.Errorf("expected mirrored corner at (69,30)")
	}
	// And nothing back at the unmirrored location.
	if green(10, 10) {
		t.Errorf("outline drawn at unmirrored (10,10)")
	}
}

func TestDrawBoxClipsOutOfBounds(t *testing.T) {
	dst := image.NewRGBA(image.Rect(0, 0, 20, 20))
	// Corners outside the frame must not panic.
	drawBox(dst, []image.Point{{-50, -50}, {500, -10}, {500, 500}, {-5, 300}})
}

func TestDrawBoxIgnoresDegenerate(t *testing.T) {
	dst := image.NewRGBA(image.Rect(0, 0, 20, 20))
	drawBox(dst, nil)
	drawBox(dst, []image.Point{{1, 1}})
	for y := range 20 {
		for x := range 20 {
			if _, _, _, a := dst.At(x, y).RGBA(); a != 0 {
				t.Fatalf("drew something at (%d,%d) for degenerate input", x, y)
			}
		}
	}
}

func TestMirrorRGBA(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 3, 1))
	src.Set(0, 0, color.RGBA{R: 1, A: 255})
	src.Set(1, 0, color.RGBA{R: 2, A: 255})
	src.Set(2, 0, color.RGBA{R: 3, A: 255})

	out := mirrorRGBA(src)
	if got, want := out.Bounds(), image.Rect(0, 0, 3, 1); got != want {
		t.Fatalf("bounds = %v, want %v", got, want)
	}
	for x, want := range []uint8{3, 2, 1} {
		if r, _, _, _ := out.At(x, 0).RGBA(); uint8(r>>8) != want {
			t.Errorf("x=%d: got %d, want %d", x, uint8(r>>8), want)
		}
	}
}

// Frames can arrive with a non-zero origin; the mirror must normalise to (0,0).
func TestMirrorRGBAOffsetBounds(t *testing.T) {
	src := image.NewRGBA(image.Rect(5, 5, 8, 6))
	src.Set(5, 5, color.RGBA{R: 1, A: 255})
	src.Set(6, 5, color.RGBA{R: 2, A: 255})
	src.Set(7, 5, color.RGBA{R: 3, A: 255})

	out := mirrorRGBA(src)
	if got, want := out.Bounds(), image.Rect(0, 0, 3, 1); got != want {
		t.Fatalf("bounds = %v, want %v", got, want)
	}
	for x, want := range []uint8{3, 2, 1} {
		if r, _, _, _ := out.At(x, 0).RGBA(); uint8(r>>8) != want {
			t.Errorf("x=%d: got %d, want %d", x, uint8(r>>8), want)
		}
	}
}
