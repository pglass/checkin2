# Checkin

A local desktop app for checking students in and out each day. It supports
QR code check-ins using a webcam. All data is stored in a local SQLite file.

## Stack

- **Go + Fyne** — GUI
- **pion/mediadevices** — webcam enumeration and capture (AVFoundation on macOS,
  DirectShow on Windows)
- **gocv (OpenCV 5)** — QR detection, with a bounding-box overlay drawn in Go
- **sqlc + modernc.org/sqlite** — type-safe DB code, pure-Go SQLite driver
- **skip2/go-qrcode + go-pdf/fpdf** — QR generation and printable PDF sheets

## Prerequisites

<details>

<summary>MacOS</summary>

```sh
brew install cmake pkg-config
go install github.com/sqlc-dev/sqlc/cmd/sqlc@latest
go install fyne.io/tools/cmd/fyne@latest

make opencv                 # one-time: compile the slim static OpenCV 5.0.0
```

`make opencv` compiles OpenCV from source into `~/opencv-static/5.0.0-<arch>`,
outside the repo (takes 20-40 minutes) which is statically linked into
the app build. (No Homebrew OpenCV which only supports dynamic linking).

</details>

<details>

<summary>Windows</summary>


gocv links OpenCV through cgo. Using MingW/GCC on Windows requires OpenCV built
with that toolchain, so we can't use MSVC prebuilt binaries from scoop or etc.

`make opencv` compiles OpenCV from source, so you only need the toolchain.

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

The exe is built with `-H=windowsgui` by default, so no console window appears
behind the UI.

</details>

<details>

<summary>Cross-compilation from macOS/Linux</summary>

From macOS, you can build `checkin.exe` for Windows:

```sh
brew install mingw-w64   # macOS (apt install mingw-w64 on Debian)
make opencv-windows      # one-time: cross-build OpenCV for Windows
make build-windows       # -> ./checkin.exe + dist/checkin-<version>.exe
```

</details>

## Develop

```sh
make checkin    # compile the app        -> ./checkin
make run        # compile, then run it
make test       # run unit tests (DB, state, QR/PDF)
make bench      # QR detector benchmarks (internal/camera)
make vet        # go vet
make generate   # regenerate db/gen from db/schema.sql + db/queries.sql
```

These targets work on macOS and Windows (Git Bash). They link the OpenCV built
by `make opencv`; if it is missing, the build stops and prints the command to
build it. Pass extra `go test` flags with `ARGS`:

```sh
make test ARGS="./internal/camera/ -run TestQRDetectHitRate -v"
make bench ARGS=-benchtime=3s
```

> Bare `go test ./...` doesn't work without cgo flags. Use `make test`, or
> run `source ./opencv-env.sh` once in your shell.

## Distribution

Every build links the slim static OpenCV, so the only dynamic dependencies are
OS libraries. The binaries run on a stock machine with no OpenCV installed.

```sh
make build-darwin   # -> ./checkin       (stripped, self-contained)
make bundle         # -> ./Checkin.app   (icon + Info.plist, via Fyne)
make build-windows  # -> dist/checkin-<version>.exe
```

`build-darwin` strips debug symbols and uses `otool -L` to check that every
entry is under `/usr/lib` or `/System/Library`. `build-windows` checks that the
exe imports only system DLLs. Both fail rather than ship an unverified binary.

To cross-build for Intel Macs from Apple Silicon (clang cross-compiles natively;
this is not a Rosetta build):

```sh
ARCH=x86_64 make opencv         # once
ARCH=x86_64 make build-darwin   # -> ./checkin-x86_64
```

Third-party open-source license notices are embedded in the app itself under
**About → Licenses**, so no separate license file needs to ship alongside the
exe.



## Application Logging

The app logs at `INFO` to a rotating file `checkin.log` at the top of the
application directory. Log files larger than 10 MB are compressed and rotated.
Up to 5 rotated files are kept, and rotated files older than 90 days are pruned
at the next rotation.

```sh
checkin                       # INFO -> checkin.log in the app dir (rotated)
checkin -log-level DEBUG      # more verbose
checkin -log-file -           # log to stdout only, no file, no rotation
checkin -log-file /var/log/checkin -log-level WARN   # custom directory
```

## Centers

A **Center** is a separate database, allowing one machine to serve different
sets of students. At startup, available Centers are listed and a new Center can
be added. Only one Center can be opened at a time. To switch Centers, quit and
pick another at the next launch.

`settings.ini` and `checkin.log` are at the top level of the application
directory. Each Center is a sub-directory of the application directory,
which holds its database file. To delete a Center, delete its directory by hand.

The app enforces one process per Center. Opening a Center takes an exclusive
lock at `<centerDir>/checkin.db.lock`. The lock is an OS advisory lock (`flock`
on macOS/Linux, `LockFileEx` on Windows) and the OS releases it when the
process exits, so a crash should leave no stale lock to clean up. This assumes
the Center is on a local disk, because advisory locking is unreliable over
network shares

```sh
checkin -center "Fall 2026"   # skip the selection window (creates it if absent)
checkin -db-path /tmp/x/checkin.db   # dev escape hatch: open a database directly
```

## Data

- **Location:** `~/Library/Application Support/checkin/<Center>/checkin.db` on
  macOS, `%AppData%\checkin\<Center>\checkin.db` on Windows. It is created when
  the Center is first opened.
- **Schema:** `Student(ID, FirstName, LastName)`, unique on
  `(LastName, FirstName)`, plus an append-only
  `Log(ID, StudentID, FirstName, LastName, Action, Timestamp, AuthorizedAdult)`.
  `Action` is one of `Added`, `Checked In`, `Checked Out`, or `Deleted`, and
  `Timestamp` is unix epoch seconds. Indexed on `Timestamp` and
  `(StudentID, Timestamp)`.
- **Today's state** is also held in memory. It is rebuilt from the log at startup and
  at each calendar-day rollover.
- **Retention:** log rows are kept indefinitely. Nothing deletes history.

## QR codes

- Payload: `{"Version":2,"FirstName":"John","LastName":"Smith"}`.
- **Version 1 codes are not accepted.** v1 carried a single joined `Name` field.
- **Admin → Generate QR PDF…** produces a printable grid PDF, for all students
  or a selected subset, and opens it in the system viewer.
- With a webcam connected, a scanned code opens the same check-in/out popup as a
  double-click. An unknown code opens the Add-student dialog with the name
  filled in. Each student has an 8s scan cooldown.

## Backups

A backup creates an archive of all Center databases into a separate directory
(`backup_dir`). The `backup_dir` can be placed on separate storage or synced
to cloud storage.

- **Configuration:** set `backup_dir` and `backup_count` for the number of
  recent archives to keep.
- **Retention** keeps an archive if either rule applies: it is one of the
  `backup_count` most recent, or it is the newest archive of its calendar month.
  Recent backups stay dense while older ones thin out to one per month, instead
  of the history ending a few days back. Pruning runs after each backup, and
  only this app's own `checkin-backup-*.zip` files are ever removed. With
  the default `backup_count = 14` and a daily backup, two years leaves 37
  archives: the last 14 days plus the final backup of each preceding month.
- **Contents:** `manifest.json` at the archive root, plus `centers/<Center>.db`.
  The manifest holds the program version, the timestamp, and for each Center its
  name, size, SHA-256, student count, and log-row count.
- **Snapshots use `VACUUM INTO`**, which runs inside a read transaction and
  writes a fresh, self-contained database file. Copying `checkin.db` on its own
  would silently omit committed transactions still sitting in the `-wal` sidecar,
  and copying it while a writer is active could mix pages from different
  transactions. A `VACUUM INTO` snapshot has neither problem, and needs no
  sidecar files alongside it.
- **A Center open in another window is not backed up.** The run stops and says
  so, rather than snapshotting from under a live writer.
- **Every backup is verified as part of taking it**, and a backup that fails
  verification is reported as a failed backup. The point is to catch a bad
  archive while the source data is still on disk. An archive that silently
  failed its checks is worse than no archive, because it will be relied on.

### Verifying a backup

**Backups → Browse/Restore…** on the Center selection window lists the archives
in `backup_dir`, newest first. **Verify** re-checks one archive against what its
manifest recorded when the backup was made:

1. Extract the archive to a temporary directory.
2. For each Center, compare the snapshot's **size**, its **SHA-256**, its
   **Student** row count, and its **Log** row count.

### Restoring a backup

Backup archives can be browsed, verified, and restored. Restoring a backup adds a new
Center named `<CenterName>-<backupTimestamp>`. No Center data is overwritten during a restore.

Center names must be unique, so Centers can be deactivated and renamed to facilitate backup exploration and restoration. Deactivating a Center hides the Center from the default view, and frees up the Center name without deleting any data. A deactivated Center can be re-activated later if needed.

For example, if you want a Center restored from backup to replace an existing Center of the same name, you can deactivate the existing Center and rename the restored Center. Or, alternatively, if you finish exploring a Center restored from backup, you can deactivate the restored Center to hide it from view.
