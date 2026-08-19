package store

import (
	"errors"
	"path/filepath"
	"testing"
)

// TestOpen_SecondInstanceRejected verifies the one-process-per-Center guard: a
// second Open of the same database while the first is still open returns
// ErrAlreadyOpen, and reopening succeeds once the first is closed.
func TestOpen_SecondInstanceRejected(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")

	first, err := Open(path)
	if err != nil {
		t.Fatalf("first Open: %v", err)
	}

	if _, err := Open(path); !errors.Is(err, ErrAlreadyOpen) {
		if err == nil {
			t.Fatal("second Open succeeded; want ErrAlreadyOpen")
		}
		t.Fatalf("second Open err = %v, want ErrAlreadyOpen", err)
	}

	// Releasing the lock lets a later instance open the same Center.
	if err := first.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	second, err := Open(path)
	if err != nil {
		t.Fatalf("reopen after close: %v", err)
	}
	second.Close()
}

// TestOpen_DifferentCentersIndependent verifies the lock is per-Center: two
// different databases can be open at the same time.
func TestOpen_DifferentCentersIndependent(t *testing.T) {
	dir := t.TempDir()

	a, err := Open(filepath.Join(dir, "a.db"))
	if err != nil {
		t.Fatalf("open a: %v", err)
	}
	defer a.Close()

	b, err := Open(filepath.Join(dir, "b.db"))
	if err != nil {
		t.Fatalf("open b (different Center): %v", err)
	}
	defer b.Close()
}
