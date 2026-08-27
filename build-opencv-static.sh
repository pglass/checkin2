#!/usr/bin/env bash
# Build a slim, STATIC OpenCV 5.0.0 for linking checkin.exe into a single
# self-contained executable (no bundled DLLs).
#
# Runs two ways, picked automatically from the host OS:
#   - natively on Windows, from Git Bash with MSYS2 + the mingw toolchain
#     (same prerequisites as build-windows.sh)
#   - cross-compiled from macOS/Linux with the mingw-w64 cross toolchain
#     (`brew install mingw-w64`), which is how build-windows.sh cross-builds
#
# Why a custom build: MSYS2's prebuilt OpenCV is shared-only and pulls in ~166 MB
# of third-party DLLs this app never uses (Qt6/ICU, ffmpeg codecs, OpenBLAS). This
# build drops those but keeps every OpenCV *module* gocv's wrappers reference, so
# the vendored gocv fork links via ./opencv-env.sh. See
# build-windows.sh and the plan for the full rationale.
#
# Pinned to the OpenCV version gocv v0.43.0 targets. To update: bump OPENCV_VERSION
# to the version the installed gocv release targets (check its README), delete the
# old prefix, and re-run.
#
# Output: a static install at $PREFIX containing lib/libopencv_*.a and
# lib/pkgconfig/opencv5.pc. Default prefix is ~/opencv-static/5.0.0 natively, and
# ~/opencv-static/5.0.0-windows when cross-compiling, so a macOS host can hold
# both its native OpenCV and the Windows one without collision.
#
# Usage:
#   ./build-opencv-static.sh                 # download (if needed), configure, build, install
#   PREFIX=/c/opt/opencv-static ./build-opencv-static.sh
#   ./build-opencv-static.sh --clean         # wipe the build dir first (fresh configure)
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

# Where sources are unpacked and the result installed. Kept out of the git repo.
WORK="${WORK:-$HOME/opencv-static}"
SRC="$WORK/opencv-$OPENCV_VERSION"

# --- Toolchain: native MSYS2, or mingw-w64 cross from macOS/Linux -----------
# CROSS=1 means we are not on Windows and must drive cmake through a toolchain
# file naming the x86_64-w64-mingw32-* compilers.
CROSS=0
[ "$(uname -s)" = "Darwin" ] || [ "$(uname -s)" = "Linux" ] && CROSS=1

if [ "$CROSS" -eq 1 ]; then
  command -v x86_64-w64-mingw32-g++ >/dev/null 2>&1 || {
    echo "mingw-w64 cross toolchain not found. Install it:" >&2
    echo "  brew install mingw-w64        # macOS" >&2
    echo "  apt install mingw-w64         # Debian/Ubuntu" >&2
    exit 1
  }
  # Separate prefix so a macOS host can keep its native OpenCV alongside this one.
  PREFIX="${PREFIX:-$WORK/$OPENCV_VERSION-windows}"
  BUILD="$WORK/build-$OPENCV_VERSION-windows"
  echo "Toolchain: $(x86_64-w64-mingw32-g++ --version | head -1)  (cross -> windows/amd64)"
else
  PREFIX="${PREFIX:-$WORK/$OPENCV_VERSION}"
  BUILD="$WORK/build-$OPENCV_VERSION"
  # --- Locate MSYS2 / mingw64 (same discovery as build-windows.sh) ----------
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
fi

# cmake + a generator are required. Prefer Ninja (fast); fall back to MinGW make.
if ! command -v cmake >/dev/null 2>&1; then
  echo "cmake not found. Install it in MSYS2:  pacman -S mingw-w64-x86_64-cmake" >&2
  exit 1
fi
if command -v ninja >/dev/null 2>&1; then
  GENERATOR="Ninja"
elif [ "$CROSS" -eq 1 ]; then
  GENERATOR="Unix Makefiles"
elif command -v mingw32-make >/dev/null 2>&1; then
  GENERATOR="MinGW Makefiles"
else
  echo "No build generator. Install:  pacman -S mingw-w64-x86_64-ninja  (or mingw-w64-x86_64-make)" >&2
  exit 1
fi
[ "$CROSS" -eq 1 ] || echo "Toolchain: $(gcc --version | head -1)"
echo "Generator: $GENERATOR"

# When cross-compiling, cmake needs a toolchain file naming the target compilers.
# CMAKE_FIND_ROOT_PATH_MODE_* keep it from picking up host libraries/headers.
TOOLCHAIN_ARGS=()
if [ "$CROSS" -eq 1 ]; then
  TC="$WORK/mingw-w64-toolchain.cmake"
  cat > "$TC" <<'CMAKE'
set(CMAKE_SYSTEM_NAME Windows)
set(CMAKE_SYSTEM_PROCESSOR x86_64)
set(CMAKE_C_COMPILER   x86_64-w64-mingw32-gcc)
set(CMAKE_CXX_COMPILER x86_64-w64-mingw32-g++)
set(CMAKE_RC_COMPILER  x86_64-w64-mingw32-windres)
set(CMAKE_FIND_ROOT_PATH_MODE_PROGRAM NEVER)
set(CMAKE_FIND_ROOT_PATH_MODE_LIBRARY ONLY)
set(CMAKE_FIND_ROOT_PATH_MODE_INCLUDE ONLY)
CMAKE
  TOOLCHAIN_ARGS=(-DCMAKE_TOOLCHAIN_FILE="$TC")
fi

# --- Fetch + unpack OpenCV source (skip if already present) -----------------
mkdir -p "$WORK"
if [ ! -d "$SRC" ]; then
  ZIP="$WORK/opencv-$OPENCV_VERSION.zip"
  URL="https://github.com/opencv/opencv/archive/$OPENCV_VERSION.zip"
  echo "Downloading $URL"
  curl -fL "$URL" -o "$ZIP"
  echo "Extracting..."
  if [ "$CROSS" -eq 1 ]; then
    # bsdtar (macOS) and GNU tar (Linux) both read zip natively.
    tar -xf "$ZIP" -C "$WORK"
  else
    # Expand-Archive is reliable for GitHub's zip; MSYS tar can't read zip.
    powershell.exe -NoProfile -NonInteractive -Command \
      "Expand-Archive -Path '$(cygpath -w "$ZIP")' -DestinationPath '$(cygpath -w "$WORK")' -Force"
  fi
  rm -f "$ZIP"
fi
[ -f "$SRC/CMakeLists.txt" ] || { echo "OpenCV source missing at $SRC" >&2; exit 1; }

# --- Configure --------------------------------------------------------------
# BUILD_LIST is the minimal set of OpenCV modules the app's (trimmed) gocv fork
# links. The app only uses gocv's Mat/imgproc helpers and QRCodeDetector, so the
# fork at third_party/gocv keeps just the core, imgproc and objdetect wrappers and
# deletes the rest (dnn, video, photo, videoio, imgcodecs, highgui, calib,
# stereo, ptcloud, ...). With those wrappers gone, nothing references those
# OpenCV modules, so they are dropped here too -- most importantly dnn (~20 MB) and
# its protobuf dependency (~5 MB). The WITH_*/BUILD_* toggles drop the heavy
# dependency groups (Qt/ICU, ffmpeg codecs, OpenBLAS).
#
# Why these six: objdetect (QRCodeDetectorAruco) requires core, imgproc,
# features and geometry; geometry in turn requires flann. objdetect is OPTIONAL-linked
# against dnn -- without it, cv::FaceDetectorYN::create is still defined but
# CV_Error()s at runtime (see modules/objdetect/src/face_detect.cpp), so the
# gocv objdetect wrapper still links even though we never call those APIs.
#
# No videoio: capture moved to pion/mediadevices (its own DirectShow cgo), and the
# gocv videoio wrapper is deleted, so OpenCV's videoio -- and its cap_dshow.cpp,
# which used to collide with pion's DirectShow symbols -- is never built. That
# retires the old WITH_DSHOW=OFF workaround entirely.
#
# Remaining backend/accel toggles:
#   WITH_IPP=OFF  : Intel ships IPPICV only as an MSVC .lib that mingw ld cannot
#                   link; MSYS2's build has HAVE_IPP undefined as well, so there is
#                   no acceleration difference vs the validated build.
# --clean also wipes the install PREFIX, not just the build tree: when BUILD_LIST
# shrinks, `install` adds the new modules but never removes the .a files from a
# previous (larger) build, leaving stale libs (e.g. dnn) behind. Wiping the prefix
# guarantees the install reflects exactly what was built this run.
[ "$CLEAN" -eq 1 ] && rm -rf "$BUILD" "$PREFIX"
# cygpath -m converts the MSYS path to the mixed C:/... form cmake wants; it
# does not exist when cross-compiling, where the path is already native.
if [ "$CROSS" -eq 1 ]; then CMAKE_PREFIX_ARG="$PREFIX"; else CMAKE_PREFIX_ARG="$(cygpath -m "$PREFIX")"; fi

cmake -S "$SRC" -B "$BUILD" -G "$GENERATOR" ${TOOLCHAIN_ARGS[@]+"${TOOLCHAIN_ARGS[@]}"} \
  -DCMAKE_BUILD_TYPE=Release \
  -DCMAKE_INSTALL_PREFIX="$CMAKE_PREFIX_ARG" \
  -DBUILD_SHARED_LIBS=OFF \
  -DBUILD_LIST=core,imgproc,geometry,features,flann,objdetect \
  -DWITH_QT=OFF -DWITH_GTK=OFF -DWITH_WIN32UI=ON \
  -DWITH_FFMPEG=OFF \
  -DWITH_GSTREAMER=OFF -DWITH_GPHOTO2=OFF -DWITH_1394=OFF \
  -DWITH_FREETYPE=OFF -DWITH_GDAL=OFF -DWITH_GDCM=OFF -DWITH_VA=OFF -DWITH_VA_INTEL=OFF \
  -DWITH_MSMF=OFF -DWITH_DSHOW=OFF \
  -DWITH_V4L=OFF -DWITH_AVFOUNDATION=OFF \
  -DWITH_IPP=OFF \
  -DWITH_LAPACK=OFF \
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
JOBS="$(nproc 2>/dev/null || sysctl -n hw.ncpu 2>/dev/null || echo 4)"
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
PC="$(find "$PREFIX" -name opencv5.pc 2>/dev/null | head -1)"
[ -n "$PC" ] && [ -f "$PC" ] || { echo "FAILED: opencv5.pc not generated under $PREFIX" >&2; exit 1; }

echo
echo "Static OpenCV $OPENCV_VERSION installed at: $PREFIX"
echo "pkg-config file: $PC"
echo "Next: build the single-exe with  ./build-windows.sh"
