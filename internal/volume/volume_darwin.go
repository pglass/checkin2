package volume

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// volumesDir is where macOS mounts every drive other than the system disk.
const volumesDir = "/Volumes"

// diskutilTimeout bounds one diskutil call. It normally takes well under a
// second, but a drive that is failing can stall it.
const diskutilTimeout = 10 * time.Second

// ID returns the ID of the partition holding dir, or "" when dir is not on a
// drive mounted under /Volumes: the system disk is always mounted in the same
// place, so its paths need no ID.
//
// The ID is the partition's UUID where it has one (a GPT drive). Otherwise --
// most USB sticks are MBR-partitioned FAT32, and diskutil reports no partition
// UUID for those -- it is the volume UUID, which macOS derives from the
// filesystem's serial number, so it changes if the drive is reformatted.
//
// dir need not exist, but its drive must be connected.
func ID(dir string) (string, error) {
	mount, _, ok := splitVolume(dir)
	if !ok {
		return "", nil
	}
	info, err := diskutilInfo(mount)
	if err != nil {
		return "", err
	}
	// diskutil also answers for a plain folder at /Volumes/Name left behind on
	// the system disk, reporting the system disk's mount point; that is not
	// the drive dir names.
	if info["MountPoint"] != mount {
		return "", ErrNotMounted
	}
	if id := info["DiskUUID"]; id != "" {
		return id, nil
	}
	if id := info["VolumeUUID"]; id != "" {
		return id, nil
	}
	return "", fmt.Errorf("no partition or volume UUID for %s", mount)
}

// Resolve returns where dir is now: its path below its drive's mount point,
// moved to wherever the partition with the given ID is mounted. dir is returned
// unchanged when id is empty or dir is not on a drive under /Volumes, and
// ErrNotMounted when the partition is not connected -- in which case dir must
// not be used, since another drive may be mounted at its old path.
func Resolve(dir, id string) (string, error) {
	if id == "" {
		return dir, nil
	}
	_, rel, ok := splitVolume(dir)
	if !ok {
		return dir, nil
	}
	info, err := diskutilInfo(id)
	if err != nil || info["MountPoint"] == "" {
		return "", ErrNotMounted
	}
	return filepath.Join(info["MountPoint"], rel), nil
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

// diskutilInfo runs `diskutil info -plist` on device (a mount point or a UUID)
// and returns the string values of its top-level dictionary.
func diskutilInfo(device string) (map[string]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), diskutilTimeout)
	defer cancel()
	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, "diskutil", "info", "-plist", device)
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			err = fmt.Errorf("%w: %s", err, msg)
		}
		return nil, fmt.Errorf("diskutil info %s: %w", device, err)
	}
	return plistStrings(out)
}

// plistStrings returns the <string> values of an XML property list's top-level
// <dict>, by key. Values of any other type (integers, booleans, nested
// containers) are skipped.
func plistStrings(data []byte) (map[string]string, error) {
	var doc struct {
		Dict struct {
			// A dict's children alternate <key> and a value element.
			Items []struct {
				XMLName xml.Name
				Text    string `xml:",chardata"`
			} `xml:",any"`
		} `xml:"dict"`
	}
	if err := xml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parse diskutil plist: %w", err)
	}
	out := map[string]string{}
	var key string
	for _, it := range doc.Dict.Items {
		if it.XMLName.Local == "key" {
			key = it.Text
			continue
		}
		if it.XMLName.Local == "string" {
			out[key] = it.Text
		}
		key = ""
	}
	return out, nil
}
