package qr

import "testing"

// A student or Center named like a Windows device still gets a usable name.
func TestFileNamesAvoidWindowsReserved(t *testing.T) {
	if got := fileBase(Student{Label: "Con"}); got != "Con_" {
		t.Errorf("fileBase(Con) = %q, want Con_", got)
	}
	if got := sanitizeDirName("NUL"); got != "NUL_" {
		t.Errorf("sanitizeDirName(NUL) = %q, want NUL_", got)
	}
}
