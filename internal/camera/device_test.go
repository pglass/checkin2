package camera

import (
	"errors"
	"image"
	"testing"

	"github.com/pion/mediadevices/pkg/driver"
	"github.com/pion/mediadevices/pkg/io/video"
	"github.com/pion/mediadevices/pkg/prop"
)

// fakeAdapter is a driver.Adapter whose Close can be made to fail, which is how
// a real driver wrapper gets stuck: driver.State only advances to StateClosed
// when Close returns nil.
type fakeAdapter struct {
	closeErr error
	opens    int
	closes   int
	openErr  error
}

func (f *fakeAdapter) Open() error {
	f.opens++
	return f.openErr
}

func (f *fakeAdapter) Close() error {
	f.closes++
	return f.closeErr
}

func (f *fakeAdapter) Properties() []prop.Media { return []prop.Media{{}} }

// VideoRecord is required for wrapAdapter to accept this as a video driver; the
// tests here only exercise open/close state, so it is never read from.
func (f *fakeAdapter) VideoRecord(prop.Media) (video.Reader, error) {
	return video.ReaderFunc(func() (image.Image, func(), error) {
		return nil, func() {}, errors.New("not implemented")
	}), nil
}

// newFakeDriver wraps f the same way driver.Manager.Register does, so the state
// machine under test is the real one.
func newFakeDriver(t *testing.T, f *fakeAdapter) driver.Driver {
	t.Helper()
	m := driver.GetManager()
	before := len(m.Query(func(driver.Driver) bool { return true }))
	if err := m.Register(f, driver.Info{Label: t.Name()}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	all := m.Query(func(driver.Driver) bool { return true })
	if len(all) != before+1 {
		t.Fatalf("registered driver not found: %d drivers, want %d", len(all), before+1)
	}
	for _, d := range all {
		if d.Info().Label == t.Name() {
			return d
		}
	}
	t.Fatal("registered driver not found by label")
	return nil
}

// A driver left open (the state a failed Close leaves behind) must be closed by
// resetDeviceState so the next Open succeeds instead of returning
// "invalid state: driver is already opened".
func TestResetDeviceStateRecoversStuckDriver(t *testing.T) {
	f := &fakeAdapter{closeErr: errors.New("close failed")}
	d := newFakeDriver(t, f)

	if err := d.Open(); err != nil {
		t.Fatalf("first Open: %v", err)
	}
	// Close fails, so the wrapper stays non-closed -- the stuck state.
	_ = d.Close()
	if d.Status() == driver.StateClosed {
		t.Fatal("driver should be stuck non-closed for this test")
	}
	// Without a reset, reopening is exactly the failure the user hit.
	if err := d.Open(); err == nil {
		t.Fatal("Open on a non-closed driver should fail")
	} else if err.Error() != "invalid state: driver is already opened" {
		t.Fatalf("unexpected error: %v", err)
	}

	// Close now succeeds, so the reset can clear the state.
	f.closeErr = nil
	resetDeviceState(d, 0)

	if d.Status() != driver.StateClosed {
		t.Fatalf("state = %v, want closed after reset", d.Status())
	}
	if err := d.Open(); err != nil {
		t.Fatalf("Open after reset: %v", err)
	}
}

// On the common path the device is already closed and the reset must not touch
// it -- an extra Close would be a needless device call on every start.
func TestResetDeviceStateNoOpWhenClosed(t *testing.T) {
	f := &fakeAdapter{}
	d := newFakeDriver(t, f)

	if d.Status() != driver.StateClosed {
		t.Fatalf("fresh driver state = %v, want closed", d.Status())
	}
	resetDeviceState(d, 0)
	if f.closes != 0 {
		t.Fatalf("Close called %d times on an already-closed driver, want 0", f.closes)
	}
}
