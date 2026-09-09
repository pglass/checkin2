// Package center manages "Centers": independent data sets, each living in its
// own sub-directory of the application directory. A Center owns its database;
// only one Center is open at a time. Settings (settings.ini) and the log file
// remain at the top level of the app directory and are shared by all Centers.
package center

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/pglass/checkin/internal/store"
)

// DBFileName is the database file created inside every Center directory.
const DBFileName = "checkin.db"

// SettingsFileName is the shared settings file at the top of the app directory.
const SettingsFileName = "settings.ini"

// ErrExists is returned by Create and Rename when the Center directory already
// exists.
var ErrExists = errors.New("a Center with that name already exists")

// ErrOpen is returned by Rename when the Center is open in another window.
// Renaming its directory while another process is using the database inside
// would leave that process writing to a path that no longer exists under the
// name it knows.
var ErrOpen = errors.New("this Center is open in another window")

// Center is one selectable data set on this machine.
type Center struct {
	// Name is the Center's directory name, which is also its display name.
	Name string
	// Dir is the absolute path to the Center's directory.
	Dir string
}

// DBPath is the path to this Center's database file.
func (c Center) DBPath() string { return filepath.Join(c.Dir, DBFileName) }

// AppDir returns the durable per-OS application directory, creating it if
// needed. Centers are sub-directories of this directory.
func AppDir() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	appDir := filepath.Join(dir, "checkin")
	if err := os.MkdirAll(appDir, 0o755); err != nil {
		return "", err
	}
	return appDir, nil
}

// SettingsPath returns the path to the shared settings file in appDir.
func SettingsPath(appDir string) string {
	return filepath.Join(appDir, SettingsFileName)
}

// List discovers the Centers in appDir by listing its sub-directories, sorted
// by name (case-insensitive). Hidden directories (leading ".") are skipped.
func List(appDir string) ([]Center, error) {
	entries, err := os.ReadDir(appDir)
	if err != nil {
		return nil, err
	}

	var centers []Center
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		centers = append(centers, Center{Name: e.Name(), Dir: filepath.Join(appDir, e.Name())})
	}
	sort.Slice(centers, func(i, j int) bool {
		a, b := strings.ToLower(centers[i].Name), strings.ToLower(centers[j].Name)
		if a == b {
			return centers[i].Name < centers[j].Name
		}
		return a < b
	})
	return centers, nil
}

// ValidateName reports whether name is usable as a Center directory name.
// Names must be non-empty, free of path separators and characters that are
// illegal on Windows, and must not be a relative-path special case.
func ValidateName(name string) error {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return errors.New("name cannot be empty")
	}
	if trimmed != name {
		return errors.New("name cannot start or end with spaces")
	}
	if name == "." || name == ".." {
		return fmt.Errorf("%q is not a valid name", name)
	}
	if strings.HasPrefix(name, ".") {
		return errors.New("name cannot start with a period")
	}
	// Reserved on Windows and/or meaningful to the filesystem everywhere.
	if strings.ContainsAny(name, `/\:*?"<>|`) {
		return errors.New(`name cannot contain / \ : * ? " < > |`)
	}
	for _, r := range name {
		if r < 0x20 {
			return errors.New("name cannot contain control characters")
		}
	}
	if strings.HasSuffix(name, ".") {
		return errors.New("name cannot end with a period")
	}
	return nil
}

// Create makes a new Center directory in appDir. It returns ErrExists if a
// directory of that name is already there.
func Create(appDir, name string) (Center, error) {
	if err := ValidateName(name); err != nil {
		return Center{}, err
	}
	dir := filepath.Join(appDir, name)
	if _, err := os.Stat(dir); err == nil {
		return Center{}, ErrExists
	} else if !os.IsNotExist(err) {
		return Center{}, err
	}
	if err := os.Mkdir(dir, 0o755); err != nil {
		return Center{}, err
	}
	return Center{Name: name, Dir: dir}, nil
}

// Rename renames Center oldName to newName within appDir, returning the
// renamed Center.
//
// A Center is just its directory, so this is a directory rename: the database
// inside keeps its fixed file name and is untouched. Renaming to the same name
// is a no-op that succeeds, so a user who opens the dialog and confirms without
// editing is not shown an error.
//
// It returns ErrExists if newName is taken by a different Center, and ErrOpen
// if the Center is open in another window -- renaming out from under a running
// instance would leave it writing into a directory that no longer exists under
// the name it opened.
//
// A rename that differs only in case ("alpha" to "Alpha") is a real rename, not
// a collision, even though the two names are the same path on a
// case-insensitive filesystem; the case-sensitive comparison below lets it
// through to os.Rename, which handles it.
func Rename(appDir, oldName, newName string) (Center, error) {
	if err := ValidateName(newName); err != nil {
		return Center{}, err
	}
	oldDir := filepath.Join(appDir, oldName)
	newDir := filepath.Join(appDir, newName)

	if oldName == newName {
		return Center{Name: newName, Dir: newDir}, nil
	}
	if _, err := os.Stat(oldDir); err != nil {
		return Center{}, err
	}
	// Case-insensitive filesystems report the destination as existing when it
	// is really the source under another spelling; that case is a rename, and
	// os.Rename performs it.
	if !strings.EqualFold(oldName, newName) {
		if _, err := os.Stat(newDir); err == nil {
			return Center{}, ErrExists
		} else if !os.IsNotExist(err) {
			return Center{}, err
		}
	}
	if store.IsOpenElsewhere(filepath.Join(oldDir, DBFileName)) {
		return Center{}, ErrOpen
	}
	if err := os.Rename(oldDir, newDir); err != nil {
		return Center{}, err
	}
	return Center{Name: newName, Dir: newDir}, nil
}
