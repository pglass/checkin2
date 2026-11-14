// Package volume identifies the drive a directory is on, so a backup directory
// on a removable drive can be found again after the drive's mount point
// changes. A drive letter on Windows depends on the order drives are plugged
// in, and macOS mounts a second drive with the same name at "/Volumes/Name 1",
// so a saved path alone can name the wrong drive, or none.
//
// A directory is remembered as its path plus the ID of the volume it is on (see
// ID), and Resolve finds where that volume is mounted now.
package volume

import "errors"

// ErrNotMounted is returned by Resolve when the directory's drive is not
// connected.
var ErrNotMounted = errors.New("drive is not connected")

// ErrDuplicateID is returned by Resolve when more than one connected drive has
// the directory's ID -- a cloned drive, or sticks imaged from the same source --
// so there is no telling which one the directory is on.
var ErrDuplicateID = errors.New("more than one connected drive has this drive's ID")
