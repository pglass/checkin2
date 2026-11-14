package volume

import (
	"encoding/binary"
	"fmt"
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/unix"
)

// volumesDir is where macOS mounts every drive other than the system disk.
const volumesDir = "/Volumes"

// ID returns the volume UUID of the drive holding dir, or "" when dir is not on
// a drive mounted under /Volumes: the system disk is always mounted in the same
// place, so its paths need no ID. The volume UUID is the one diskutil reports;
// for a FAT drive macOS derives it from the filesystem's serial number, so it
// changes if the drive is reformatted.
//
// dir need not exist, but its drive must be connected.
func ID(dir string) (string, error) {
	mount, _, ok := splitVolume(dir)
	if !ok {
		return "", nil
	}
	// statfs reports the filesystem a path is on, so a plain folder left at
	// /Volumes/Name on the system disk, or the /Volumes/Macintosh HD link to
	// it, is not taken for a drive.
	var st unix.Statfs_t
	if err := unix.Statfs(mount, &st); err != nil {
		return "", ErrNotMounted
	}
	if unix.ByteSliceToString(st.Mntonname[:]) != mount {
		return "", ErrNotMounted
	}
	return volumeUUID(mount)
}

// Resolve returns where dir is now: its path below its drive's mount point,
// moved to wherever the drive with the given ID is mounted. dir is returned
// unchanged when id is empty or dir is not on a drive under /Volumes. It
// returns ErrNotMounted when no connected drive has the ID, and ErrDuplicateID
// when several do; dir must not be used then, since another drive may be
// mounted at its old path.
func Resolve(dir, id string) (string, error) {
	if id == "" {
		return dir, nil
	}
	_, rel, ok := splitVolume(dir)
	if !ok {
		return dir, nil
	}
	mounts, err := volumeMounts()
	if err != nil {
		return "", err
	}
	var found []string
	for _, m := range mounts {
		if u, err := volumeUUID(m); err == nil && u == id {
			found = append(found, m)
		}
	}
	switch len(found) {
	case 0:
		return "", ErrNotMounted
	case 1:
		return filepath.Join(found[0], rel), nil
	}
	return "", ErrDuplicateID
}

// volumeMounts returns the mount points of the mounted drives under /Volumes.
func volumeMounts() ([]string, error) {
	n, err := unix.Getfsstat(nil, unix.MNT_NOWAIT)
	if err != nil {
		return nil, fmt.Errorf("list mounted drives: %w", err)
	}
	// Room for a drive mounted between the two calls; one beyond that is
	// simply not seen this time.
	buf := make([]unix.Statfs_t, n+4)
	n, err = unix.Getfsstat(buf, unix.MNT_NOWAIT)
	if err != nil {
		return nil, fmt.Errorf("list mounted drives: %w", err)
	}
	var out []string
	for _, st := range buf[:n] {
		if m := unix.ByteSliceToString(st.Mntonname[:]); strings.HasPrefix(m, volumesDir+"/") {
			out = append(out, m)
		}
	}
	return out, nil
}

// volumeUUID returns the volume UUID of the filesystem mounted at mount, read
// with getattrlist(2). x/sys/unix has no wrapper for getattrlist, so it is
// called by number.
func volumeUUID(mount string) (string, error) {
	al := unix.Attrlist{
		Bitmapcount: unix.ATTR_BIT_MAP_COUNT,
		Commonattr:  unix.ATTR_CMN_RETURNED_ATTRS,
		Volattr:     unix.ATTR_VOL_INFO | unix.ATTR_VOL_UUID,
	}
	// The reply: its length (uint32), the attributes actually returned (five
	// uint32 bitmaps: common, volume, dir, file, fork), then the UUID.
	const (
		volattrOff = 4 + 4
		uuidOff    = 4 + 5*4
	)
	var buf [uuidOff + 16]byte
	p, err := unix.BytePtrFromString(mount)
	if err != nil {
		return "", err
	}
	_, _, errno := unix.Syscall6(unix.SYS_GETATTRLIST,
		uintptr(unsafe.Pointer(p)), uintptr(unsafe.Pointer(&al)),
		uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)), 0, 0)
	if errno != 0 {
		return "", fmt.Errorf("read volume UUID of %s: %w", mount, errno)
	}
	// A filesystem without a UUID leaves the attribute out of the reply.
	if binary.LittleEndian.Uint32(buf[volattrOff:])&unix.ATTR_VOL_UUID == 0 {
		return "", fmt.Errorf("%s has no volume UUID", mount)
	}
	u := buf[uuidOff:]
	return fmt.Sprintf("%X-%X-%X-%X-%X", u[0:4], u[4:6], u[6:8], u[8:10], u[10:16]), nil
}

// Rel returns dir's path below its drive's mount point, which is what stays the
// same when the drive is mounted somewhere else. ok is false for a path that
// is not on a drive under /Volumes.
func Rel(dir string) (rel string, ok bool) {
	_, rel, ok = splitVolume(dir)
	return rel, ok
}

// splitVolume splits a path under /Volumes into its drive's mount point and
// the rest: "/Volumes/USB/backups" is "/Volumes/USB" and "backups". ok is false
// for a path that is not under /Volumes.
func splitVolume(dir string) (mount, rel string, ok bool) {
	dir = filepath.Clean(dir)
	after, found := strings.CutPrefix(dir, volumesDir+"/")
	if !found || after == "" {
		return "", "", false
	}
	name, rel, _ := strings.Cut(after, "/")
	return filepath.Join(volumesDir, name), rel, true
}
