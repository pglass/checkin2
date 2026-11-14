# Checkin

A (vibecoded) local, offline desktop app for checking students in and out each day with QR codes.

## Features

* **QR code scanning**: Scan unique QR code with a webcam for quick check-ins
* **Low-cost**: 100% free software that you run on an inexpensive computer
* **Long retention**: Multi-year retention and no limits on number of students or check-ins
* **Local storage**: Fully offline. No internet required.
* **Automatic backups**: Full backups to locally-attached storage
* **History**: View, search, and export history of check-in times

## Install

Find the latest release on Github at https://github.com/pglass/checkin2/releases/latest, and download and run the `checkin-*-setup.exe` installer.

> [!NOTE]
> There are only Windows releases. To request releases for other platforms, open [an
issue](https://github.com/pglass/checkin2/issues).

## Quick tour

This is a brief first time setup and tour of the program.

You will add students to the program, use a QR code to check in and out, and learn a few other features.

1. Add a Center. A Center represents a group of students.
2. Open the Center by clicking on the center name

   * Opening a Center displays the Center view, which is a list of students and their check in/out times for the current day.
   * Since the Center was just added, there are no students so the Center view is empty (go to the next step)

3. Add students to the Center one of two ways:

   * `File > Import...`: To import many students from a spreadsheet
   * `File > Add student`: To add one student at a time

4. Verify your webcam is working for QR code sign in:

   * The program automatically selects a webcam, if it finds one, and begins scanning for QR codes
   * Use the `Camera` menu to manually select a webcam to use for QR-code scanning
     * _If you do not see your camera in the list, ensure the webcam is connected to the computer and restart the program. Webcams are enumerated only on startup._
   * `Camera > Show camera window` to view the camera feed. This shows you what the camera is seeing, but you do not need the camera feed open to scan QR-codes. To hide the feed, just close the camera window.
   * _(In the bottom left of the Center view window will display "Camera On" when a camera is on and scanning for QR codes)_

5. Generate a QR code for checking in or out:

   * To create one QR code, click `Show QR Code` next to a student's name
   * To create many QR codes, use the `File > Generate QR Codes...` menu. Choose the students for which to generate QR codes. When complete, it opens a directory containing the QR code image files.

6. Sign in/out using a QR code

   * Hold a QR code up to the camera (about 6-12" away)
   * The program prompts for an "authorized adult" name when the QR code scans:
     * Enter the name of the adult checking in this student
     * Click "Check In" to complete the check-in
   * Scan the QR code again to check the student out
     * Recent authorized adults for this student are suggested as clickable buttons below the text box.
     * Enter the adult name by clicking the a recent adult's button, or type a name into the text box.
     * Click "Check Out" to check the student out
   * _(Checked-in students are sorted to the top of the list)_
   * _(The program ignores quickly-repeated QR code scans. Once a QR code is scanned once, the same QR code is ignored for several seconds.)_

7. View history of check in times.

   * `File > History` opens the history viewer
   * Optionally, filter history by a start date, end date, and by one or more student names
   * Optionally, select the sort order
   * Press "Retrieve" to display the history in the current window. Press "Save History to File..." to export that history to a text file

8. Configure backups to an external drive

   * First, create a new empty directory on the external drive
   * Go to `File > Settings` to open the Settings menu
   * Find the `backup_dirs` setting, click `+ Add backup directory`, and then click "Browse..."
     * In the Browse window, navigate to the directory on the external drive, and then click Open
   * Trigger a backup from the startup window:
     * To get to the startup window, close the program and then re-open it.
     * Press the "Back up now" button in the bottom right of the startup window (if an automatic backup does not trigger).
     * It may say `A backup was already created today`. You can press `Back up now` to create another one.
   * After the backup completes, you can check the external drive for a `checkin-backup-*.zip` file.
   * _The app automatically takes backups every 2 days. It backups to a default internal directory, and any additional directories you configure._
   * _You can see the last backup time at the bottom of the startup window._
   * _If you have multiple Centers, a backup backs up all centers into a single archive._

# Development

<details>

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
make build-windows   # -> ./checkin.exe + dist/checkin.exe

./scripts/build-windows.sh --run       # build, then launch
./scripts/build-windows.sh --console   # keep the console window (stdout logging)
```

The exe is built with `-H=windowsgui` by default, so no console window appears
behind the UI.

### Code signing

For now, releases are signed with a self-issued certificate chain: a long-lived
root CA kept offline, and a short-lived leaf that does the signing. The root and
its fingerprint are published in [docs/CERT.md](docs/CERT.md).

```sh
make build-windows   # produces dist/checkin.exe
make sign-windows    # signs it in place
make verify-windows  # checks the signature
```

Signing needs `osslsigncode` (`brew install osslsigncode`) and the PKCS#12
bundle, read from `CHECKIN_P12` (default `~/.checkin-signing/checkin-signing.p12`).
The password comes from `CHECKIN_P12_PASS` or an interactive prompt; it is never
passed on the command line. Signing is a separate step from the build so that
building needs no secrets, and so a rebuild cannot silently discard a signature.

Signatures are timestamped, so an exe stays valid after the signing certificate
expires. Renewing means issuing a new leaf from the same root — machines that
already trust the root need no action.

### Releases

A release is a draft GitHub release carrying the signed exe (zipped), a
`manifest.json` describing it, and a signature over that manifest.

```sh
make build-windows   # -> dist/checkin.exe
make sign-windows    # Authenticode-sign it
make manifest        # -> dist/checkin-<version>-windows-amd64.zip + manifest.json
make sign-manifest   # -> manifest.json.p7s + manifest.json.tsr
make release         # draft GitHub release (needs `gh`)
```

`make release` creates a **draft**, so nothing is public until you review the
files and notes and publish it from the web UI. Release notes come from the
matching `# <version>` section of `CHANGELOG.md`.

The manifest records the version and, for each artifact, the download URL and
SHA-256:

```json
{
  "schema": 1,
  "version": "0.0.7",
  "released_at": "2026-09-17T16:42:15Z",
  "notes_url": "https://github.com/pglass/checkin/releases/tag/v0.0.7",
  "artifacts": [
    {
      "os": "windows", "arch": "amd64",
      "filename": "checkin-0.0.7-windows-amd64.zip",
      "url": "https://github.com/pglass/checkin/releases/download/v0.0.7/checkin-0.0.7-windows-amd64.zip",
      "size": 24758641,
      "sha256": "6027cc…",
      "contains": { "filename": "checkin.exe", "sha256": "08bf1f…" }
    }
  ]
}
```

There is no auto-update yet. The manifest exists so one can be added without
reissuing past releases: a client fetches the latest manifest, compares
`version` to its own, downloads the zip, and checks it against `sha256` before
unpacking. The hash covers the **zip**, since that is what gets downloaded —
verifying only the exe inside would mean running an unverified archive through
a zip extractor first.

#### Verifying a manifest

`manifest.json` is signed with the same certificate that signs the exe. The
signature is detached CMS (`manifest.json.p7s`, DER) and carries the signer's
full chain, so verifying needs only the root CA:

```sh
make verify-manifest ROOT=~/.checkin-signing/checkin-root.crt
```

The root must be **pinned** by whoever verifies — compiled into a client, or
obtained out of band. Shipping it next to the manifest would prove nothing,
since anyone able to replace one could replace both. The leaf travels inside
the signature; only the root has to be known in advance.

`manifest.json.tsr` is an RFC3161 timestamp over the manifest, from the same
authority that timestamps the exe. It is what lets a signature outlive the
short-lived signing leaf: the check becomes "was the leaf valid at the time
the token asserts" rather than "is it valid now". Verifying the token needs the
timestamp authority's own root — a second, independent anchor:

```sh
curl -o digicert-tsa-root.pem https://cacerts.digicert.com/DigiCertTrustedRootG4.crt.pem
CHECKIN_TSA_ROOTS=digicert-tsa-root.pem make verify-manifest ROOT=~/.checkin-signing/checkin-root.crt
```

Without `CHECKIN_TSA_ROOTS` the timestamp is reported but not verified — an
unverified token's claimed time is attacker-chosen, so it is never treated as
valid on its own.

</details>

<details>

<summary>Cross-compilation from macOS/Linux</summary>

From macOS, you can build `checkin.exe` for Windows:

```sh
brew install mingw-w64   # macOS (apt install mingw-w64 on Debian)
make opencv-windows      # one-time: cross-build OpenCV for Windows
make build-windows       # -> ./checkin.exe + dist/checkin.exe
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
> run `source ./scripts/opencv-env.sh` once in your shell.

## Distribution

Every build links the slim static OpenCV, so the only dynamic dependencies are
OS libraries. The binaries run on a stock machine with no OpenCV installed.

```sh
make build-darwin   # -> ./checkin       (stripped, self-contained)
make bundle         # -> ./Checkin.app   (icon + Info.plist, via Fyne)
make build-windows  # -> dist/checkin.exe
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

- Payload: a URL of the form `<qr_base_url>/<NameBase64>`, where `NameBase64` is
  `Base64URL(First + "+" + Last)` -- so "John Smith" encodes to
  `https://pglass.github.io/checkin2/qr/Sm9obitTbWl0aA`.
- Only the final path segment is read when scanning, so changing `qr_base_url`
  (settings.ini, not in the Settings window) does not invalidate printed cards.
- **File → Generate QR Code…** writes one 512x640 PNG per student (QR code with
  the name underneath) into `<Desktop>/Checkin QR Codes/<CenterName>/`, for all
  students or a selected subset. Generating one code opens the image; generating
  several opens the folder in the system file browser. Renaming a Center renames
  its folder too.
- With a webcam connected, a scanned code opens the same check-in/out popup as a
  double-click. An unknown code opens the Add-student dialog with the name
  filled in. Each student has an 8s scan cooldown.

## Backups

A backup creates an archive of all Center databases in the **internal backup
directory**, then copies it to each configured **backup directory**
(`backup_dirs`). Backup directories can be on separate storage or synced to
cloud storage.

- **Automatic backups:** at startup, if no backup in the internal directory is
  younger than `auto_backup_frequency_days` (default `2`), a backup runs just as
  if **Back Up Now** were pressed. Any dialog it raises opens with "Automatic
  backup wants to proceed (runs every 2 days)". Nothing runs while no Center has data.
- **Internal backup directory:** always on, and not configurable. It sits beside
  the app directory: `checkin-backups` next to `checkin` (on macOS,
  `~/Library/Application Support/checkin-backups`). Every backup is written and
  fully verified here first. Settings shows its path as `internal_backup_dir`.
- **Backup directories:** none by default. Add them in Settings, or in
  `settings.ini` as numbered keys under `[backup]`:

  ```ini
  backup_dirs1 = /Volumes/USB/checkin-backups
  backup_dirs2 = /Users/me/Dropbox/checkin-backups
  ```

  Numbers only set the order; gaps are fine, and saving from Settings renumbers
  them `1..N`. An older `backup_dir` setting is moved into `backup_dirs` once, at
  startup, and removed from the file.
- **Copying:** after each backup, every archive in the internal directory that a
  backup directory lacks is copied to it, so a newly added directory receives the
  whole history. Each copy is written as `.partial`, checked by SHA-256 against
  the original, then renamed into place. A backup directory that is unavailable
  (an unplugged drive) is reported and skipped; it is never created on the
  system disk in the drive's place. The backup itself still succeeds.
  Before the backup starts, unavailable backup directories are listed with a
  warning: connect the drive and press **Refresh** (once everything is
  available the backup starts after a 4-second countdown), **Proceed** without
  them, or **Cancel**.
- **Backup directories are add-only:** archives already there are never
  overwritten or deleted, including ones the internal directory no longer has.
- **Retention:** every archive is kept; backups are never deleted. Archives are
  small (roughly 10-20 MB for two Centers after ten years of daily use).
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
in a backup directory, newest first. A selector at the top switches between the
internal backup directory, each configured backup directory, and **Browse…**,
which picks any other folder (for example, a drive from another machine). **Verify** re-checks one archive against what its
manifest recorded when the backup was made:

1. Extract the archive to a temporary directory.
2. For each Center, compare the snapshot's **size** and its **SHA-256**, run
   SQLite's **`PRAGMA integrity_check`** on it, then compare its **Student** row
   count and its **Log** row count.

Restoring a Center runs the same checks on its snapshot first, and each new
snapshot is integrity-checked right after `VACUUM INTO`, before it goes into the
archive.

### Restoring a backup

Backup archives can be browsed, verified, and restored. **Explore…** on an
archive lists the Centers inside it, each of which can be restored. Restoring adds
a new Center named `<CenterName>-<backupTimestamp>`. No Center data is
overwritten during a restore.

Center names must be unique, so Centers can be deactivated and renamed to
facilitate backup exploration and restoration. Deactivating a Center hides the
Center from the default view, and frees up the Center name without deleting any
data. A deactivated Center can be re-activated later if needed.

For example, if you want a Center restored from backup to replace an existing
Center of the same name, you can deactivate the existing Center and rename the
restored Center. Or, alternatively, if you finish exploring a Center restored
from backup, you can deactivate the restored Center to hide it from view.

</details>
