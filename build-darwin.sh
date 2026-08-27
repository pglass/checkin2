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

# --- OpenCV + cgo environment ----------------------------------------------
# One shared definition of how this project links OpenCV, used by every build
# and test entry point. Exports CGO_* (and GOFLAGS for the one Fyne build tag),
# and handles the ARCH cross-compile flags.
source "$(dirname "$0")/opencv-env.sh"


# App version. Single source of truth is the Makefile's VERSION; keep this
# default in sync. Override with `VERSION=x.y.z ./build-darwin.sh`.
VERSION="${VERSION:-0.0.6}"
# -X stamps the in-app version (About window, --version, startup log).
# -s -w drop the symbol table and DWARF: this is a distributable, not a debug
# target. -no_warn_duplicate_libraries silences the harmless repeated -lobjc
# the Fyne/mediadevices frameworks produce (same flag the Makefile uses).
LDFLAGS="-s -w -X github.com/pglass/checkin/internal/version.Version=$VERSION"
LDFLAGS="$LDFLAGS -extldflags=-Wl,-no_warn_duplicate_libraries"

echo "Building $OUT (GOARCH=$GOARCH)"
GOARCH="$GOARCH" go build -ldflags "$LDFLAGS" -o "$OUT" ./cmd/checkin/

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
