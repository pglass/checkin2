// Package version exposes the application's build version.
package version

// Version is the program version. It defaults to "dev" for local/unstamped
// builds and is overridden at build time via
//
//	-ldflags "-X github.com/pglass/checkin/internal/version.Version=x.y.z"
//
// See the Makefile (macOS) and build-windows.sh (Windows) for the injection.
//
// On a macOS bundle built with `fyne package --appVersion`, the ldflags value
// is NOT set (fyne builds the binary itself); instead the version lives in the
// app's Fyne metadata. Resolve() prefers that metadata when present, so the
// same value shows up everywhere regardless of which build path produced it.
var Version = "dev"

// metadataVersion is the version reported by the Fyne app metadata, if any.
// The UI layer sets it once the Fyne app is constructed (see ui.NewApp); it is
// empty for bare `go build` and non-Fyne callers.
var metadataVersion string

// SetMetadataVersion records the version from the Fyne app metadata. Empty
// values are ignored so the ldflags/default fallback stays in effect.
func SetMetadataVersion(v string) {
	if v != "" {
		metadataVersion = v
	}
}

// Resolve returns the best-known version: the Fyne metadata value if set
// (populated on packaged macOS builds), otherwise the ldflags/default Version.
func Resolve() string {
	if metadataVersion != "" {
		return metadataVersion
	}
	return Version
}
