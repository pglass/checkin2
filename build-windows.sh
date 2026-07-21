#!/usr/bin/env bash
# Build the checkin app on Windows (Git Bash / MSYS) against a MinGW-built
# OpenCV 4 from MSYS2. Mirror of build-windows.ps1 for bash users.
#
# gocv needs OpenCV built with the same toolchain cgo uses (MinGW/GCC), so
# opencv.org's MSVC binaries and scoop's `opencv` (v5) do NOT work. This uses
# MSYS2's precompiled mingw-w64-x86_64-opencv (4.13.0, the version gocv v0.43.0
# targets) and its GCC, feeding cgo the flags from `pkg-config opencv4` via
# gocv's `customenv` build tag.
#
# One-time setup (see README "Prerequisites (Windows)"):
#   scoop install msys2
#   <msys2> pacman -S mingw-w64-x86_64-toolchain mingw-w64-x86_64-opencv \
#                     mingw-w64-x86_64-pkgconf   mingw-w64-x86_64-qt6-5compat
#
# Usage:
#   ./build-windows.sh          # -> ./checkin.exe
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

# --- Locate MSYS2 / mingw64 -------------------------------------------------
# Try `scoop prefix msys2`, then the default scoop path, then C:\msys64.
MSYS_ROOT=""
if command -v scoop >/dev/null 2>&1; then
  MSYS_ROOT="$(scoop prefix msys2 2>/dev/null | tr -d '\r' || true)"
fi
for cand in "$MSYS_ROOT" "$HOME/scoop/apps/msys2/current" "/c/msys64"; do
  if [ -n "$cand" ] && [ -x "$cand/mingw64/bin/gcc.exe" ]; then
    MINGW="$(cygpath -m "$cand/mingw64" 2>/dev/null || echo "$cand/mingw64")"
    MINGW_UNIX="$cand/mingw64"
    break
  fi
done
if [ -z "${MINGW:-}" ]; then
  echo "mingw64 gcc not found. Install MSYS2 + the mingw toolchain (see README)." >&2
  exit 1
fi

# --- Toolchain + cgo environment -------------------------------------------
# Prepend mingw64/bin so cgo uses MSYS2's gcc AND the OpenCV/Qt6/runtime DLLs
# are found at run time. pkg-config supplies the include + link flags.
export PATH="$MINGW_UNIX/bin:$PATH"
export PKG_CONFIG_PATH="$MINGW_UNIX/lib/pkgconfig"
export CGO_ENABLED=1

if ! pkg-config --exists opencv4; then
  echo "pkg-config could not find opencv4. Install mingw-w64-x86_64-opencv in MSYS2 (see README)." >&2
  exit 1
fi
export CGO_CPPFLAGS="$(pkg-config --cflags opencv4)"
export CGO_CXXFLAGS="--std=c++11 -DNDEBUG"
export CGO_LDFLAGS="$(pkg-config --libs opencv4)"

echo "OpenCV: $(pkg-config --modversion opencv4)  |  gcc: $(gcc --version | head -1)"

# customenv: gocv takes all cgo flags from the CGO_* env above.
# migrated_fynedo: opts into Fyne 2.8's future main-goroutine behaviour.
TAGS="customenv,migrated_fynedo"
LDFLAGS=""
[ "$GUI" -eq 1 ] && LDFLAGS="-H=windowsgui"

if [ -n "$LDFLAGS" ]; then
  go build -tags "$TAGS" -ldflags "$LDFLAGS" -o checkin.exe ./cmd/checkin/
else
  go build -tags "$TAGS" -o checkin.exe ./cmd/checkin/
fi
echo "Built ./checkin.exe"

if [ "$RUN" -eq 1 ]; then
  echo "Running..."
  ./checkin.exe -log-level DEBUG -log-file -
fi
