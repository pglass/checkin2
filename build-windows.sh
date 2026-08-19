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
export CGO_ENABLED=1
export CGO_CXXFLAGS="--std=c++11 -DNDEBUG"

# Link the custom slim static OpenCV (built by build-opencv-static.sh) into a
# single self-contained exe -- no OpenCV/MinGW DLLs bundled. Must match
# build-opencv-static.sh's PREFIX default.
OPENCV_STATIC_PREFIX="${OPENCV_STATIC_PREFIX:-$HOME/opencv-static/4.13.0}"
# opencv4.pc installs under x64/mingw/staticlib/pkgconfig on Windows; locate it.
PC_FILE="$(find "$OPENCV_STATIC_PREFIX" -name opencv4.pc 2>/dev/null | head -1)"
if [ -z "$PC_FILE" ]; then
  echo "static opencv4.pc not found under $OPENCV_STATIC_PREFIX." >&2
  echo "Build it first:  ./build-opencv-static.sh" >&2
  exit 1
fi
export PKG_CONFIG_PATH="$(dirname "$PC_FILE")"
# --dont-define-prefix: honor the absolute prefix baked into opencv4.pc, else
# pkgconf recomputes a wrong one from the deep staticlib/pkgconfig nesting.
# --static-libgcc/-libstdc++ and -static pull the C/C++/pthread runtime into the
# exe too, so the only remaining imports are Windows system DLLs.
PKGCFG="pkg-config --dont-define-prefix"
export CGO_CPPFLAGS="$($PKGCFG --cflags opencv4)"
# OpenCV's generated opencv4.pc has two Windows/MSVC artifacts that break mingw
# linking, so sanitise the flags:
#   -lRunTmChk.a : an MSVC runtime-check lib absent in mingw -> drop it (OpenCV
#                  was built by this same gcc, so nothing actually needs it)
#   -lntdll.a    : the .a suffix is invalid in an -l name -> -lntdll (which exists)
OPENCV_LIBS="$($PKGCFG --static --libs opencv4 | sed -E 's/ -lRunTmChk(\.a)?//g; s/ -lntdll\.a/ -lntdll/g')"
# opencv4.pc omits a Win32 import lib the statically-linked OpenCV needs:
#   comdlg32  - highgui window_w32.cpp save/open dialogs (Get{Save,Open}FileNameA)
# highgui's Win32 code may or may not survive --gc-sections, so link comdlg32
# unconditionally. It imports a system DLL, so the exe stays portable.
#
# DirectShow libs are NOT listed here. Webcam capture is pion/mediadevices, whose
# camera_windows.go declares its own `#cgo LDFLAGS: -lstrmiids -lole32 -loleaut32
# -lquartz`. Note this only works because the OpenCV build now sets WITH_DSHOW=OFF:
# OpenCV's cap_dshow.cpp defines the DirectShow IIDs itself and would collide with
# strmiids ("multiple definition"). Turning DSHOW back on means that clash returns.
WIN32_LIBS="-lcomdlg32"
export CGO_LDFLAGS="$OPENCV_LIBS $WIN32_LIBS -static -static-libgcc -static-libstdc++"
echo "OpenCV: $($PKGCFG --modversion opencv4) (STATIC)  |  gcc: $(gcc --version | head -1)"

# customenv: gocv takes all cgo flags from the CGO_* env above.
# migrated_fynedo: opts into Fyne 2.8's future main-goroutine behaviour.
TAGS="customenv,migrated_fynedo"

# App version. Single source of truth is the Makefile's VERSION; keep this
# default in sync. Override with `VERSION=x.y.z ./build-windows.sh`.
VERSION="${VERSION:-0.0.3}"
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
