//go:build !darwin

package volume

// ID is not implemented on this platform yet: every directory is used at its
// saved path.
func ID(dir string) (string, error) { return "", nil }

// Resolve returns dir unchanged; see ID.
func Resolve(dir, id string) (string, error) { return dir, nil }
