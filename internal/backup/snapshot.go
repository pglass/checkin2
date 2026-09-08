package backup

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/gofrs/flock"
	"github.com/pglass/checkin/internal/center"
	_ "modernc.org/sqlite"
)

// lockSuffix must match store's: backups take the same per-Center lock the app
// takes when a Center is open, so a backup and a running Center can never touch
// one database at the same time.
const lockSuffix = ".lock"

// snapshotCenter writes a consistent snapshot of c's database to destPath and
// returns the manifest entry describing it.
//
// The Center is opened read-only for the duration. Backups run from the startup
// screen with no Center open, but another instance of the app may hold this
// Center, so the same advisory lock the store uses is taken first: a Center in
// use is reported rather than snapshotted from under a live writer.
//
// A Center whose database file does not exist yet -- created but never opened
// -- is snapshotted as an empty database rather than treated as an error, so one
// unused Center cannot fail the whole backup.
func snapshotCenter(ctx context.Context, c center.Center, destPath string) (CenterInfo, error) {
	info := CenterInfo{Name: c.Name, File: filepath.ToSlash(filepath.Join("centers", snapshotName(c.Name)))}

	dbPath := c.DBPath()
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		// Nothing to snapshot: write an empty file so the archive still has an
		// entry for this Center and the manifest stays in step with it.
		if err := os.WriteFile(destPath, nil, 0o644); err != nil {
			return CenterInfo{}, err
		}
		return info, nil
	} else if err != nil {
		return CenterInfo{}, err
	}

	lock := flock.New(dbPath + lockSuffix)
	locked, err := lock.TryLock()
	if err != nil {
		return CenterInfo{}, fmt.Errorf("acquire center lock: %w", err)
	}
	if !locked {
		return CenterInfo{}, fmt.Errorf("this Center is open in another window")
	}
	defer lock.Unlock()

	db, err := sql.Open("sqlite", snapshotDSN(dbPath))
	if err != nil {
		return CenterInfo{}, err
	}
	defer db.Close()

	// VACUUM INTO refuses to overwrite, and MkdirTemp gives us a fresh
	// directory, so the destination should not exist -- but a retried run in a
	// reused directory would otherwise fail confusingly.
	if err := os.Remove(destPath); err != nil && !os.IsNotExist(err) {
		return CenterInfo{}, err
	}

	// VACUUM INTO rebuilds the database into a new file inside a read
	// transaction: the result merges any not-yet-checkpointed WAL content, is
	// consistent as of one instant, and needs no -wal sidecar of its own.
	if _, err := db.ExecContext(ctx, `VACUUM INTO `+quoteSQLString(destPath)); err != nil {
		return CenterInfo{}, fmt.Errorf("snapshot database: %w", err)
	}

	st, err := os.Stat(destPath)
	if err != nil {
		return CenterInfo{}, err
	}
	info.SizeBytes = st.Size()

	// Counted from the snapshot rather than the live database so the number
	// always describes what is actually in the archive.
	count, err := studentCount(ctx, destPath)
	if err != nil {
		return CenterInfo{}, err
	}
	info.StudentCount = count

	return info, nil
}

// studentCount opens a snapshot and counts its students, for the manifest.
func studentCount(ctx context.Context, path string) (int64, error) {
	db, err := sql.Open("sqlite", snapshotDSN(path))
	if err != nil {
		return 0, err
	}
	defer db.Close()

	var n int64
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM Student`).Scan(&n); err != nil {
		return 0, fmt.Errorf("count students: %w", err)
	}
	return n, nil
}

// snapshotDSN builds a connection string for reading a database during a
// backup. busy_timeout is set so a momentary lock does not fail the run;
// url encoding keeps paths with spaces (e.g. macOS "Application Support")
// valid, as in store's dsn.
func snapshotDSN(path string) string {
	q := url.Values{}
	q.Add("_pragma", "busy_timeout(5000)")
	return "file:" + path + "?" + q.Encode()
}

// quoteSQLString renders s as a SQL string literal. VACUUM INTO takes its
// destination as a literal rather than a bound parameter, so the path has to be
// escaped here; doubling single quotes is SQLite's escape.
func quoteSQLString(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}
