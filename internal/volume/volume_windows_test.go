package volume

import "testing"

func TestSplitDrive(t *testing.T) {
	for _, tc := range []struct {
		dir, root, rel string
		ok             bool
	}{
		{`E:\checkin-backups`, `E:\`, `checkin-backups`, true},
		{`e:\a\b`, `E:\`, `a\b`, true},
		{`F:\`, `F:\`, ``, true},
		{`\\server\share\backups`, ``, ``, false},
		{`backups`, ``, ``, false},
	} {
		root, rel, ok := splitDrive(tc.dir)
		if root != tc.root || rel != tc.rel || ok != tc.ok {
			t.Errorf("splitDrive(%q) = %q, %q, %v; want %q, %q, %v",
				tc.dir, root, rel, ok, tc.root, tc.rel, tc.ok)
		}
	}
}

func TestFormatSerial(t *testing.T) {
	if got := formatSerial(0x1A2B3C4D); got != "1A2B-3C4D" {
		t.Errorf("formatSerial = %q, want 1A2B-3C4D", got)
	}
	if got := formatSerial(0xBEEF); got != "0000-BEEF" {
		t.Errorf("formatSerial = %q, want 0000-BEEF", got)
	}
}
