# Checkin

A local desktop app for checking students in and out each day. Tracks students,
records an append-only log of actions, and can drive check-ins from a webcam
that scans per-student QR codes. All data lives in a durable local SQLite file.

## Stack

- **Go + Fyne** — GUI
- **pion/mediadevices** — webcam enumeration + capture (AVFoundation on macOS,
  DirectShow on Windows)
- **gocv (OpenCV 5)** — QR detection, with a bounding-box overlay drawn in Go
- **sqlc + modernc.org/sqlite** — type-safe DB code, pure-Go SQLite driver
- **skip2/go-qrcode + go-pdf/fpdf** — QR generation + printable PDF sheets

## Prerequisites (macOS)

```sh
brew install cmake pkg-config
go install github.com/sqlc-dev/sqlc/cmd/sqlc@latest
go install fyne.io/tools/cmd/fyne@latest

make opencv                 # one-time: compile the slim static OpenCV 5.0.0
```

That compiles OpenCV from source (20–40 min, once per architecture) into
`~/opencv-static/5.0.0-<arch>`, outside the repo. Every later target links it,
so builds are self-contained and **no Homebrew OpenCV is needed**.

> **Why not `brew install` opencv?** It only links dynamically; we link
> statically to ship a single binary. `opencv-env.sh` sets `PKG_CONFIG_LIBDIR`
> (which *replaces* pkg-config's search path) rather than `PKG_CONFIG_PATH`
> (which only prepends), so a Homebrew OpenCV can never be picked up by
> accident.

## Prerequisites (Windows)

gocv links OpenCV through **cgo**, which needs OpenCV built with the **same
toolchain** (MinGW/GCC), so opencv.org's MSVC binaries and scoop's `opencv` do
**not** work. `make opencv` compiles it from source, so no OpenCV package is
needed — only the toolchain:

```powershell
scoop install msys2      # or install MSYS2 from msys2.org
```

```sh
# In an MSYS2 shell:
pacman -Syu   # run once; reopen the shell if it asks you to
pacman -S mingw-w64-x86_64-toolchain mingw-w64-x86_64-cmake \
          mingw-w64-x86_64-ninja     mingw-w64-x86_64-pkgconf
```

Then, from Git Bash:

```sh
make opencv          # one-time: compile the slim static OpenCV 5.0.0
make build-windows   # -> ./checkin.exe + dist/checkin-<version>.exe

./build-windows.sh --run       # build, then launch
./build-windows.sh --console   # keep the console window (stdout logging)
```

The exe is built with `-H=windowsgui` by default so no console window appears
behind the UI.

### Cross-compiling from macOS or Linux

The same targets run on a non-Windows host, so `checkin.exe` can be built
without a Windows machine:

```sh
brew install mingw-w64   # macOS (apt install mingw-w64 on Debian)
make opencv-windows      # one-time: cross-build OpenCV for Windows
make build-windows       # -> ./checkin.exe + dist/checkin-<version>.exe
```

That OpenCV installs to `~/opencv-static/5.0.0-windows`, alongside (not
replacing) the native `5.0.0-<arch>` build; `opencv-env.sh` selects it via
`TARGET_OS=windows`.

> A cross-built exe cannot be run on the build host (`--run` is refused there).
> `make build-windows` verifies its imports are all system DLLs, but
> **smoke-test it on real Windows before shipping** — especially the webcam,
> which nothing on the host can exercise.

Both platforms link OpenCV the same way: every entry point sources
`opencv-env.sh`, which resolves the flags through `pkg-config` and exports
`CGO_*`. There are no build tags for linking. The script absorbs the platform
differences — MSYS2's `mingw64\bin` on `PATH` and stripping two MSVC artifacts
(`-lRunTmChk`, `-lntdll.a`) from OpenCV's generated `.pc` on Windows; stripping
two malformed framework flags and adding `-arch` on macOS.

## Develop

```sh
make checkin    # compile the app        -> ./checkin
make run        # compile, then run it
make test       # run unit tests (DB, state, QR/PDF)
make bench      # QR detector benchmarks (internal/camera)
make vet        # go vet
make generate   # regenerate db/gen from db/schema.sql + db/queries.sql
```

These work on macOS and Windows (Git Bash), and link the OpenCV built by
`make opencv`; if it is missing, the build stops with the command to build it.
Pass extra `go test` flags with `ARGS`:

```sh
make test ARGS="./internal/camera/ -run TestQRDetectHitRate -v"
make bench ARGS=-benchtime=3s
```

> A bare `go test ./...` will **not** work: cgo needs the `CGO_*` flags that
> `opencv-env.sh` exports. Use `make test`, or `source ./opencv-env.sh` once in
> your shell and then run `go` directly.

## Logging

By default the app logs at **INFO** to a rotating file `checkin.log` inside the
open Center's directory (before a Center is selected, startup logs to stdout).
Rotation keeps disk usage bounded (10 MB per file, up to 5 compressed backups,
90-day max age).

```sh
checkin                       # INFO -> checkin.log in the Center dir (rotated)
checkin -log-level DEBUG      # more verbose
checkin -log-file -           # log to stdout only, no file, no rotation
checkin -log-file /var/log/checkin -log-level WARN   # custom directory
```

Levels: `DEBUG`, `INFO`, `WARN`, `ERROR`. At DEBUG you also get: each student
check-in/out/add/remove, each QR detection (with the decoded JSON), and the raw
string of any QR code that failed to parse. Webcam presence is logged at startup
(INFO if found, WARN if not).

## Centers

A **Center** is an independent data set — one laptop can serve different groups
of students at different times. Each Center is a sub-directory of the
application directory and owns its own database and log files. Only one Center
is open at a time.

At startup a window lists the Centers found on disk (discovered by listing
sub-directories) and offers **Add Center…** to create a new one; the name you
type becomes the directory name. To switch Centers, quit and pick another at the
next launch. Deleting a Center means deleting its directory by hand.

`settings.ini` stays at the top level of the application directory and is shared
by every Center.

**One process per Center.** Each instance holds today's check-in/out state in
memory, so two instances open on the same Center would show divergent state and
double-log actions. To prevent this, opening a Center takes an exclusive lock on
a `checkin.db.lock` file in the Center directory; a second attempt to open the
same Center is refused with a message (the selection window stays up so you can
pick another). The lock is an OS advisory lock (`flock` on macOS/Linux,
`LockFileEx` on Windows) released automatically when the process exits, so a
crash leaves nothing to clean up. Different Centers can still be open at once in
separate instances. This assumes the Center lives on a local disk — advisory
locking is unreliable over network shares (SMB/NFS).

```sh
checkin -center "Fall 2026"   # skip the selection window (creates it if absent)
checkin -db-path /tmp/x/checkin.db   # dev escape hatch: open a database directly
```

## Data

- **Location:** `~/Library/Application Support/checkin/<Center>/checkin.db`
  (macOS), `%AppData%\checkin\<Center>\checkin.db` (Windows). Created when the
  Center is first opened.
- **Schema:** `Student(ID, FirstName, LastName)` unique on
  `(LastName, FirstName)`, and an append-only
  `Log(ID, StudentID, FirstName, LastName, Action, Timestamp, AuthorizedAdult)`
  where `Action` is `Added | Checked In | Checked Out | Deleted` and `Timestamp`
  is unix epoch seconds. Indexed on `Timestamp` and `(StudentID, Timestamp)`.
- **Today's state** is held in memory, rebuilt from the log at startup and on
  calendar-day rollover.
- **Retention:** log rows are kept indefinitely. Nothing deletes history.

## Backups

A backup writes **one zip archive covering every Center**, into the directory
set by `backup_dir`. Backups are run from the Center selection window ("Back Up
Now"), which is the one place where no Center is open.

- **Configure:** set `backup_dir` in Settings (empty turns backups off) and
  `backup_count` for how many archives to keep. After each backup the oldest
  archives beyond that count are deleted; only this app's own
  `checkin-backup-*.zip` files are ever removed, so other files in the directory
  are left alone.
- **Contents:** `manifest.json` at the archive root (program version, timestamp,
  and per-Center name, size, SHA-256, student count, and log-row count) plus
  `centers/<Center>.db`.
- **Snapshots use `VACUUM INTO`**, which runs inside a read transaction and
  writes a fresh, self-contained database. This matters because the database
  runs in WAL mode: copying `checkin.db` on its own would silently omit
  committed transactions still sitting in the `-wal` sidecar, and copying it
  while a writer is active could mix pages from different transactions. The
  snapshot has neither problem and needs no sidecar files alongside it.
- **A Center open in another window is not backed up** -- the run stops and says
  so, rather than snapshotting from under a live writer.
- **Every backup is verified as part of taking it**, and a backup that fails
  verification is reported as a failed backup. Catching a bad archive while the
  source data is still on disk is the whole point; an archive that silently
  failed its checks is worse than none, because it will be relied on.
- **Off-machine copies:** point `backup_dir` at a folder your cloud storage app
  syncs (Google Drive, OneDrive, Dropbox) or at an external drive. The app only
  writes files; whether and when they upload is up to that app, and it cannot
  report on it. A backup on the same disk as the original is lost with it.

### Verifying a backup

**Backups → Browse/Restore…** on the Center selection window lists the archives
in `backup_dir`, newest first. **Verify** re-checks one archive against what its
manifest recorded when the backup was made:

1. Extract the archive to a temporary directory.
2. Per Center: compare the snapshot's **size**, its **SHA-256**, its **Student**
   row count, and its **Log** row count.

All four are recorded from the snapshot, never from the live database: a
snapshot is a rebuilt file, so its size and hash do not match the original even
when the data is identical (see the `VACUUM INTO` note above).

The size check catches truncation; the checksum catches a file altered in place,
which a size comparison alone would miss; the row counts catch a snapshot that
is internally valid but is not the one the manifest describes. Every check runs
even after one fails, so a single pass reports everything wrong with an archive.

An archive written before checksums were recorded says so rather than being
reported as corrupt.

Restoring from an archive is not automated yet: unzip it and copy the wanted
`centers/<Center>.db` over a Center's `checkin.db` while the app is closed.

## QR codes

- Payload: `{"Version":2,"FirstName":"John","LastName":"Smith"}`.
- **v1 codes are not accepted.** v1 carried a single joined `Name` that cannot
  be split back into the two columns reliably, so sheets printed before the
  change must be reprinted. A scan with the wrong version, or a missing first or
  last name, is ignored (logged at DEBUG).
- **Admin → Generate QR PDF…** produces a printable grid PDF (all students or a
  selected subset) and opens it in the system viewer.
- With a webcam connected, a scanned code opens the same check-in/out popup as a
  double-click; an unknown code opens the Add-student dialog prefilled. Each
  student has an 8s scan cooldown.

## Distribution

Every build links the slim static OpenCV, so the only dynamic dependencies are
OS libraries — the binaries run on a stock machine with no OpenCV installed.

```sh
make build-darwin   # -> ./checkin       (stripped, self-contained)
make bundle         # -> ./Checkin.app   (icon + Info.plist, via Fyne)
make build-windows  # -> dist/checkin-<version>.exe
```

`build-darwin` strips debug symbols and verifies with `otool -L` that every
entry is under `/usr/lib` or `/System/Library`; `build-windows` verifies the
exe imports only system DLLs. Both fail rather than ship an unverified binary.

Cross-build for Intel Macs from Apple Silicon (clang cross-compiles natively —
not a Rosetta build):

```sh
ARCH=x86_64 make opencv         # once
ARCH=x86_64 make build-darwin   # -> ./checkin-x86_64
```

Third-party open-source license notices are embedded in the app itself under
**About → Licenses**; no separate license file needs to ship alongside the exe.
