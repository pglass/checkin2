# shellcheck shell=bash
# Set up the MinGW toolchain + cgo environment to link the custom slim STATIC
# OpenCV 4.13.0 (built by build-opencv-static.sh) via gocv's `customenv` build
# tag. SOURCE this file (don't execute it) so the exports land in the caller's
# shell: `source ./opencv-env-windows.sh`. Both build-windows.sh and
# test-windows.sh use it so builds and tests link the exact same OpenCV lib.
#
# Assumes the caller has already run `set -euo pipefail`. Exits (via `return`
# when sourced) non-zero on missing toolchain / OpenCV.

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
  return 1
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
  return 1
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
