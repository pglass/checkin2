// Package version exposes the application's build version.
package version

// devVersion is the placeholder for builds that were not stamped with ldflags
// (a bare `go build`, `go test`, or `fyne package`, which compiles the binary
// itself). It is the signal that Version carries nothing authoritative.
const devVersion = "dev"

// Version is the program version. It defaults to devVersion for local/unstamped
// builds and is overridden at build time via
//
//	-ldflags "-X github.com/pglass/checkin/internal/version.Version=x.y.z"
//
// See the Makefile (macOS) and scripts/build-windows.sh (Windows) for the injection.
// The Makefile's VERSION is the single source of truth, so a stamped value
// always wins in Resolve().
//
// On a macOS bundle built with `fyne package --appVersion`, the ldflags value
// is NOT set; instead the version lives in the app's Fyne metadata, and
// Resolve() falls back to that.
var Version = devVersion

// metadataVersion is the version reported by the Fyne app metadata, if any.
// The UI layer sets it once the Fyne app is constructed (see ui.NewFyneApp).
//
// Note this is never empty in a Fyne app: with no bundle Info.plist and no
// FyneApp.toml, Fyne reports its own placeholder ("0.0.1"), which looks like a
// real version but is not ours. That is why a stamped Version takes precedence.
var metadataVersion string

// SetMetadataVersion records the version from the Fyne app metadata. Empty
// values are ignored so the ldflags/default fallback stays in effect.
func SetMetadataVersion(v string) {
	if v != "" {
		metadataVersion = v
	}
}

// Resolve returns the best-known version: the ldflags-stamped Version when the
// build set one, otherwise the Fyne app metadata (populated on packaged macOS
// builds), otherwise the "dev" placeholder.
func Resolve() string {
	if Version != devVersion {
		return Version
	}
	if metadataVersion != "" {
		return metadataVersion
	}
	return Version
}
