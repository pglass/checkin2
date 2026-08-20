package camera

import (
	"testing"
	"time"
)

// A repeated payload is swallowed until the cooldown expires.
func TestAllowCooldown(t *testing.T) {
	c := New(6, 640, 480, time.Minute)

	if !c.allow("payload") {
		t.Fatal("first scan should be allowed")
	}
	if c.allow("payload") {
		t.Error("immediate repeat should be swallowed by the cooldown")
	}
	// A different code is unaffected.
	if !c.allow("other") {
		t.Error("a different payload should be allowed")
	}
}

// ForgetScan clears one payload's cooldown so the next scan is accepted. This
// is what lets a student added from a scan be checked in immediately, rather
// than after waiting out qr_scan_cooldown.
func TestForgetScanResetsCooldown(t *testing.T) {
	c := New(6, 640, 480, time.Minute)

	if !c.allow("payload") {
		t.Fatal("first scan should be allowed")
	}
	if c.allow("payload") {
		t.Fatal("repeat should be swallowed before ForgetScan")
	}

	c.ForgetScan("payload")

	if !c.allow("payload") {
		t.Error("scan after ForgetScan should be allowed")
	}
	// And the cooldown applies again from that new scan.
	if c.allow("payload") {
		t.Error("cooldown should restart after the forgiven scan")
	}
}

// Forgetting an unknown payload is a no-op, not a panic.
func TestForgetScanUnknownPayload(t *testing.T) {
	c := New(6, 640, 480, time.Minute)
	c.ForgetScan("never seen")
	if !c.allow("never seen") {
		t.Error("unrelated payload should still be allowed")
	}
}

// ForgetScan only clears the payload it is given.
func TestForgetScanIsPerPayload(t *testing.T) {
	c := New(6, 640, 480, time.Minute)
	c.allow("a")
	c.allow("b")

	c.ForgetScan("a")

	if !c.allow("a") {
		t.Error("forgotten payload should be allowed")
	}
	if c.allow("b") {
		t.Error("other payloads should keep their cooldown")
	}
}
