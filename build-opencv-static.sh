#!/usr/bin/env bash
# Build a slim, STATIC OpenCV 4.13.0 for linking checkin.exe into a single
# self-contained executable (no bundled DLLs). Run from Git Bash on Windows with
# MSYS2 + the mingw toolchain installed (same prerequisites as build-windows.sh).
#
# Why a custom build: MSYS2's prebuilt OpenCV is shared-only and pulls in ~166 MB
# of third-party DLLs this app never uses (Qt6/ICU, ffmpeg codecs, OpenBLAS). This
# build drops those but keeps every OpenCV *module* gocv's wrappers reference, so
# vanilla (unmodified) gocv still links via its `customenv` build tag. See
# build-windows.sh --static and the plan for the full rationale.
#
# Pinned to the OpenCV version gocv v0.43.0 targets. To update: bump OPENCV_VERSION
# to the version the installed gocv release targets (check its README), delete the
# old prefix, and re-run.
#
# Output: a static install at $PREFIX (default ~/opencv-static/4.13.0) containing
# lib/libopencv_*.a and lib/pkgconfig/opencv4.pc.
#
# Usage:
#   ./build-opencv-static.sh                 # download (if needed), configure, build, install
#   PREFIX=/c/opt/opencv-static ./build-opencv-static.sh
#   ./build-opencv-static.sh --clean         # wipe the build dir first (fresh configure)
set -euo pipefail
cd "$(dirname "$0")"

OPENCV_VERSION="4.13.0"
CLEAN=0
for arg in "$@"; do
  case "$arg" in
    --clean) CLEAN=1 ;;
    *) echo "unknown option: $arg" >&2; exit 2 ;;
  esac
done

# Where sources are unpacked and the result installed. Kept out of the git repo.
WORK="${WORK:-$HOME/opencv-static}"
PREFIX="${PREFIX:-$WORK/$OPENCV_VERSION}"
SRC="$WORK/opencv-$OPENCV_VERSION"
BUILD="$WORK/build-$OPENCV_VERSION"

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
# Put mingw64/bin first so cmake uses MSYS2's gcc/g++/windres, not any other.
export PATH="$MINGW_UNIX/bin:$PATH"

# cmake + a generator are required. Prefer Ninja (fast); fall back to MinGW make.
if ! command -v cmake >/dev/null 2>&1; then
  echo "cmake not found. Install it in MSYS2:  pacman -S mingw-w64-x86_64-cmake" >&2
  exit 1
fi
if command -v ninja >/dev/null 2>&1; then
  GENERATOR="Ninja"
elif command -v mingw32-make >/dev/null 2>&1; then
  GENERATOR="MinGW Makefiles"
else
  echo "No build generator. Install:  pacman -S mingw-w64-x86_64-ninja  (or mingw-w64-x86_64-make)" >&2
  exit 1
fi
echo "Toolchain: $(gcc --version | head -1)  |  generator: $GENERATOR"

# --- Fetch + unpack OpenCV source (skip if already present) -----------------
mkdir -p "$WORK"
if [ ! -d "$SRC" ]; then
  ZIP="$WORK/opencv-$OPENCV_VERSION.zip"
  URL="https://github.com/opencv/opencv/archive/$OPENCV_VERSION.zip"
  echo "Downloading $URL"
  curl -fL "$URL" -o "$ZIP"
  echo "Extracting..."
  # Expand-Archive is reliable for GitHub's zip; MSYS tar can't read zip.
  powershell.exe -NoProfile -NonInteractive -Command \
    "Expand-Archive -Path '$(cygpath -w "$ZIP")' -DestinationPath '$(cygpath -w "$WORK")' -Force"
  rm -f "$ZIP"
fi
[ -f "$SRC/CMakeLists.txt" ] || { echo "OpenCV source missing at $SRC" >&2; exit 1; }

# --- Configure --------------------------------------------------------------
# BUILD_LIST includes every module any gocv wrapper .cpp references, so vanilla
# gocv links with no undefined symbols. The WITH_*/BUILD_* toggles drop the three
# heavy dependency groups (Qt/ICU, ffmpeg codecs, OpenBLAS).
#
# Two toggles match MSYS2's prebuilt OpenCV (so this static build behaves like the
# shared build the app was validated against -- verified via its cvconfig.h):
#   WITH_MSMF=OFF : the MSMF camera backend does not compile with MinGW GCC (known
#                   ComPtr/IID template error); MSYS2 disables it too. DSHOW is the
#                   webcam backend in both builds.
#   WITH_IPP=OFF  : Intel ships IPPICV only as an MSVC .lib that mingw ld cannot
#                   link; MSYS2's build has HAVE_IPP undefined as well, so there is
#                   no acceleration difference vs the validated build.
[ "$CLEAN" -eq 1 ] && rm -rf "$BUILD"
cmake -S "$SRC" -B "$BUILD" -G "$GENERATOR" \
  -DCMAKE_BUILD_TYPE=Release \
  -DCMAKE_INSTALL_PREFIX="$(cygpath -m "$PREFIX")" \
  -DBUILD_SHARED_LIBS=OFF \
  -DBUILD_LIST=core,imgproc,imgcodecs,videoio,objdetect,dnn,calib3d,features2d,flann,photo,video,highgui \
  -DWITH_QT=OFF -DWITH_GTK=OFF -DWITH_WIN32UI=ON \
  -DWITH_FFMPEG=OFF \
  -DWITH_GSTREAMER=OFF -DWITH_GPHOTO2=OFF -DWITH_1394=OFF \
  -DWITH_FREETYPE=OFF -DWITH_GDAL=OFF -DWITH_GDCM=OFF -DWITH_VA=OFF -DWITH_VA_INTEL=OFF \
  -DWITH_MSMF=OFF -DWITH_DSHOW=ON \
  -DWITH_IPP=OFF \
  -DWITH_LAPACK=OFF \
  -DWITH_PROTOBUF=ON -DBUILD_PROTOBUF=ON \
  -DWITH_PNG=ON -DBUILD_PNG=ON -DBUILD_ZLIB=ON \
  -DWITH_JPEG=ON -DBUILD_JPEG=ON \
  -DWITH_TIFF=OFF -DWITH_WEBP=OFF -DWITH_OPENJPEG=OFF -DWITH_JASPER=OFF -DWITH_OPENEXR=OFF \
  -DBUILD_EXAMPLES=OFF -DBUILD_TESTS=OFF -DBUILD_PERF_TESTS=OFF -DBUILD_DOCS=OFF \
  -DBUILD_opencv_apps=OFF -DBUILD_opencv_python3=OFF -DBUILD_opencv_java=OFF \
  -DENABLE_PRECOMPILED_HEADERS=OFF \
  -DOPENCV_ALLOCATOR_STATS_COUNTER_TYPE=int64_t \
  -DOPENCV_GENERATE_PKGCONFIG=ON \
  -Wno-dev

# --- Build + install --------------------------------------------------------
JOBS="$(nproc 2>/dev/null || echo 4)"
cmake --build "$BUILD" --target install --parallel "$JOBS"

# --- Verify -----------------------------------------------------------------
# On Windows OpenCV installs to $PREFIX/x64/mingw/staticlib, so search the tree.
# A real static archive is libopencv_core*.a (NOT a .dll.a import stub).
CORE_A="$(find "$PREFIX" -name 'libopencv_core*.a' ! -name '*.dll.a' 2>/dev/null | head -1)"
if [ -z "$CORE_A" ]; then
  echo "FAILED: no static libopencv_core*.a found under $PREFIX" >&2
  exit 1
fi
if find "$PREFIX" -name 'libopencv_core*.dll.a' 2>/dev/null | grep -q .; then
  echo "WARNING: found .dll.a import stubs -- this looks like a SHARED build, not static." >&2
fi
PC="$(find "$PREFIX" -name opencv4.pc 2>/dev/null | head -1)"
[ -n "$PC" ] && [ -f "$PC" ] || { echo "FAILED: opencv4.pc not generated under $PREFIX" >&2; exit 1; }

echo
echo "Static OpenCV $OPENCV_VERSION installed at: $PREFIX"
echo "pkg-config file: $PC"
echo "Next: build the single-exe with  ./build-windows.sh --static"
