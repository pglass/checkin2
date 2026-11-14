package volume

import (
	"strings"
	"testing"
)

func TestSplitVolume(t *testing.T) {
	for _, tc := range []struct {
		dir, mount, rel string
		ok              bool
	}{
		{"/Volumes/USB/checkin-backups", "/Volumes/USB", "checkin-backups", true},
		{"/Volumes/USB 1/a/b/", "/Volumes/USB 1", "a/b", true},
		{"/Volumes/USB", "/Volumes/USB", "", true},
		{"/Volumes", "", "", false},
		{"/Users/me/backups", "", "", false},
		{"/VolumesX/a", "", "", false},
	} {
		mount, rel, ok := splitVolume(tc.dir)
		if mount != tc.mount || rel != tc.rel || ok != tc.ok {
			t.Errorf("splitVolume(%q) = %q, %q, %v; want %q, %q, %v",
				tc.dir, mount, rel, ok, tc.mount, tc.rel, tc.ok)
		}
	}
}

// The system disk's volume UUID reads, in diskutil's format, without error.
func TestVolumeUUIDOfRoot(t *testing.T) {
	u, err := volumeUUID("/")
	if err != nil {
		t.Fatal(err)
	}
	if len(u) != 36 || strings.Count(u, "-") != 4 {
		t.Errorf("volumeUUID(/) = %q, want a UUID", u)
	}
}
