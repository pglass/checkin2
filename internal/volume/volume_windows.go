package volume

import (
	"fmt"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
)

// ID returns the volume serial number of the drive holding dir, as "1A2B-3C4D"
// (the form `vol` prints), or "" when dir needs none: a path on the Windows
// system drive, which never changes letter, or one that is not on a lettered
// local drive (a network share, for example). The serial is stored in the
// filesystem itself, so every computer reads the same one until the drive is
// reformatted.
//
// dir need not exist, but its drive must be connected.
func ID(dir string) (string, error) {
	root, _, ok := splitDrive(dir)
	if !ok || strings.EqualFold(root, systemDriveRoot()) {
		return "", nil
	}
	if !idDriveType(root) {
		return "", ErrNotMounted
	}
	return volumeSerial(root)
}

// Resolve returns where dir is now: its path below its drive's root, moved to
// whichever drive letter the volume with the given ID has now. dir is returned
// unchanged when id is empty or dir is not on a lettered drive. It returns
// ErrNotMounted when no connected drive has the ID, and ErrDuplicateID when
// several do; dir must not be used then, since another drive may have its old
// letter.
func Resolve(dir, id string) (string, error) {
	if id == "" {
		return dir, nil
	}
	_, rel, ok := splitDrive(dir)
	if !ok {
		return dir, nil
	}
	mask, err := windows.GetLogicalDrives()
	if err != nil {
		return "", fmt.Errorf("list drives: %w", err)
	}
	var found []string
	for i := 0; i < 26; i++ {
		if mask&(1<<i) == 0 {
			continue
		}
		root := string(rune('A'+i)) + `:\`
		if !idDriveType(root) {
			continue
		}
		if s, err := volumeSerial(root); err == nil && s == id {
			found = append(found, root)
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

// idDriveType reports whether the drive at root is one a backup directory is
// identified on: removable or fixed. Network, optical, and RAM drives are not
// looked at, so an unreachable network drive cannot stall Resolve.
func idDriveType(root string) bool {
	p, err := windows.UTF16PtrFromString(root)
	if err != nil {
		return false
	}
	switch windows.GetDriveType(p) {
	case windows.DRIVE_REMOVABLE, windows.DRIVE_FIXED:
		return true
	}
	return false
}

// volumeSerial returns the serial number of the volume at root ("E:\") as
// "1A2B-3C4D". A serial of 0 -- a filesystem without one -- is an error, so
// such drives get no ID rather than all sharing one.
func volumeSerial(root string) (string, error) {
	p, err := windows.UTF16PtrFromString(root)
	if err != nil {
		return "", err
	}
	// An empty card-reader slot would otherwise show a "no disk" dialog.
	old := windows.SetErrorMode(windows.SEM_FAILCRITICALERRORS)
	defer windows.SetErrorMode(old)
	var serial uint32
	if err := windows.GetVolumeInformation(p, nil, 0, &serial, nil, nil, nil, 0); err != nil {
		return "", fmt.Errorf("read volume serial of %s: %w", root, err)
	}
	if serial == 0 {
		return "", fmt.Errorf("%s has no volume serial", root)
	}
	return formatSerial(serial), nil
}

// formatSerial formats a volume serial number as `vol` does: "1A2B-3C4D".
func formatSerial(serial uint32) string {
	return fmt.Sprintf("%04X-%04X", serial>>16, serial&0xFFFF)
}

// Rel returns dir's path below its drive's root, which is what stays the same
// when the drive gets another letter. ok is false for a path without a drive
// letter.
func Rel(dir string) (rel string, ok bool) {
	_, rel, ok = splitDrive(dir)
	return rel, ok
}

// splitDrive splits a path on a lettered drive into the drive's root and the
// rest: `E:\checkin-backups\x` is `E:\` and `checkin-backups\x`. ok is false
// for a path without a drive letter, such as a network share.
func splitDrive(dir string) (root, rel string, ok bool) {
	vol := filepath.VolumeName(dir)
	if len(vol) != 2 || vol[1] != ':' {
		return "", "", false
	}
	root = strings.ToUpper(vol) + `\`
	rel = strings.TrimLeft(dir[len(vol):], `\/`)
	return root, rel, true
}

// systemDriveRoot returns the root of the drive Windows is installed on, such
// as `C:\`, or "" if it cannot be found.
func systemDriveRoot() string {
	win, err := windows.GetWindowsDirectory()
	if err != nil {
		return ""
	}
	root, _, _ := splitDrive(win)
	return root
}
