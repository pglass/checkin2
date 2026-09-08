package backup

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/pglass/checkin/internal/center"
)

// restoreNameTimeFormat is the timestamp appended to a restored Center's name.
// It has no colons and no path separators, so the result is a valid Center name
// on every platform (see center.ValidateName), and it sorts chronologically
// next to its siblings in the selection list.
const restoreNameTimeFormat = "2006-01-02-1504"

// RestoreName is the name a Center from this backup is restored under:
// the original name with the backup's timestamp appended.
//
// Restoring always creates a new Center rather than overwriting an existing
// one, so the name has to differ from the original. Deriving it from the
// backup's own timestamp -- rather than "copy 2" or the current time -- means
// the restored Center says which backup it came from.
func RestoreName(centerName string, at time.Time) string {
	return centerName + "-" + at.Format(restoreNameTimeFormat)
}

// Restore extracts one Center's snapshot from an archive into a new Center in
// appDir, named by RestoreName, and returns the Center it created.
//
// It never overwrites: if a Center of that name already exists -- a second
// restore of the same backup -- it fails with center.ErrExists rather than
// replacing whatever is there. The caller decides what to tell the user.
//
// The snapshot is extracted to a temporary file and checked before it is put in
// place, so a failed restore leaves no half-made Center behind: the Center
// directory is created only once there is a database to put in it, and is
// removed again if anything after that fails.
func Restore(ctx context.Context, archivePath, centerName, appDir string) (center.Center, error) {
	man, err := ReadManifest(archivePath)
	if err != nil {
		return center.Center{}, err
	}

	var info CenterInfo
	var found bool
	for _, ci := range man.Centers {
		if ci.Name == centerName {
			info, found = ci, true
			break
		}
	}
	if !found {
		return center.Center{}, fmt.Errorf("this backup does not contain a Center named %q", centerName)
	}

	name := RestoreName(info.Name, man.CreatedAt)
	if err := center.ValidateName(name); err != nil {
		return center.Center{}, fmt.Errorf("cannot restore as %q: %w", name, err)
	}

	// Extract and check before creating anything in appDir, so a bad archive
	// does not leave an empty Center behind.
	tmpDir, err := os.MkdirTemp("", "checkin-restore-*")
	if err != nil {
		return center.Center{}, fmt.Errorf("create temporary directory: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	tmpDB := filepath.Join(tmpDir, "restored.db")
	if err := extractEntry(archivePath, info.File, tmpDB); err != nil {
		return center.Center{}, err
	}

	// The same checks Verify runs, against the file about to become a live
	// Center: restoring a snapshot that does not match its manifest would put
	// silently wrong data in front of the user.
	for _, check := range Checks {
		if r := runCheck(ctx, check, info, tmpDB); !r.OK {
			return center.Center{}, fmt.Errorf(
				"this backup did not pass its checks and was not restored (%s: %s)",
				check.Label(), r.Detail)
		}
	}

	c, err := center.Create(appDir, name)
	if err != nil {
		return center.Center{}, err // includes center.ErrExists
	}

	// From here on the Center directory exists, so any failure has to clean it
	// up rather than leave a Center with no database in the selection list.
	if err := moveFile(tmpDB, c.DBPath()); err != nil {
		os.RemoveAll(c.Dir)
		return center.Center{}, fmt.Errorf("write the restored database: %w", err)
	}
	return c, nil
}

// extractEntry writes the named archive entry to dest.
func extractEntry(archivePath, entryName, dest string) error {
	zr, err := zip.OpenReader(archivePath)
	if err != nil {
		return fmt.Errorf("open archive: %w", err)
	}
	defer zr.Close()

	for _, f := range zr.File {
		if f.Name != entryName {
			continue
		}
		return extractOne(f, dest)
	}
	return fmt.Errorf("the archive has no entry named %q", entryName)
}

// moveFile moves src to dest, falling back to a copy when the two are on
// different filesystems -- the temporary directory and the application
// directory often are, so a bare os.Rename is not enough.
func moveFile(src, dest string) error {
	if err := os.Rename(src, dest); err == nil {
		return nil
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	// Streamed rather than read whole: a Center's database can be large.
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Remove(src)
}
