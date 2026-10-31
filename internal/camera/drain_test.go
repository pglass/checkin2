package camera

import (
	"context"
	"image"
	"image/color"
	"testing"
	"time"

	"github.com/pion/mediadevices/pkg/io/video"
)

// The drainer reads every frame but copies only the first one delivered after
// each request, so the detector gets a current frame and the rest cost no copy.
func TestDrainCopiesOnlyRequestedFrames(t *testing.T) {
	c := New(6, 640, 480, time.Minute)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	feed := make(chan image.Image)
	reader := video.ReaderFunc(func() (image.Image, func(), error) {
		select {
		case img := <-feed:
			return img, func() {}, nil
		case <-ctx.Done():
			return nil, func() {}, ctx.Err()
		}
	})
	detCh := make(chan *image.RGBA, 1)
	go c.drain(ctx, reader, detCh)

	// Each send returns once the drainer is back in Read, so sending a frame
	// also means the one before it has been fully handled.
	send := func(shade uint8) {
		t.Helper()
		img := image.NewRGBA(image.Rect(0, 0, 2, 2))
		img.SetRGBA(0, 0, color.RGBA{R: shade, A: 255})
		select {
		case feed <- img:
		case <-time.After(5 * time.Second):
			t.Fatal("drainer stopped reading frames")
		}
	}

	send(1)
	send(2)
	if len(detCh) != 0 {
		t.Fatal("frame handed on without a request")
	}

	c.wantFrame.Store(true)
	send(3)
	send(4)
	send(5)
	if len(detCh) != 1 {
		t.Fatalf("detCh holds %d frames after one request, want 1", len(detCh))
	}
	if got := (<-detCh).RGBAAt(0, 0).R; got != 3 {
		t.Errorf("detector got frame %d, want the first after the request (3)", got)
	}
}
