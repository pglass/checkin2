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
	"time"

	"github.com/pglass/checkin/internal/store"
)

// DBFileName is the database file created inside every Center directory.
const DBFileName = "checkin.db"

// SettingsFileName is the shared settings file at the top of the app directory.
const SettingsFileName = "settings.ini"

// ErrExists is returned by Create and Rename when the Center directory already
// exists.
var ErrExists = errors.New("a Center with that name already exists")

// ErrOpen is returned by Rename and Deactivate when the Center is open in
// another window. Renaming its directory while another process is using the
// database inside would leave that process writing to a path that no longer
// exists under the name it knows.
var ErrOpen = errors.New("this Center is open in another window")

// deactivatedMarker separates a deactivated Center's name from the timestamp of
// when it was deactivated. A Center directory is named either "<Name>" or
// "<Name>_deactivated_<timestamp>", and that name is the whole of the state:
// there is no index or metadata file that could disagree with what is on disk,
// and a Center restored from a backup is deactivated or not according to the
// directory the archive carried.
const deactivatedMarker = "_deactivated_"

// deactivatedTimeFormat is the timestamp in a deactivated Center's directory
// name. Sorts lexicographically, contains nothing a filesystem objects to, and
// matches the format backup archives use.
const deactivatedTimeFormat = "2006-01-02-150405"

// Center is one selectable data set on this machine.
type Center struct {
	// Name is the Center's display name. For an active Center this is also its
	// directory name; for a deactivated one the directory carries a suffix and
	// this is the name it had before, and will have again if reactivated.
	Name string
	// Dir is the absolute path to the Center's directory.
	Dir string
	// DeactivatedAt is when the Center was deactivated, zero for an active one.
	// Deactivation is a soft delete: the data is untouched and the Center is
	// merely hidden, so it can be reactivated later.
	DeactivatedAt time.Time
}

// Deactivated reports whether this Center is deactivated, and so hidden from
// the selection list, excluded from backups, and not openable.
func (c Center) Deactivated() bool { return !c.DeactivatedAt.IsZero() }

// DBPath is the path to this Center's database file.
func (c Center) DBPath() string { return filepath.Join(c.Dir, DBFileName) }

// DirName is the directory name a Center with this name and state has: the
// plain name when active, name plus the deactivation stamp when not.
func DirName(name string, deactivatedAt time.Time) string {
	if deactivatedAt.IsZero() {
		return name
	}
	return name + deactivatedMarker + deactivatedAt.Format(deactivatedTimeFormat)
}

// parseDirName splits a Center directory name into the display name and, for a
// deactivated Center, when it was deactivated.
//
// A directory whose name contains the marker but whose timestamp does not parse
// is treated as an ordinary active Center called exactly that: the name is user
// data, and a Center someone happened to call "Summer_deactivated_camp" must
// not be hidden from them because it looked like a marker.
func parseDirName(dir string) (name string, deactivatedAt time.Time) {
	i := strings.LastIndex(dir, deactivatedMarker)
	if i < 0 {
		return dir, time.Time{}
	}
	stamp := dir[i+len(deactivatedMarker):]
	at, err := time.ParseInLocation(deactivatedTimeFormat, stamp, time.Local)
	if err != nil {
		return dir, time.Time{}
	}
	return dir[:i], at
}

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

// List discovers the active Centers in appDir, sorted by name
// (case-insensitive). Deactivated Centers are left out, which is what keeps
// them off the selection list and out of backups without either caller needing
// to know deactivation exists.
func List(appDir string) ([]Center, error) {
	all, err := ListAll(appDir)
	if err != nil {
		return nil, err
	}
	active := make([]Center, 0, len(all))
	for _, c := range all {
		if !c.Deactivated() {
			active = append(active, c)
		}
	}
	return active, nil
}

// ListAll is List including deactivated Centers, for the selection window's
// "Show Deactivated" view. Sorted the same way, so a deactivated Center sits
// next to an active one of the same name rather than at the end.
func ListAll(appDir string) ([]Center, error) {
	entries, err := os.ReadDir(appDir)
	if err != nil {
		return nil, err
	}

	var centers []Center
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		name, at := parseDirName(e.Name())
		centers = append(centers, Center{
			Name:          name,
			Dir:           filepath.Join(appDir, e.Name()),
			DeactivatedAt: at,
		})
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

// Deactivate hides c by renaming its directory to "<Name>_deactivated_<stamp>",
// returning the deactivated Center.
//
// This is a soft delete: nothing inside the directory is touched, so the data
// survives and Reactivate puts it back. It exists so a Center restored from a
// backup can take over a name that is already in use -- deactivate the old one,
// rename the restored one -- without either being deleted.
//
// at is when the deactivation happened, and becomes part of the directory name.
// It returns ErrOpen if the Center is open in another window, for the same
// reason Rename does.
func Deactivate(appDir, name string, at time.Time) (Center, error) {
	if at.IsZero() {
		return Center{}, errors.New("deactivation time is required")
	}
	oldDir := filepath.Join(appDir, name)
	if _, err := os.Stat(oldDir); err != nil {
		return Center{}, err
	}
	if store.IsOpenElsewhere(filepath.Join(oldDir, DBFileName)) {
		return Center{}, ErrOpen
	}

	newName := DirName(name, at)
	newDir := filepath.Join(appDir, newName)
	// Two deactivations of the same Center within one second would collide.
	// Vanishingly unlikely by hand, but the cost of being wrong is clobbering a
	// Center, so it is refused rather than risked.
	if _, err := os.Stat(newDir); err == nil {
		return Center{}, ErrExists
	} else if !os.IsNotExist(err) {
		return Center{}, err
	}
	if err := os.Rename(oldDir, newDir); err != nil {
		return Center{}, err
	}
	return Center{Name: name, Dir: newDir, DeactivatedAt: at}, nil
}

// Reactivate restores a deactivated Center to its original name, returning the
// reactivated Center.
//
// It returns ErrExists when an active Center already holds that name: the two
// would be one directory, so the user has to rename or deactivate the other
// one first. That is the whole point of the restore-from-backup workflow this
// supports, so it is an ordinary outcome rather than a failure -- the caller is
// expected to explain it, not to report an error.
func Reactivate(appDir string, c Center) (Center, error) {
	if !c.Deactivated() {
		return Center{}, errors.New("this Center is not deactivated")
	}
	if store.IsOpenElsewhere(c.DBPath()) {
		return Center{}, ErrOpen
	}

	newDir := filepath.Join(appDir, c.Name)
	if _, err := os.Stat(newDir); err == nil {
		return Center{}, ErrExists
	} else if !os.IsNotExist(err) {
		return Center{}, err
	}
	if err := os.Rename(c.Dir, newDir); err != nil {
		return Center{}, err
	}
	return Center{Name: c.Name, Dir: newDir}, nil
}
