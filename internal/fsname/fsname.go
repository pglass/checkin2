// Package fsname holds file-name rules shared by everything that turns a user's
// text into a file or directory name.
package fsname

import "strings"

// windowsReserved are the device names Windows will not use as a file or
// directory name, in any letter case. The list is Microsoft's ("Naming Files,
// Paths, and Namespaces"), including the superscript-digit forms, plus CONIN$
// and CONOUT$. Go has no exported equivalent: filepath.IsLocal applies these
// rules only when built for Windows.
var windowsReserved = map[string]bool{
	"CON": true, "PRN": true, "AUX": true, "NUL": true,
	"CONIN$": true, "CONOUT$": true,
}

func init() {
	for _, p := range []string{"COM", "LPT"} {
		for _, d := range []string{"0", "1", "2", "3", "4", "5", "6", "7", "8", "9", "¹", "²", "³"} {
			windowsReserved[p+d] = true
		}
	}
}

// IsWindowsReserved reports whether name is a Windows device name, which
// Windows refuses as a file or directory name. The check is the one Windows
// applies: case-insensitive, and ignoring any extension and any trailing
// spaces or dots before it, so "con", "CON.txt" and "NUL .db" all count.
// Checked on every platform, so a name accepted on a Mac still works when the
// data is restored on Windows.
func IsWindowsReserved(name string) bool {
	base, _, _ := strings.Cut(name, ".")
	base = strings.TrimRight(base, " ")
	return windowsReserved[strings.ToUpper(base)]
}
