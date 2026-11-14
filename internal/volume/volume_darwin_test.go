package volume

import (
	"reflect"
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

func TestPlistStrings(t *testing.T) {
	data := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Bootable</key>
	<false/>
	<key>MountPoint</key>
	<string>/Volumes/USB</string>
	<key>Size</key>
	<integer>63864569856</integer>
	<key>Nested</key>
	<array><string>ignored</string></array>
	<key>VolumeUUID</key>
	<string>02F42F02-4309-35D1-A149-0F88047B266D</string>
</dict>
</plist>`)
	got, err := plistStrings(data)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"MountPoint": "/Volumes/USB",
		"VolumeUUID": "02F42F02-4309-35D1-A149-0F88047B266D",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("plistStrings = %q, want %q", got, want)
	}
}
