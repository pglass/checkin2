#!/usr/bin/env bash
# Build the checkin app on Windows (Git Bash / MSYS) as a single, self-contained
# static executable. Mirror of build-windows.ps1 for bash users.
#
# gocv needs OpenCV built with the same toolchain cgo uses (MinGW/GCC), so
# opencv.org's MSVC binaries and scoop's `opencv` (v5) do NOT work. This links a
# custom slim STATIC OpenCV 4.13.0 (the version gocv v0.43.0 targets) built by
# build-opencv-static.sh, feeding cgo the flags via gocv's `customenv` build tag.
# The result is one checkin.exe that depends only on Windows system DLLs -- no
# OpenCV/Qt/MinGW runtime DLLs to bundle.
#
# One-time setup (see README "Prerequisites (Windows)"):
#   scoop install msys2
#   <msys2> pacman -S mingw-w64-x86_64-toolchain mingw-w64-x86_64-cmake \
#                     mingw-w64-x86_64-ninja      mingw-w64-x86_64-pkgconf
#   ./build-opencv-static.sh    # build the static OpenCV this script links
#
# Usage:
#   ./build-windows.sh          # -> ./checkin.exe (static)
#   ./build-windows.sh --run    # build, then launch
#   ./build-windows.sh --gui    # no console window (for distribution)
set -euo pipefail
cd "$(dirname "$0")"

RUN=0
GUI=0
for arg in "$@"; do
  case "$arg" in
    --run) RUN=1 ;;
    --gui) GUI=1 ;;
    *) echo "unknown option: $arg" >&2; exit 2 ;;
  esac
done

# --- MinGW toolchain + static-OpenCV cgo environment ------------------------
# Sets PATH/CGO_* to link the custom slim static OpenCV. Shared with
# test-windows.sh so builds and tests use the exact same OpenCV lib.
source "$(dirname "$0")/opencv-env-windows.sh"

# customenv: gocv takes all cgo flags from the CGO_* env above.
# migrated_fynedo: opts into Fyne 2.8's future main-goroutine behaviour.
TAGS="customenv,migrated_fynedo"

# App version. Single source of truth is the Makefile's VERSION; keep this
# default in sync. Override with `VERSION=x.y.z ./build-windows.sh`.
VERSION="${VERSION:-0.0.5}"
# -X stamps the in-app version (the About dialog, --version, and startup log).
# -s -w drop the Go symbol table and DWARF debug info: this is a self-contained
# distributable, not a debug target, and stripping them roughly halves the exe
# (~100 MB -> ~56 MB) with no runtime effect. The Makefile's dev build keeps them.
LDFLAGS="-s -w -X github.com/pglass/checkin/internal/version.Version=$VERSION"
[ "$GUI" -eq 1 ] && LDFLAGS="$LDFLAGS -H=windowsgui"

# Generate a versioninfo resource (fyne.syso) so Windows Explorer's file
# Properties -> Details shows a File/Product version. `go build` links any
# *.syso in the main package dir automatically. This mirrors what `fyne
# package` does internally (goversioninfo), but without triggering Fyne's own
# build, which cannot use this project's hand-tuned OpenCV cgo env.
SYSO="./cmd/checkin/fyne.syso"
VI_JSON="$(mktemp)"
# Clear any syso from a previous build so a failure below can't silently link a
# stale version into this exe. Always clean both up on exit.
rm -f "$SYSO"
trap 'rm -f "$SYSO" "$VI_JSON"' EXIT
# goversioninfo wants a four-part x.y.z.build FileVersion; pad missing parts.
IFS='.' read -r VMAJ VMIN VPATCH _ <<<"$VERSION"
cat >"$VI_JSON" <<JSON
{
  "FixedFileInfo": {
    "FileVersion": {"Major": ${VMAJ:-0}, "Minor": ${VMIN:-0}, "Patch": ${VPATCH:-0}, "Build": 0},
    "ProductVersion": {"Major": ${VMAJ:-0}, "Minor": ${VMIN:-0}, "Patch": ${VPATCH:-0}, "Build": 0}
  },
  "StringFileInfo": {
    "ProductName": "Checkin",
    "FileDescription": "Checkin",
    "ProductVersion": "$VERSION",
    "FileVersion": "$VERSION"
  }
}
JSON
if go run github.com/josephspurrier/goversioninfo/cmd/goversioninfo \
     -o "$SYSO" "$VI_JSON"; then
  echo "Wrote versioninfo -> $SYSO (v$VERSION)"
else
  echo "warning: goversioninfo failed; exe will lack File version metadata." >&2
  rm -f "$SYSO"
fi

go build -tags "$TAGS" -ldflags "$LDFLAGS" -o checkin.exe ./cmd/checkin/
echo "Built ./checkin.exe (v$VERSION)"

if [ "$RUN" -eq 1 ]; then
  echo "Running..."
  ./checkin.exe -log-level DEBUG -log-file -
fi
