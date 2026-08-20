#!/usr/bin/env bash
# Build the checkin app on macOS as a self-contained binary, linking the slim
# STATIC OpenCV produced by build-opencv-static-darwin.sh.
#
# Why: `make checkin` links Homebrew's opencv@4 dylibs by absolute path, so the
# binary only runs on machines with the same Homebrew install (see README
# "Distribution"). This links OpenCV in statically instead, leaving only macOS
# system frameworks as dynamic dependencies -- the binary runs on a stock Mac.
#
# It also cross-builds: ARCH=x86_64 on an Apple Silicon machine produces a
# binary for Intel Macs. clang cross-compiles natively, so this is not a Rosetta
# build and runs at full speed.
#
# One-time setup:
#   brew install cmake pkg-config
#   ARCH=x86_64 ./build-opencv-static-darwin.sh    # ~20-40 min, once per arch
#
# Usage:
#   ./build-darwin.sh                 # -> ./checkin (native arch, static OpenCV)
#   ARCH=x86_64 ./build-darwin.sh     # -> ./checkin-x86_64 for Intel Macs
#   ./build-darwin.sh --run           # build, then launch (native only)
set -euo pipefail
cd "$(dirname "$0")"

RUN=0
for arg in "$@"; do
  case "$arg" in
    --run) RUN=1 ;;
    *) echo "unknown option: $arg" >&2; exit 2 ;;
  esac
done

[ "$(uname -s)" = "Darwin" ] || { echo "This script is macOS only." >&2; exit 1; }

HOST_ARCH="$(uname -m)"
ARCH="${ARCH:-$HOST_ARCH}"
case "$ARCH" in
  arm64|x86_64) ;;
  *) echo "unsupported ARCH: $ARCH (want arm64 or x86_64)" >&2; exit 2 ;;
esac

# Go's name for the same thing.
case "$ARCH" in
  arm64)  GOARCH="arm64" ;;
  x86_64) GOARCH="amd64" ;;
esac

# Output is arch-suffixed when cross-building so a foreign binary is never
# mistaken for a runnable local one.
OUT="checkin"
[ "$ARCH" = "$HOST_ARCH" ] || OUT="checkin-$ARCH"

# --- Locate the static OpenCV ----------------------------------------------
# Must match build-opencv-static-darwin.sh's PREFIX default.
OPENCV_VERSION="4.13.0"
OPENCV_STATIC_PREFIX="${OPENCV_STATIC_PREFIX:-$HOME/opencv-static/$OPENCV_VERSION-$ARCH}"
# `|| true` is required, not decorative: find exits non-zero when the prefix does
# not exist, and under `set -o pipefail` that failure propagates through head and
# `set -e` kills the script at this assignment -- before the message below can
# explain what is wrong. The result is an exit 1 with no output at all.
PC_FILE="$(find "$OPENCV_STATIC_PREFIX" -name opencv4.pc 2>/dev/null | head -1 || true)"
if [ -z "$PC_FILE" ]; then
  echo "No static OpenCV for $ARCH: opencv4.pc not found under $OPENCV_STATIC_PREFIX" >&2
  echo >&2
  echo "Build it first (once per arch, takes 20-40 min):" >&2
  echo "  ARCH=$ARCH ./build-opencv-static-darwin.sh" >&2
  # Point at any other arch that IS built, since picking the wrong ARCH (or
  # omitting it) is the likely mistake.
  OTHER="$(ls -d "$HOME/opencv-static/$OPENCV_VERSION-"* 2>/dev/null | sed "s|.*$OPENCV_VERSION-||" | grep -v "^$ARCH\$" | tr '\n' ' ' || true)"
  if [ -n "${OTHER// /}" ]; then
    echo >&2
    echo "Already built for: ${OTHER% }" >&2
    echo "To build the app for one of those instead:  ARCH=${OTHER%% *} $0" >&2
  fi
  exit 1
fi
# PKG_CONFIG_LIBDIR, not PKG_CONFIG_PATH: PATH only prepends to pkg-config's
# built-in search path, so a Homebrew opencv@4 could still be found and linked
# dynamically, quietly defeating the point of this script. LIBDIR replaces the
# search path, making the static OpenCV the only candidate.
export PKG_CONFIG_LIBDIR="$(dirname "$PC_FILE")"

# --- cgo environment --------------------------------------------------------
# The opencvstatic build tag selects third_party/gocv/cgo_static_darwin.go,
# which is a single `#cgo pkg-config: --static opencv4` -- so the whole link
# line comes from the opencv4.pc found above. No CGO_LDFLAGS surgery is needed
# here (unlike the Windows build, which must sanitise MSVC artifacts out of its
# generated .pc).
export CGO_ENABLED=1
export CGO_CXXFLAGS="--std=c++11 -DNDEBUG"
# Cross-compiling cgo needs the target arch passed to clang for both the
# compile and the link; Go does not infer it from GOARCH.
if [ "$ARCH" != "$HOST_ARCH" ]; then
  export CGO_CFLAGS="-arch $ARCH"
  export CGO_CXXFLAGS="$CGO_CXXFLAGS -arch $ARCH"
  export CGO_LDFLAGS="-arch $ARCH"
fi

echo "OpenCV: $(pkg-config --modversion opencv4) (STATIC $ARCH)  |  $(cc --version | head -1)"

# opencvstatic:    link the static OpenCV via pkg-config --static
# migrated_fynedo: opts into Fyne 2.8's future main-goroutine behaviour
#                  (same tag the Makefile uses)
TAGS="opencvstatic,migrated_fynedo"

# App version. Single source of truth is the Makefile's VERSION; keep this
# default in sync. Override with `VERSION=x.y.z ./build-darwin.sh`.
VERSION="${VERSION:-0.0.3}"
# -X stamps the in-app version (About window, --version, startup log).
# -s -w drop the symbol table and DWARF: this is a distributable, not a debug
# target. -no_warn_duplicate_libraries silences the harmless repeated -lobjc
# the Fyne/mediadevices frameworks produce (same flag the Makefile uses).
LDFLAGS="-s -w -X github.com/pglass/checkin/internal/version.Version=$VERSION"
LDFLAGS="$LDFLAGS -extldflags=-Wl,-no_warn_duplicate_libraries"

echo "Building $OUT (GOARCH=$GOARCH, tags: $TAGS)"
GOARCH="$GOARCH" go build -tags "$TAGS" -ldflags "$LDFLAGS" -o "$OUT" ./cmd/checkin/

# --- Verify -----------------------------------------------------------------
echo
file "$OUT"
# The point of the exercise: no OpenCV dylibs in the load commands. Anything
# under /opt/homebrew or /usr/local means a dylib leaked into the link and the
# binary will not run on a machine without that Homebrew install.
echo "Non-system dynamic dependencies:"
if otool -L "$OUT" | tail -n +2 | grep -Ev '/usr/lib/|/System/Library/' | grep -q .; then
  otool -L "$OUT" | tail -n +2 | grep -Ev '/usr/lib/|/System/Library/'
  echo "WARNING: the binary links non-system libraries; it is not self-contained." >&2
else
  echo "  (none -- only system libraries and frameworks)"
fi

if [ "$RUN" -eq 1 ]; then
  if [ "$ARCH" != "$HOST_ARCH" ]; then
    echo "Not running: $OUT is $ARCH, this machine is $HOST_ARCH." >&2
    exit 0
  fi
  exec "./$OUT" -log-level DEBUG -log-file -
fi
