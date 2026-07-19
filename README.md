# Checkin

A local desktop app for checking students in and out each day. Tracks students,
records an append-only log of actions, and can drive check-ins from a webcam
that scans per-student QR codes. All data lives in a durable local SQLite file.

## Stack

- **Go + Fyne** — GUI
- **gocv (OpenCV 4)** — webcam capture + QR detection with bounding-box overlay
- **sqlc + modernc.org/sqlite** — type-safe DB code, pure-Go SQLite driver
- **skip2/go-qrcode + go-pdf/fpdf** — QR generation + printable PDF sheets

## Prerequisites (macOS)

```sh
brew install opencv@4 pkg-config
go install github.com/sqlc-dev/sqlc/cmd/sqlc@latest
go install fyne.io/tools/cmd/fyne@latest
```

> **Why `opencv@4` and not `opencv`?** Homebrew's default `opencv` formula is now
> OpenCV **5**, which gocv does not support. `opencv@4` is keg-only, so it lives
> at `$(brew --prefix opencv@4)` and does not shadow anything. The `Makefile`
> points `PKG_CONFIG_PATH` at it automatically.

## Develop

```sh
make build      # compile everything
make run        # run the app
make test       # run unit tests (DB, state, pruner, QR/PDF)
make generate   # regenerate db/gen from db/schema.sql + db/queries.sql
```

## Logging

By default the app logs at **INFO** to a rotating file `checkin.log` in the same
directory as the database. Rotation keeps disk usage bounded (10 MB per file, up
to 5 compressed backups, 90-day max age).

```sh
checkin                       # INFO -> checkin.log next to the DB (rotated)
checkin -log-level DEBUG      # more verbose
checkin -log-file -           # log to stdout only, no file, no rotation
checkin -log-file /var/log/checkin -log-level WARN   # custom directory
```

Levels: `DEBUG`, `INFO`, `WARN`, `ERROR`. At DEBUG you also get: each student
check-in/out/add/remove, each QR detection (with the decoded JSON), and the raw
string of any QR code that failed to parse. Webcam presence is logged at startup
(INFO if found, WARN if not).

## Data

- **Location:** `~/Library/Application Support/checkin/checkin.db` (macOS),
  `%AppData%\checkin\checkin.db` (Windows). Created on first run.
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

## Distribution ⚠️

`make package` builds a Fyne app bundle, **but the binary is not
self-contained.** It dynamically links OpenCV dylibs by absolute Homebrew path
(e.g. `/opt/homebrew/opt/opencv@4/lib/libopencv_*.dylib`). On another machine it
will only launch if either:

1. **The target has `opencv@4` installed** at the same prefix
   (`brew install opencv@4`) — simplest for internal/known machines; or
2. **The dylibs are bundled** into the `.app` and their load paths rewritten
   (e.g. with `dylibbundler` or `install_name_tool` + `@rpath`) — required for
   distributing to machines without Homebrew/OpenCV.

Choose the approach before shipping. For Windows, ship the OpenCV runtime DLLs
alongside the `.exe`.
