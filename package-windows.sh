#!/usr/bin/env bash
# Package the checkin app into a single, self-contained Windows executable.
#
# Builds checkin.exe against the slim STATIC OpenCV (see build-opencv-static.sh)
# so the result depends only on Windows system DLLs -- there are no OpenCV, Qt,
# or MinGW runtime DLLs to bundle. The whole app is the one dist/checkin.exe.
#
# Usage:
#   ./package-windows.sh             # build static exe, copy to dist/checkin.exe
#   ./package-windows.sh --no-build  # package the existing ./checkin.exe as-is
#   ./package-windows.sh --zip       # also wrap it in dist/checkin-windows.zip
set -euo pipefail
cd "$(dirname "$0")"

BUILD=1
ZIP=0
for arg in "$@"; do
  case "$arg" in
    --no-build) BUILD=0 ;;
    --zip) ZIP=1 ;;
    *) echo "unknown option: $arg" >&2; exit 2 ;;
  esac
done

OUT_DIR="dist"
# Version the packaged artifacts (build-windows.sh builds a plain ./checkin.exe;
# only the distributable copy carries the version). Single source of truth is the
# Makefile / build-windows.sh default; keep this in sync. Exported below so the
# build stamps the same version the file is named for. Override with VERSION=x.y.z.
VERSION="${VERSION:-0.0.5}"
export VERSION
EXE_NAME="checkin-$VERSION.exe"
ZIP_PATH="$OUT_DIR/checkin-$VERSION-windows.zip"

# --- Toolchain on PATH ------------------------------------------------------
# Sourcing the shared OpenCV env puts mingw64/bin on PATH, which is what ldd
# needs below to resolve DLL references by loader rules -- if the static link
# leaked a non-system DLL, ldd must be able to find it so the check fails.
source "$(dirname "$0")/opencv-env.sh"

# --- Build the single self-contained exe ------------------------------------
if [ "$BUILD" -eq 1 ]; then
  ./build-windows.sh --gui
fi
[ -f checkin.exe ] || { echo "checkin.exe not found; run without --no-build." >&2; exit 1; }

# Prove it is truly self-contained: no dependency may resolve into mingw64, and
# nothing may be missing. Only Windows system DLLs (C:\Windows\...) are allowed.
if ldd checkin.exe | grep -qiE '/mingw64/|=> not found'; then
  echo "Static build is NOT self-contained -- these deps are non-system:" >&2
  ldd checkin.exe | grep -iE '/mingw64/|not found' >&2
  echo "Something failed to link statically; aborting." >&2
  exit 1
fi

mkdir -p "$OUT_DIR"
cp checkin.exe "$OUT_DIR/$EXE_NAME"
echo "Single exe -> $OUT_DIR/$EXE_NAME ($(du -h checkin.exe | cut -f1))"

# --- Optionally zip it up (PowerShell's Compress-Archive; git bash has no zip) --
if [ "$ZIP" -eq 1 ]; then
  rm -f "$ZIP_PATH"
  powershell.exe -NoProfile -NonInteractive -Command \
    "Compress-Archive -Path '$(cygpath -w "$OUT_DIR/$EXE_NAME")' -DestinationPath '$(cygpath -w "$ZIP_PATH")' -Force" \
    || { echo "Compress-Archive failed." >&2; exit 1; }
  echo "Packaged $(du -h "$ZIP_PATH" | cut -f1) -> $ZIP_PATH"
fi
