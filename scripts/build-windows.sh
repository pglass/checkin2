#!/usr/bin/env bash
# Build checkin.exe as a single, self-contained static executable.
#
# Runs two ways, picked automatically from the host OS:
#   - natively on Windows, from Git Bash / MSYS2
#   - cross-compiled from macOS/Linux via the mingw-w64 cross toolchain
#     (`brew install mingw-w64`), so a release exe can be produced without a
#     Windows machine. Cross-built exes cannot be smoke-tested here; --run is
#     rejected in that mode.
#
# The exe is also verified self-contained and copied to dist/ as a versioned
# release artifact. (This absorbed the old package-windows.sh, which only added
# those two steps around this script.)
#
# gocv needs OpenCV built with the same toolchain cgo uses (MinGW/GCC), so
# opencv.org's MSVC binaries and scoop's `opencv` (v5) do NOT work. This links a
# custom slim STATIC OpenCV 5.0.0 built by scripts/build-opencv-static.sh, feeding cgo
# the flags via ./scripts/opencv-env.sh.
# The result is one checkin.exe that depends only on Windows system DLLs -- no
# OpenCV/Qt/MinGW runtime DLLs to bundle.
#
# One-time setup, native Windows (see README "Prerequisites (Windows)"):
#   scoop install msys2
#   <msys2> pacman -S mingw-w64-x86_64-toolchain mingw-w64-x86_64-cmake \
#                     mingw-w64-x86_64-ninja      mingw-w64-x86_64-pkgconf
#   ./scripts/build-opencv-static.sh    # build the static OpenCV this script links
#
# Usage:
#   ./scripts/build-windows.sh            # -> ./checkin.exe + dist/checkin-$VERSION.exe
#   ./scripts/build-windows.sh --run      # build, then launch (native Windows only)
#   ./scripts/build-windows.sh --console  # keep the console window, for debugging
#
# The exe is built with -H=windowsgui by default: this is a kiosk app, and
# without it Windows opens a console box behind the UI on every launch. Pass
# --console when you need `-log-file -` (stdout logging) to be visible.
set -euo pipefail
# Scripts live in scripts/; every path below is relative to the repo root.
cd "$(dirname "$0")/.."

RUN=0
CONSOLE=0
for arg in "$@"; do
  case "$arg" in
    --run) RUN=1 ;;
    --console) CONSOLE=1 ;;
    *) echo "unknown option: $arg" >&2; exit 2 ;;
  esac
done

# --- MinGW toolchain + static-OpenCV cgo environment ------------------------
# Sets PATH/CGO_*/GOFLAGS to link the slim static OpenCV. Shared with every
# other build and test entry point, so they cannot disagree.
# TARGET_OS=windows makes scripts/opencv-env.sh select the mingw-w64 cross toolchain and
# the -windows OpenCV prefix when the host is not Windows; on Windows it is a
# no-op and the native MSYS2 toolchain is used.
export TARGET_OS=windows
source ./scripts/opencv-env.sh

if [ "$(uname -s)" != "MINGW"* ] && [ "$RUN" -eq 1 ] && [ "${GOOS:-}" = "windows" ] && [ "$(uname -s)" != "Windows_NT" ]; then
  case "$(uname -s)" in
    Darwin|Linux) echo "--run cannot launch a cross-built Windows exe from $(uname -s)." >&2; exit 2 ;;
  esac
fi

# App version. Single source of truth is the Makefile's VERSION; keep this
# default in sync. Override with `VERSION=x.y.z ./scripts/build-windows.sh`.
VERSION="${VERSION:-0.0.7}"
# -X stamps the in-app version (the About dialog, --version, and startup log).
# -s -w drop the Go symbol table and DWARF debug info: this is a self-contained
# distributable, not a debug target, and stripping them roughly halves the exe
# (~100 MB -> ~56 MB) with no runtime effect. The Makefile's dev build keeps them.
LDFLAGS="-s -w -X github.com/pglass/checkin/internal/version.Version=$VERSION"
# -H=windowsgui suppresses the console window. Default on (kiosk app); --console
# turns it back on so stdout logging is visible while debugging.
[ "$CONSOLE" -eq 1 ] || LDFLAGS="$LDFLAGS -H=windowsgui"

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
# Two cross-compile hazards here:
#   - GOOS/GOARCH/CC are exported for the target, so they must be cleared or
#     `go run` builds goversioninfo itself as a Windows exe and cannot exec it.
#   - goversioninfo picks the .syso architecture from its own -64/-arm flags,
#     which BOTH default to true. On an arm64 host that yields an Aarch64 COFF
#     object, and the x86_64 linker rejects it with "file format not
#     recognized". -arm=false forces the pe-x86-64 object the exe needs.
if env -u GOOS -u GOARCH -u CC -u CXX -u CGO_LDFLAGS -u CGO_CPPFLAGS \
     go run github.com/josephspurrier/goversioninfo/cmd/goversioninfo \
     -64 -arm=false -o "$SYSO" "$VI_JSON"; then
  echo "Wrote versioninfo -> $SYSO (v$VERSION)"
else
  echo "warning: goversioninfo failed; exe will lack File version metadata." >&2
  rm -f "$SYSO"
fi

go build -ldflags "$LDFLAGS" -o checkin.exe ./cmd/checkin/
echo "Built ./checkin.exe (v$VERSION)"

# --- Verify it is truly self-contained --------------------------------------
# Two ways to check, since ldd only exists on Windows: natively it resolves each
# import by loader rules (so a leaked non-system DLL shows up as a /mingw64/
# path or "not found"); cross-compiling, objdump lists the import table and
# every name must be a known Windows system DLL.
#
# Neither check may be skipped silently: an earlier version ran ldd
# unconditionally, so on macOS it printed "ldd: command not found", the `if` saw
# a non-zero status, and the release continued completely unverified.
if command -v ldd >/dev/null 2>&1; then
  if ldd checkin.exe | grep -qiE '/mingw64/|=> not found'; then
    echo "Static build is NOT self-contained -- these deps are non-system:" >&2
    ldd checkin.exe | grep -iE '/mingw64/|not found' >&2
    echo "Something failed to link statically; aborting." >&2
    exit 1
  fi
  echo "Self-contained: all imports resolve to system DLLs (ldd)."
elif command -v x86_64-w64-mingw32-objdump >/dev/null 2>&1; then
  # Allow-list of Windows system DLLs this app legitimately imports. The
  # api-ms-win-crt-* set is the UCRT, present on Windows 10+.
  BAD="$(x86_64-w64-mingw32-objdump -p checkin.exe \
    | sed -n 's/^[[:space:]]*DLL Name:[[:space:]]*//p' \
    | tr 'A-Z' 'a-z' | sort -u \
    | grep -vE '^(api-ms-win-crt-[a-z0-9-]+\.dll|advapi32\.dll|comdlg32\.dll|gdi32\.dll|kernel32\.dll|ole32\.dll|oleaut32\.dll|opengl32\.dll|quartz\.dll|shell32\.dll|user32\.dll|winmm\.dll|ws2_32\.dll|imm32\.dll|dwmapi\.dll|msvcrt\.dll)$' || true)"
  if [ -n "$BAD" ]; then
    echo "Static build is NOT self-contained -- unexpected DLL imports:" >&2
    printf '  %s\n' $BAD >&2
    echo "Something failed to link statically; aborting." >&2
    exit 1
  fi
  echo "Self-contained: all imports are Windows system DLLs (objdump)."
else
  echo "Cannot verify self-containment: neither ldd nor x86_64-w64-mingw32-objdump found." >&2
  exit 1
fi

# --- Versioned release artifact ---------------------------------------------
mkdir -p dist
cp checkin.exe "dist/checkin-$VERSION.exe"
echo "Release exe -> dist/checkin-$VERSION.exe ($(du -h checkin.exe | cut -f1))"

# The exe this produced is unsigned, so Windows greets it with a SmartScreen
# "unknown developer" prompt. Signing is a separate step (it needs the key, and
# this build must work without one), which makes it easy to forget -- and the
# failure is silent here and only visible to whoever downloads it. So say so.
#
# Skipped when the artifact already carries a signature, which happens when this
# script is re-run over a signed exe: the copy above has just discarded that
# signature, and the note below is what says to put it back.
# Output is captured before matching rather than piped into grep: `verify`
# always exits non-zero here (this chain is not publicly rooted), and under
# `set -o pipefail` a pipeline would inherit that and invert the test.
_SIG_OUT=""
if command -v osslsigncode >/dev/null 2>&1; then
  _SIG_OUT="$(osslsigncode verify -in "dist/checkin-$VERSION.exe" 2>&1 || true)"
fi
if [ -z "$_SIG_OUT" ] || [[ "$_SIG_OUT" == *"No signature found"* ]]; then
  echo
  echo "This exe is UNSIGNED. For a release, sign and verify it:"
  echo "    make sign-windows"
  echo "    make verify-windows"
  echo "(Rebuilding discards a signature, so sign after the final build.)"
fi

if [ "$RUN" -eq 1 ]; then
  echo "Running..."
  ./checkin.exe -log-level DEBUG -log-file -
fi
