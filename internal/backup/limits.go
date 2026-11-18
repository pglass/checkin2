package backup

import (
	"fmt"
	"sync"
)

// Unzip limits bound what reading an archive can write or hold in memory. An
// archive is untrusted until verified -- one picked with Browse… could be
// anything -- and its manifest's own sizes are as untrusted as the rest, so
// these fixed caps are what stop a crafted or corrupt archive from filling the
// disk or memory.
const (
	// DefaultMaxManifestMB is the default limit on the manifest, which a real
	// archive keeps to a few kilobytes.
	DefaultMaxManifestMB = 1
	// DefaultMaxEntryMB is the default limit on one Center's snapshot.
	DefaultMaxEntryMB = 1024
)

const bytesPerMB = 1 << 20

type unzipLimits struct{ manifest, entry int64 }

var (
	limitsMu      sync.RWMutex
	currentLimits = unzipLimits{
		manifest: DefaultMaxManifestMB * bytesPerMB,
		entry:    DefaultMaxEntryMB * bytesPerMB,
	}
)

// SetUnzipLimits sets the most an archive's manifest, and each Center's
// snapshot in it, may extract to, in megabytes.
func SetUnzipLimits(manifestMB, entryMB int) {
	limitsMu.Lock()
	defer limitsMu.Unlock()
	currentLimits = unzipLimits{
		manifest: int64(manifestMB) * bytesPerMB,
		entry:    int64(entryMB) * bytesPerMB,
	}
}

// limits returns the limits set by SetUnzipLimits, in bytes.
func limits() unzipLimits {
	limitsMu.RLock()
	defer limitsMu.RUnlock()
	return currentLimits
}

// formatMB renders a byte limit for a message, in megabytes.
func formatMB(n int64) string {
	if n%bytesPerMB == 0 {
		return fmt.Sprintf("%d MB", n/bytesPerMB)
	}
	return fmt.Sprintf("%.1f MB", float64(n)/bytesPerMB)
}
