//go:build !darwin && !windows

package volume

// ID is not implemented on this platform yet: every directory is used at its
// saved path.
func ID(dir string) (string, error) { return "", nil }

// Rel reports no drive-relative path; see ID.
func Rel(dir string) (string, bool) { return "", false }

// Resolve returns dir unchanged; see ID.
func Resolve(dir, id string) (string, error) { return dir, nil }
