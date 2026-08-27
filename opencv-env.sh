# shellcheck shell=bash
# Set up the cgo environment to link the slim STATIC OpenCV 5 that
# build-opencv-static.sh (Windows) / build-opencv-static-darwin.sh (macOS)
# installs. SOURCE this file, do not execute it:
#
#     source ./opencv-env.sh
#
# Every build and test entry point goes through here -- build-darwin.sh,
# build-windows.sh, package-windows.sh and the Makefile -- so there is exactly
# one definition of how this project links OpenCV, and builds and tests can
# never disagree about it.
#
# The vendored gocv fork (third_party/gocv/cgo.go) declares no #cgo directives,
# so cgo takes the include and link flags purely from the CGO_* variables
# exported below. That is why there are no build tags for linking.
#
# Inputs (optional):
#   ARCH                   target arch, macOS only (defaults to the host's)
#   OPENCV_STATIC_PREFIX   override the OpenCV install prefix
#
# Assumes the caller has run `set -euo pipefail`; returns non-zero on a missing
# toolchain or OpenCV.

OPENCV_VERSION="5.0.0"

case "$(uname -s)" in
  Darwin) _OCV_OS=darwin ;;
  *)      _OCV_OS=windows ;;   # Git Bash / MSYS2
esac

# --- Locate the static OpenCV ----------------------------------------------
if [ "$_OCV_OS" = darwin ]; then
  # Arch-suffixed prefix: an arm64 and an x86_64 build coexist. Must match
  # build-opencv-static-darwin.sh's PREFIX default.
  ARCH="${ARCH:-$(uname -m)}"
  OPENCV_STATIC_PREFIX="${OPENCV_STATIC_PREFIX:-$HOME/opencv-static/$OPENCV_VERSION-$ARCH}"
  _OCV_BUILD_CMD="ARCH=$ARCH ./build-opencv-static-darwin.sh"
else
  OPENCV_STATIC_PREFIX="${OPENCV_STATIC_PREFIX:-$HOME/opencv-static/$OPENCV_VERSION}"
  _OCV_BUILD_CMD="./build-opencv-static.sh"

  # --- MinGW toolchain (Windows only) ---------------------------------------
  # gocv needs OpenCV built with the same toolchain cgo uses, so MSVC binaries
  # do not work. Try `scoop prefix msys2`, the default scoop path, then C:\msys64.
  _MSYS_ROOT=""
  if command -v scoop >/dev/null 2>&1; then
    _MSYS_ROOT="$(scoop prefix msys2 2>/dev/null | tr -d '\r' || true)"
  fi
  for _cand in "$_MSYS_ROOT" "$HOME/scoop/apps/msys2/current" "/c/msys64"; do
    if [ -n "$_cand" ] && [ -x "$_cand/mingw64/bin/gcc.exe" ]; then
      _MINGW_UNIX="$_cand/mingw64"
      break
    fi
  done
  if [ -z "${_MINGW_UNIX:-}" ]; then
    echo "mingw64 gcc not found. Install MSYS2 + the mingw toolchain (see README)." >&2
    return 1
  fi
  # Prepend mingw64/bin so cgo uses MSYS2's gcc.
  export PATH="$_MINGW_UNIX/bin:$PATH"
fi

# OpenCV 5 installs opencv5.pc; on Windows it lands under
# x64/mingw/staticlib/pkgconfig, so search the tree rather than guessing.
# `|| true` is required, not decorative: find exits non-zero when the prefix does
# not exist, and under `set -o pipefail` that failure propagates through head and
# `set -e` kills the caller at this assignment -- before the message below can
# explain what is wrong.
_PC_FILE="$(find "$OPENCV_STATIC_PREFIX" -name opencv5.pc 2>/dev/null | head -1 || true)"
if [ -z "$_PC_FILE" ]; then
  echo "No static OpenCV $OPENCV_VERSION: opencv5.pc not found under $OPENCV_STATIC_PREFIX" >&2
  echo >&2
  echo "Build it first (once, takes 20-40 min):" >&2
  echo "  $_OCV_BUILD_CMD" >&2
  if [ "$_OCV_OS" = darwin ]; then
    # Point at any other arch that IS built, since picking the wrong ARCH (or
    # omitting it) is the likely mistake.
    _OTHER="$(ls -d "$HOME/opencv-static/$OPENCV_VERSION-"* 2>/dev/null \
              | sed "s|.*$OPENCV_VERSION-||" | grep -v "^$ARCH\$" | tr '\n' ' ' || true)"
    if [ -n "${_OTHER// /}" ]; then
      echo >&2
      echo "Already built for: ${_OTHER% }" >&2
    fi
  fi
  return 1
fi

# --- cgo environment --------------------------------------------------------
export CGO_ENABLED=1
# OpenCV 5 requires C++17 (it is built with C++17 by default).
export CGO_CXXFLAGS="--std=c++17 -DNDEBUG"

# --dont-define-prefix: honor the absolute prefix baked into opencv5.pc. Without
# it pkgconf recomputes a wrong prefix from the deep staticlib/pkgconfig nesting
# on Windows.
#
# PKG_CONFIG_LIBDIR, not PKG_CONFIG_PATH: PATH only *prepends* to pkg-config's
# built-in search path, so a Homebrew opencv could still be found and linked
# dynamically, quietly defeating the point. LIBDIR replaces the search path, so
# the static OpenCV is the only candidate.
export PKG_CONFIG_LIBDIR="$(dirname "$_PC_FILE")"
_PKGCFG="pkg-config --dont-define-prefix"

export CGO_CPPFLAGS="$($_PKGCFG --cflags opencv5)"
_OPENCV_LIBS="$($_PKGCFG --static --libs opencv5)"

if [ "$_OCV_OS" = darwin ]; then
  # OpenCV's generated .pc emits two malformed flags on macOS that the linker
  # rejects; translate them to what clang actually wants:
  #   -lOpenGL.framework -> a framework, not a -l library (and nothing here
  #                         needs GL, so drop it along with its -L)
  #   -lIconv::Iconv     -> a raw CMake target name leaked into the .pc
  _OPENCV_LIBS="$(printf '%s' "$_OPENCV_LIBS" \
    | sed -E 's|-L/Applications/Xcode\.app[^ ]*Frameworks||g; s/-lOpenGL\.framework//g; s/-lIconv::Iconv/-liconv/g')"
  export CGO_LDFLAGS="$_OPENCV_LIBS"
  # Cross-compiling cgo needs the target arch passed to clang for both the
  # compile and the link; Go does not infer it from GOARCH.
  if [ "$ARCH" != "$(uname -m)" ]; then
    export CGO_CFLAGS="-arch $ARCH"
    export CGO_CXXFLAGS="$CGO_CXXFLAGS -arch $ARCH"
    export CGO_LDFLAGS="$CGO_LDFLAGS -arch $ARCH"
  fi
else
  # OpenCV's generated .pc has two Windows/MSVC artifacts that break mingw
  # linking, so sanitise them:
  #   -lRunTmChk.a : an MSVC runtime-check lib absent in mingw -> drop it (OpenCV
  #                  was built by this same gcc, so nothing actually needs it)
  #   -lntdll.a    : the .a suffix is invalid in an -l name -> -lntdll (which exists)
  _OPENCV_LIBS="$(printf '%s' "$_OPENCV_LIBS" | sed -E 's/ -lRunTmChk(\.a)?//g; s/ -lntdll\.a/ -lntdll/g')"
  # opencv5.pc omits a Win32 import lib the statically-linked OpenCV needs:
  #   comdlg32 - highgui window_w32.cpp save/open dialogs (Get{Save,Open}FileNameA)
  # It may or may not survive --gc-sections, so link it unconditionally; it
  # imports a system DLL, so the exe stays portable.
  #
  # DirectShow libs are NOT listed here. Webcam capture is pion/mediadevices, whose
  # camera_windows.go declares its own `#cgo LDFLAGS: -lstrmiids -lole32 -loleaut32
  # -lquartz`. This only works because the OpenCV build sets WITH_DSHOW=OFF:
  # OpenCV's cap_dshow.cpp defines the DirectShow IIDs itself and would collide
  # with strmiids ("multiple definition").
  #
  # -static -static-libgcc -static-libstdc++ pull the C/C++/pthread runtime into
  # the exe, so the only remaining imports are Windows system DLLs.
  export CGO_LDFLAGS="$_OPENCV_LIBS -lcomdlg32 -static -static-libgcc -static-libstdc++"
fi

# migrated_fynedo asserts that all UI mutations happen on the main goroutine or
# inside fyne.Do (see internal/ui/camera.go and holdbutton.go). It silences Fyne
# 2.8's migration warning and opts into the future default behaviour. This is
# the only build tag the project uses; linking needs none.
export GOFLAGS="-tags=migrated_fynedo${GOFLAGS:+ $GOFLAGS}"

echo "OpenCV: $($_PKGCFG --modversion opencv5) (STATIC${ARCH:+ $ARCH})  |  $(${CC:-cc} --version | head -1)"
