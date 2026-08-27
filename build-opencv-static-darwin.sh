#!/usr/bin/env bash
# Build a slim, STATIC OpenCV 5.0.0 for linking the checkin app into a
# self-contained macOS binary (no OpenCV dylibs to bundle or rewrite).
#
# macOS counterpart of build-opencv-static.sh (which is MSYS2/Windows only).
# Same module list and same trimming rationale -- see that script's comments for
# why these six modules and no more. The differences here are all platform:
#   - clang instead of mingw gcc, so no MSYS2 discovery
#   - unzip/tar instead of powershell Expand-Archive, no cygpath
#   - sysctl -n hw.ncpu instead of nproc
#   - WITH_WIN32UI/DSHOW/MSMF are meaningless; WITH_AVFOUNDATION stays OFF
#     because capture is pion/mediadevices, not OpenCV videoio
#   - CMAKE_OSX_ARCHITECTURES selects the target arch, so one script builds
#     either a native arm64 OpenCV or an x86_64 one (for Intel Macs) under
#     Rosetta-free cross-compilation: clang targets x86_64 without Rosetta.
#
# Output: a static install at $PREFIX containing lib/libopencv_*.a and
# lib/pkgconfig/opencv5.pc. The prefix is arch-suffixed so an arm64 and an
# x86_64 build can coexist.
#
# Usage:
#   ./build-opencv-static-darwin.sh                  # native arch
#   ARCH=x86_64 ./build-opencv-static-darwin.sh      # for Intel Macs
#   ./build-opencv-static-darwin.sh --clean          # wipe build dir + prefix first
set -euo pipefail
cd "$(dirname "$0")"

OPENCV_VERSION="5.0.0"
CLEAN=0
for arg in "$@"; do
  case "$arg" in
    --clean) CLEAN=1 ;;
    *) echo "unknown option: $arg" >&2; exit 2 ;;
  esac
done

[ "$(uname -s)" = "Darwin" ] || { echo "This script is macOS only. On Windows use build-opencv-static.sh." >&2; exit 1; }

# Target architecture. Defaults to the host's; set ARCH=x86_64 on an Apple
# Silicon machine to produce libraries for an Intel Mac.
ARCH="${ARCH:-$(uname -m)}"
case "$ARCH" in
  arm64|x86_64) ;;
  *) echo "unsupported ARCH: $ARCH (want arm64 or x86_64)" >&2; exit 2 ;;
esac

# Kept out of the git repo, and arch-suffixed so both arches can coexist.
WORK="${WORK:-$HOME/opencv-static}"
PREFIX="${PREFIX:-$WORK/$OPENCV_VERSION-$ARCH}"
SRC="$WORK/opencv-$OPENCV_VERSION"
BUILD="$WORK/build-$OPENCV_VERSION-$ARCH"

command -v cmake >/dev/null 2>&1 || { echo "cmake not found. Install it:  brew install cmake" >&2; exit 1; }
if command -v ninja >/dev/null 2>&1; then
  GENERATOR="Ninja"
else
  GENERATOR="Unix Makefiles"
fi
echo "Toolchain: $(cc --version | head -1)  |  arch: $ARCH  |  generator: $GENERATOR"

# --- Fetch + unpack OpenCV source (skip if already present) -----------------
# The source tree is arch-independent, so both arches share one unpack.
mkdir -p "$WORK"
if [ ! -d "$SRC" ]; then
  ZIP="$WORK/opencv-$OPENCV_VERSION.zip"
  URL="https://github.com/opencv/opencv/archive/$OPENCV_VERSION.zip"
  echo "Downloading $URL"
  curl -fL "$URL" -o "$ZIP"
  echo "Extracting..."
  # macOS ships bsdtar, which reads zip natively (unlike MSYS tar).
  tar -xf "$ZIP" -C "$WORK"
  rm -f "$ZIP"
fi
[ -f "$SRC/CMakeLists.txt" ] || { echo "OpenCV source missing at $SRC" >&2; exit 1; }

# --- Configure --------------------------------------------------------------
# BUILD_LIST and the WITH_*/BUILD_* toggles are deliberately identical to
# build-opencv-static.sh: the trimmed gocv fork at third_party/gocv references
# only core, imgproc and objdetect wrappers, and objdetect pulls in features ->
# geometry, flann. Everything else (dnn and its protobuf especially) is
# dropped. Keep the two scripts' module lists in sync.
#
# CMAKE_OSX_ARCHITECTURES makes clang emit $ARCH objects; combined with
# CMAKE_OSX_DEPLOYMENT_TARGET it is a genuine cross-compile, not a Rosetta
# build, so it runs at full speed on Apple Silicon.
#
# CMAKE_SYSTEM_PROCESSOR must be set too: OpenCV keys its SIMD/HAL decisions off
# the *host* processor otherwise. Cross-building x86_64 on an arm64 Mac without
# it enables KleidiCV (an ARM NEON HAL), whose sources are then compiled with
# `-mcpu=armv8-a` against an x86_64 target and fail with "unknown target CPU
# 'armv8-a'". WITH_KLEIDICV=OFF belts-and-braces the same thing.
[ "$CLEAN" -eq 1 ] && rm -rf "$BUILD" "$PREFIX"
cmake -S "$SRC" -B "$BUILD" -G "$GENERATOR" \
  -DCMAKE_BUILD_TYPE=Release \
  -DCMAKE_INSTALL_PREFIX="$PREFIX" \
  -DCMAKE_OSX_ARCHITECTURES="$ARCH" \
  -DCMAKE_SYSTEM_PROCESSOR="$ARCH" \
  -DCMAKE_OSX_DEPLOYMENT_TARGET=11.0 \
  -DWITH_KLEIDICV=OFF \
  -DWITH_CAROTENE=OFF \
  -DBUILD_SHARED_LIBS=OFF \
  -DBUILD_LIST=core,imgproc,geometry,features,flann,objdetect \
  -DWITH_QT=OFF -DWITH_GTK=OFF \
  -DWITH_FFMPEG=OFF \
  -DWITH_GSTREAMER=OFF -DWITH_GPHOTO2=OFF -DWITH_1394=OFF \
  -DWITH_FREETYPE=OFF -DWITH_GDAL=OFF -DWITH_GDCM=OFF -DWITH_VA=OFF -DWITH_VA_INTEL=OFF \
  -DWITH_V4L=OFF -DWITH_AVFOUNDATION=OFF \
  -DWITH_IPP=OFF \
  -DWITH_LAPACK=OFF \
  -DWITH_OPENCL=OFF \
  -DWITH_PROTOBUF=OFF -DBUILD_PROTOBUF=OFF \
  -DWITH_PNG=OFF -DWITH_JPEG=OFF -DWITH_TIFF=OFF -DWITH_WEBP=OFF \
  -DWITH_OPENJPEG=OFF -DWITH_JASPER=OFF -DWITH_OPENEXR=OFF \
  -DBUILD_EXAMPLES=OFF -DBUILD_TESTS=OFF -DBUILD_PERF_TESTS=OFF -DBUILD_DOCS=OFF \
  -DBUILD_opencv_apps=OFF -DBUILD_opencv_python3=OFF -DBUILD_opencv_java=OFF \
  -DENABLE_PRECOMPILED_HEADERS=OFF \
  -DOPENCV_ALLOCATOR_STATS_COUNTER_TYPE=int64_t \
  -DOPENCV_GENERATE_PKGCONFIG=ON \
  -Wno-dev

# --- Build + install --------------------------------------------------------
JOBS="$(sysctl -n hw.ncpu 2>/dev/null || echo 4)"
cmake --build "$BUILD" --target install --parallel "$JOBS"

# --- Verify -----------------------------------------------------------------
# A real static archive is libopencv_core*.a; a .dylib here means the static
# switch did not take.
# || true: without it, a find failure (missing/partial install) is fatal under
# set -o pipefail before the message below can explain what went wrong.
CORE_A="$(find "$PREFIX" -name 'libopencv_core*.a' 2>/dev/null | head -1 || true)"
if [ -z "$CORE_A" ]; then
  echo "FAILED: no static libopencv_core*.a found under $PREFIX" >&2
  exit 1
fi
if find "$PREFIX" -name 'libopencv_core*.dylib' 2>/dev/null | grep -q .; then
  echo "WARNING: found .dylib files -- this looks like a SHARED build, not static." >&2
fi
# The archive must actually be the requested architecture, or the app link will
# fail late with "found architecture ..., required architecture ...".
if ! lipo -info "$CORE_A" 2>/dev/null | grep -q "$ARCH"; then
  echo "FAILED: $CORE_A is not $ARCH: $(lipo -info "$CORE_A" 2>&1)" >&2
  exit 1
fi
PC="$(find "$PREFIX" -name opencv5.pc 2>/dev/null | head -1 || true)"
[ -n "$PC" ] && [ -f "$PC" ] || { echo "FAILED: opencv5.pc not generated under $PREFIX" >&2; exit 1; }

# --- Fix the generated .pc --------------------------------------------------
# OpenCV's pkg-config generator mishandles macOS frameworks: it emits Apple
# frameworks as `-lFoo.framework`, which is not a valid linker flag, and the app
# link dies with "ld: library 'OpenGL.framework' not found". Rewrite those into
# proper `-framework Foo` pairs. (The Windows script has the same class of fixup
# for its MSVC artifacts, applied at build time instead -- here it is done once,
# in the .pc, so every consumer of this prefix gets the corrected flags.)
#
# The accompanying -L into the SDK Frameworks directory is harmless but useless
# once the -l form is gone; it is left alone to keep the edit minimal.
if grep -q -- '-l[A-Za-z0-9_]*\.framework' "$PC"; then
  # BSD sed (macOS) needs the -i backup-suffix argument, hence -i ''.
  sed -i '' -E 's/-l([A-Za-z0-9_]+)\.framework/-framework \1/g' "$PC"
  echo "Patched $PC: rewrote -lFoo.framework as -framework Foo"
fi
# Guard against a future OpenCV emitting a form this does not catch.
if grep -q -- '\.framework' "$PC"; then
  echo "WARNING: $PC still mentions .framework; check the Libs lines by hand." >&2
fi

echo
echo "Static OpenCV $OPENCV_VERSION ($ARCH) installed at: $PREFIX"
echo "pkg-config file: $PC"
echo "Next: build the app with  ARCH=$ARCH ./build-darwin.sh"
