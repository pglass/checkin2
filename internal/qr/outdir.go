package qr

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// RootDirName is the folder created on the user's Desktop to hold generated QR
// images. Each Center gets its own sub-directory inside it, so codes for one
// Center are never mixed in with another's.
const RootDirName = "Checkin QR Codes"

// RootDir is the Desktop folder QR images are written under. The Desktop is
// where a user will look for something they were just handed, so it is used
// when it exists; on a machine without one (a server, or a locale where the
// folder has another name) the home directory stands in rather than failing.
func RootDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	base := filepath.Join(home, "Desktop")
	if fi, err := os.Stat(base); err != nil || !fi.IsDir() {
		base = home
	}
	return filepath.Join(base, RootDirName), nil
}

// CenterDir is the directory a Center's QR images are written to:
// <Desktop>/Checkin QR Codes/<CenterName>. It is not created here; the
// generator creates it when there is something to put in it.
func CenterDir(centerName string) (string, error) {
	root, err := RootDir()
	if err != nil {
		return "", err
	}
	name := sanitizeDirName(centerName)
	if name == "" {
		return "", errors.New("center name is empty")
	}
	return filepath.Join(root, name), nil
}

// RenameCenterDir moves a Center's QR output directory to follow a Center
// rename, so the images a user already generated stay findable under the name
// they now know the Center by.
//
// It is best-effort by design and reports no error for the ordinary cases:
// the old directory does not exist (nothing was ever generated), or the new
// one already does (the user made it, or generated under that name before).
// Renaming a Center must not fail because of a folder on the Desktop.
func RenameCenterDir(oldName, newName string) error {
	if oldName == newName {
		return nil
	}
	oldDir, err := CenterDir(oldName)
	if err != nil {
		return err
	}
	newDir, err := CenterDir(newName)
	if err != nil {
		return err
	}
	if oldDir == newDir {
		return nil
	}
	if fi, err := os.Stat(oldDir); err != nil || !fi.IsDir() {
		return nil // nothing generated for the old name
	}
	// A case-only rename ("alpha" -> "Alpha") looks like a collision on a
	// case-insensitive filesystem but is really the same directory; os.Rename
	// performs it, so only a genuinely different path is checked.
	if !strings.EqualFold(filepath.Base(oldDir), filepath.Base(newDir)) {
		if _, err := os.Stat(newDir); err == nil {
			return nil // the destination is already in use; leave both alone
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	return os.Rename(oldDir, newDir)
}

// sanitizeDirName strips characters a filesystem objects to from a Center name.
// Center names are already validated against the same set, so this is a
// backstop for names that arrived another way (a directory created by hand, or
// restored from a backup made elsewhere).
func sanitizeDirName(name string) string {
	var b strings.Builder
	for _, r := range strings.TrimSpace(name) {
		switch {
		case r < 0x20, r == 0x7f:
		case strings.ContainsRune(`/\:*?"<>|`, r):
			b.WriteByte('-')
		default:
			b.WriteRune(r)
		}
	}
	return strings.TrimRight(b.String(), ". ")
}
