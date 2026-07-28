#!/usr/bin/env bash
# Package the checkin app into a self-contained Windows zip.
#
# Produces dist/checkin-windows.zip containing checkin.exe plus every MSYS2 /
# MinGW runtime DLL it depends on (OpenCV, Qt6, libstdc++, libgcc, etc.), so the
# app runs on a Windows machine that does NOT have MSYS2 installed.
#
# How the DLL set is found: we run `ldd` on the built exe (with mingw64/bin on
# PATH) and copy every dependency that resolves inside .../mingw64/bin. That is
# the exact load-time DLL closure, resolved recursively by ldd. Windows system
# DLLs (KERNEL32, USER32, ...) live in C:\Windows and are intentionally skipped.
#
# NOTE: the app uses Fyne for its UI and gocv only for camera capture / decoding
# -- it never opens an OpenCV highgui window -- so Qt is loaded but never
# initialised as a GUI. That means no Qt platform plugin (platforms/qwindows.dll)
# is needed. If a future change calls gocv's window/imshow APIs, you'll also need
# to bundle mingw64/share/qt6/plugins/platforms/qwindows.dll into a platforms/
# subfolder next to the exe.
#
# Two modes:
#   * default (shared) -- bundles checkin.exe + its MinGW DLL closure into a zip.
#   * --static         -- builds a single self-contained checkin.exe against the
#                         slim static OpenCV (see build-opencv-static.sh) and copies
#                         just that one file to dist/. No DLLs to bundle.
#
# Usage:
#   ./package-windows.sh             # shared: build (--gui) then zip exe + DLLs
#   ./package-windows.sh --no-build  # shared: package the existing ./checkin.exe as-is
#   ./package-windows.sh --static    # static: single dist/checkin.exe (no DLLs)
#   ./package-windows.sh --static --zip   # static, wrapped in dist/checkin-windows.zip
set -euo pipefail
cd "$(dirname "$0")"

BUILD=1
STATIC=0
ZIP=0
for arg in "$@"; do
  case "$arg" in
    --no-build) BUILD=0 ;;
    --static) STATIC=1 ;;
    --zip) ZIP=1 ;;
    *) echo "unknown option: $arg" >&2; exit 2 ;;
  esac
done

OUT_DIR="dist"
STAGE_NAME="checkin"          # folder users see after unzipping
STAGE="$OUT_DIR/$STAGE_NAME"
ZIP_PATH="$OUT_DIR/checkin-windows.zip"

# --- Locate MSYS2 / mingw64 (same discovery as build-windows.sh) ------------
MSYS_ROOT=""
if command -v scoop >/dev/null 2>&1; then
  MSYS_ROOT="$(scoop prefix msys2 2>/dev/null | tr -d '\r' || true)"
fi
MINGW_UNIX=""
for cand in "$MSYS_ROOT" "$HOME/scoop/apps/msys2/current" "/c/msys64"; do
  if [ -n "$cand" ] && [ -x "$cand/mingw64/bin/gcc.exe" ]; then
    MINGW_UNIX="$cand/mingw64"
    break
  fi
done
if [ -z "$MINGW_UNIX" ]; then
  echo "mingw64 not found. Install MSYS2 + the mingw toolchain (see README)." >&2
  exit 1
fi
# mingw64/bin must be on PATH so ldd can resolve the OpenCV/Qt DLLs by loader rules.
export PATH="$MINGW_UNIX/bin:$PATH"

# --- Static mode: single self-contained exe, no DLL bundling ----------------
if [ "$STATIC" -eq 1 ]; then
  if [ "$BUILD" -eq 1 ]; then
    ./build-windows.sh --gui --static
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
  cp checkin.exe "$OUT_DIR/checkin.exe"
  echo "Single exe -> $OUT_DIR/checkin.exe ($(du -h checkin.exe | cut -f1))"

  if [ "$ZIP" -eq 1 ]; then
    rm -f "$ZIP_PATH"
    powershell.exe -NoProfile -NonInteractive -Command \
      "Compress-Archive -Path '$(cygpath -w "$OUT_DIR/checkin.exe")' -DestinationPath '$(cygpath -w "$ZIP_PATH")' -Force" \
      || { echo "Compress-Archive failed." >&2; exit 1; }
    echo "Packaged $(du -h "$ZIP_PATH" | cut -f1) -> $ZIP_PATH"
  fi
  exit 0
fi

# --- 1. Build a distributable exe (no console window) -----------------------
if [ "$BUILD" -eq 1 ]; then
  ./build-windows.sh --gui
fi
if [ ! -f checkin.exe ]; then
  echo "checkin.exe not found. Run without --no-build, or run ./build-windows.sh first." >&2
  exit 1
fi

# --- 2. Fresh staging directory ---------------------------------------------
rm -rf "$STAGE"
mkdir -p "$STAGE"
cp checkin.exe "$STAGE/"

# --- 3. Copy the mingw DLL closure ------------------------------------------
# Surface any dependency the loader can't resolve rather than shipping a broken zip.
if ldd checkin.exe | grep -qi 'not found'; then
  echo "Unresolved dependencies:" >&2
  ldd checkin.exe | grep -i 'not found' >&2
  echo "PATH is missing a DLL directory; aborting." >&2
  exit 1
fi

declare -A seen
count=0
# ldd lines look like: "  libfoo.dll => /c/.../mingw64/bin/libfoo.dll (0x...)".
# Take the resolved path ($3) of every dependency living under mingw64/bin.
while IFS= read -r dll; do
  base="$(basename "$dll")"
  [ -n "${seen[$base]:-}" ] && continue      # a DLL can resolve more than once
  seen[$base]=1
  cp "$dll" "$STAGE/"
  count=$((count + 1))
done < <(ldd checkin.exe | awk '/=> \// {print $3}' | grep -iE '/mingw64/bin/.+\.dll$')

if [ "$count" -eq 0 ]; then
  echo "No mingw DLLs were copied -- something is wrong with ldd/PATH." >&2
  exit 1
fi
echo "Bundled $count dependency DLL(s)."

# --- 4. Zip it up (PowerShell's Compress-Archive; git bash has no zip) -------
rm -f "$ZIP_PATH"
STAGE_WIN="$(cygpath -w "$STAGE")"
ZIP_WIN="$(cygpath -w "$ZIP_PATH")"
powershell.exe -NoProfile -NonInteractive -Command \
  "Compress-Archive -Path '$STAGE_WIN' -DestinationPath '$ZIP_WIN' -Force" \
  || { echo "Compress-Archive failed." >&2; exit 1; }

echo "Packaged $(du -h "$ZIP_PATH" | cut -f1) -> $ZIP_PATH"
