package backup

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"io"
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

// snapshotCenter writes a consistent snapshot of c's database to destPath,
// checks it with SQLite's integrity check, and returns the manifest entry
// describing it. onIntegrityCheck, if not nil, is called just before the
// integrity check starts, so a caller can report it as its own step.
//
// The Center is opened read-only for the duration. Backups run from the startup
// screen with no Center open, but another instance of the app may hold this
// Center, so the same advisory lock the store uses is taken first: a Center in
// use is reported rather than snapshotted from under a live writer.
//
// A Center whose database file does not exist yet -- created but never opened
// -- is snapshotted as an empty database rather than treated as an error, so one
// unused Center cannot fail the whole backup.
func snapshotCenter(ctx context.Context, c center.Center, destPath string, onIntegrityCheck func()) (CenterInfo, error) {
	info := CenterInfo{Name: c.Name, File: filepath.ToSlash(filepath.Join("centers", snapshotName(c.Name)))}

	dbPath := c.DBPath()
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		// Nothing to snapshot: write an empty file so the archive still has an
		// entry for this Center and the manifest stays in step with it. It is
		// still hashed, so verification treats it like any other entry rather
		// than needing an exception for the empty case.
		if err := os.WriteFile(destPath, nil, 0o644); err != nil {
			return CenterInfo{}, err
		}
		sum, err := fileSHA256(destPath)
		if err != nil {
			return CenterInfo{}, err
		}
		info.SHA256 = sum
		if onIntegrityCheck != nil {
			onIntegrityCheck() // reported for every Center, so the steps add up
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

	// VACUUM INTO reads every page of the source, but it copies damage it does
	// not trip over rather than reporting it, and an interrupted write can leave
	// the output incomplete. Checking the snapshot catches both before it is
	// archived and relied on.
	if onIntegrityCheck != nil {
		onIntegrityCheck()
	}
	if err := integrityCheck(ctx, destPath); err != nil {
		return CenterInfo{}, fmt.Errorf("the backup copy of this Center failed SQLite's integrity check, "+
			"so the Center's database may be damaged: %w", err)
	}

	st, err := os.Stat(destPath)
	if err != nil {
		return CenterInfo{}, err
	}
	info.SizeBytes = st.Size()

	sum, err := fileSHA256(destPath)
	if err != nil {
		return CenterInfo{}, err
	}
	info.SHA256 = sum

	// Counted from the snapshot rather than the live database so the numbers
	// always describe what is actually in the archive.
	students, logs, err := rowCounts(ctx, destPath)
	if err != nil {
		return CenterInfo{}, err
	}
	info.StudentCount, info.LogCount = students, logs

	return info, nil
}

// rowCounts opens a snapshot and counts its Student and Log rows, for the
// manifest and for verification to compare against.
//
// A zero-length file counts as zero of each rather than an error: that is what
// a Center created but never opened is snapshotted as, and it is a legitimately
// empty backup, not a broken one.
func rowCounts(ctx context.Context, path string) (students, logs int64, err error) {
	st, err := os.Stat(path)
	if err != nil {
		return 0, 0, err
	}
	if st.Size() == 0 {
		return 0, 0, nil
	}

	db, err := sql.Open("sqlite", snapshotDSN(path))
	if err != nil {
		return 0, 0, err
	}
	defer db.Close()

	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM Student`).Scan(&students); err != nil {
		return 0, 0, fmt.Errorf("count students: %w", err)
	}
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM Log`).Scan(&logs); err != nil {
		return 0, 0, fmt.Errorf("count log rows: %w", err)
	}
	return students, logs, nil
}

// maxIntegrityProblems caps how many problems integrityCheck reports. SQLite
// can list a great many for one damaged file, and the first few say enough.
const maxIntegrityProblems = 3

// integrityCheck runs SQLite's PRAGMA integrity_check on the database at path:
// a full read of the file that checks its page and B-tree structure, that every
// index matches its table, and the NOT NULL, CHECK, and UNIQUE constraints. It
// returns nil when SQLite reports "ok", and otherwise an error naming the first
// problems found.
//
// A zero-length file passes, as in rowCounts: it is how a Center created but
// never opened is snapshotted.
func integrityCheck(ctx context.Context, path string) error {
	st, err := os.Stat(path)
	if err != nil {
		return err
	}
	if st.Size() == 0 {
		return nil
	}

	db, err := sql.Open("sqlite", snapshotDSN(path))
	if err != nil {
		return err
	}
	defer db.Close()

	rows, err := db.QueryContext(ctx, fmt.Sprintf(`PRAGMA integrity_check(%d)`, maxIntegrityProblems))
	if err != nil {
		return fmt.Errorf("run integrity check: %w", err)
	}
	defer rows.Close()

	var problems []string
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			return fmt.Errorf("read integrity check: %w", err)
		}
		problems = append(problems, line)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("run integrity check: %w", err)
	}
	if len(problems) == 1 && problems[0] == "ok" {
		return nil
	}
	return fmt.Errorf("%s", strings.Join(problems, "; "))
}

// fileSHA256 returns the hex-encoded SHA-256 of the file at path. It streams,
// so a large snapshot is not read into memory.
func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
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
