package fsname

import "testing"

func TestIsWindowsReserved(t *testing.T) {
	for _, name := range []string{"CON", "con", "Nul", "aux.txt", "NUL .db", "COM1", "lpt9", "COM¹", "CONIN$", "conout$"} {
		if !IsWindowsReserved(name) {
			t.Errorf("IsWindowsReserved(%q) = false, want true", name)
		}
	}
	for _, name := range []string{"Console", "CONX", "COM10", "LPT", "my con", "Alpha", "con-1"} {
		if IsWindowsReserved(name) {
			t.Errorf("IsWindowsReserved(%q) = true, want false", name)
		}
	}
}
