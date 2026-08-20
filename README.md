# Checkin

A local desktop app for checking students in and out each day. Tracks students,
records an append-only log of actions, and can drive check-ins from a webcam
that scans per-student QR codes. All data lives in a durable local SQLite file.

## Stack

- **Go + Fyne** — GUI
- **pion/mediadevices** — webcam enumeration + capture (AVFoundation on macOS,
  DirectShow on Windows)
- **gocv (OpenCV 4)** — QR detection, with a bounding-box overlay drawn in Go
- **sqlc + modernc.org/sqlite** — type-safe DB code, pure-Go SQLite driver
- **skip2/go-qrcode + go-pdf/fpdf** — QR generation + printable PDF sheets

## Prerequisites (macOS)

```sh
brew install cmake pkg-config
go install github.com/sqlc-dev/sqlc/cmd/sqlc@latest
go install fyne.io/tools/cmd/fyne@latest

make opencv-static-darwin   # one-time: compile the slim static OpenCV 4.13.0
```

That last step compiles OpenCV from source (20–40 min) and installs it to
`~/opencv-static/4.13.0-<arch>`, outside the repo. Everything afterwards —
`make checkin`, `make test`, `make package` — links it, so builds are
self-contained and **no Homebrew OpenCV is needed**.

> **Why not `brew install opencv@4`?** It works for compiling, but produces a
> binary that dynamically links Homebrew dylibs by absolute path, so it only
> runs on machines with the same install. The static build removes that
> constraint, and pins OpenCV **4.13.0** — the exact version gocv v0.43.0
> targets. (Homebrew's default `opencv` formula is OpenCV **5**, which gocv does
> not support at all.)
>
> The Makefile sets `PKG_CONFIG_LIBDIR` rather than `PKG_CONFIG_PATH`, which
> *replaces* pkg-config's search path instead of prepending to it — so an
> `opencv@4` you happen to have installed can never be picked up by accident.

Rebuilding OpenCV is only needed once per architecture. `ARCH=x86_64` builds for
Intel Macs from an Apple Silicon machine; see "Distribution".

## Prerequisites (Windows)

gocv links OpenCV through **cgo**, which needs OpenCV built with the **same
toolchain** (MinGW/GCC). So opencv.org's prebuilt binaries (MSVC) and scoop's
`opencv` (v5) do **not** work. Use MSYS2's precompiled MinGW build of OpenCV 4:

```powershell
# 1. Install MSYS2 (via scoop; or from msys2.org)
scoop install msys2

# 2. In an MSYS2 shell, install the toolchain + CMake + Ninja + pkg-config.
#    build-opencv-static.sh compiles OpenCV 4 from source, so no OpenCV (or Qt)
#    package is needed: the trimmed static build links neither highgui nor a GUI
#    backend (see third_party/gocv and build-opencv-static.sh's BUILD_LIST).
pacman -Syu   # run once; reopen the shell if it asks you to
pacman -S mingw-w64-x86_64-toolchain mingw-w64-x86_64-cmake \
          mingw-w64-x86_64-ninja     mingw-w64-x86_64-pkgconf
```

The static build pins OpenCV **4.13.0**, the exact version gocv v0.43.0 targets.
Build the OpenCV libs once, then the app:

```shell
./build-opencv-static.sh    # one-time: compile the slim static OpenCV 4.13.0
./build-windows.sh          # -> ./checkin.exe
./build-windows.sh --run    # build, then launch
./build-windows.sh --gui    # no console window (for distribution)
```

> **How the Windows build differs.** Both platforms link a slim static OpenCV,
> but they feed cgo differently. macOS uses gocv's **`opencvstatic`** tag, which
> resolves everything through `pkg-config --static opencv4`; Windows uses the
> **`customenv`** tag and sets the `CGO_*` flags explicitly, because pkg-config
> discovery does not work there. The Windows build also needs MSYS2's
> `mingw64\bin` on `PATH` so `gcc` resolves, and links the MinGW runtime into
> the exe. Either way the result is one self-contained binary that depends only
> on OS libraries (see "Distribution").

## Develop

```sh
make checkin    # compile the app        -> ./checkin
make run        # compile, then run it
make test       # run unit tests (DB, state, pruner, QR/PDF)
make generate   # regenerate db/gen from db/schema.sql + db/queries.sql
```

These link the static OpenCV built by `make opencv-static-darwin`; if it is
missing, the build stops with the command to build it.

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
by every Center. Background pruning only touches the open Center's database.

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
- **Schema:** `Student(ID, Name unique)` and an append-only
  `Log(ID, StudentID, StudentName, Action, Timestamp)` where `Action` is
  `Added | Checked In | Checked Out | Deleted` and `Timestamp` is unix epoch
  seconds. Indexed on `Timestamp` and `(StudentID, Timestamp)`.
- **Today's state** is held in memory, rebuilt from the log at startup and on
  calendar-day rollover.
- **Retention:** a background goroutine prunes log rows older than 180 days in
  100-row batches (see `internal/store/prune.go`). It does not run at startup.

## QR codes

- Payload: `{"Version":1,"Name":"John Smith"}`.
- **Admin → Generate QR PDF…** produces a printable grid PDF (all students or a
  selected subset) and opens it in the system viewer.
- With a webcam connected, a scanned code opens the same check-in/out popup as a
  double-click; an unknown code opens the Add-student dialog prefilled. Each
  student has an 8s scan cooldown.

## Distribution

Every macOS build links the slim static OpenCV, so the binary's only dynamic
dependencies are macOS system frameworks — it runs on a stock Mac with no
Homebrew and no OpenCV installed.

```sh
make opencv-static-darwin   # once per arch (slow; builds OpenCV 4.13.0)
make package-darwin         # -> ./checkin      (stripped, self-contained)
make package                # -> ./Checkin.app  (bundle with icon + Info.plist)
```

`package-darwin` produces a bare binary and strips debug symbols; `package`
produces a proper `.app` via Fyne. Both are self-contained.

Cross-build for Intel Macs from an Apple Silicon machine (clang cross-compiles
natively, so this is not a Rosetta build):

```sh
ARCH=x86_64 make opencv-static-darwin   # once
ARCH=x86_64 make package-darwin         # -> ./checkin-x86_64
```

Verify with `otool -L ./checkin` — every entry should be under `/usr/lib` or
`/System/Library`. `build-darwin.sh` checks this for you and warns otherwise.

**Windows** works the same way: it links a slim static OpenCV, so the build is a
single self-contained `checkin.exe` with no DLLs to ship. One-time, build the
static OpenCV, then package:

```sh
make opencv-static     # once per OpenCV version (slow; builds OpenCV 4.13.0)
make package-windows   # -> dist/checkin.exe (single self-contained binary)
```

Third-party open-source license notices are embedded in the app itself under
**About → Licenses**; no separate license file needs to ship alongside the exe.
