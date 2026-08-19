package version

import "testing"

// reset restores package state after a test mutates the globals.
func reset(t *testing.T) {
	t.Helper()
	origVersion, origMeta := Version, metadataVersion
	t.Cleanup(func() {
		Version, metadataVersion = origVersion, origMeta
	})
}

func TestResolvePrefersStampedVersion(t *testing.T) {
	reset(t)
	// A stamped ldflags version wins over Fyne's metadata, including over
	// Fyne's own "0.0.1" placeholder on unpackaged builds.
	Version = "0.0.2"
	metadataVersion = "0.0.1"
	if got := Resolve(); got != "0.0.2" {
		t.Errorf("Resolve() = %q, want %q", got, "0.0.2")
	}
}

func TestResolveFallsBackToMetadata(t *testing.T) {
	reset(t)
	// Packaged macOS builds are not stamped; the plist metadata is authoritative.
	Version = devVersion
	metadataVersion = "0.0.2"
	if got := Resolve(); got != "0.0.2" {
		t.Errorf("Resolve() = %q, want %q", got, "0.0.2")
	}
}

func TestResolveFallsBackToDev(t *testing.T) {
	reset(t)
	Version = devVersion
	metadataVersion = ""
	if got := Resolve(); got != devVersion {
		t.Errorf("Resolve() = %q, want %q", got, devVersion)
	}
}

func TestSetMetadataVersionIgnoresEmpty(t *testing.T) {
	reset(t)
	metadataVersion = "0.0.2"
	SetMetadataVersion("")
	if metadataVersion != "0.0.2" {
		t.Errorf("metadataVersion = %q, want it unchanged", metadataVersion)
	}
}
