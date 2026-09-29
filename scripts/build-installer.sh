#!/usr/bin/env bash
# Build the Windows installer, checkin-$VERSION-setup.exe, from dist/checkin.exe.
#
# Cross-builds from macOS/Linux: NSIS has a POSIX port (`brew install makensis`),
# so no Windows machine is needed here any more than it is for the exe itself.
# The installer cannot be smoke-tested here either -- verify it on Windows.
#
# Per-user install to %LOCALAPPDATA%\Programs\Checkin, so nothing prompts for
# administrator rights. See installer/checkin.nsi for why that and the plain
# overwrite upgrade were chosen.
#
# Usage:
#   ./scripts/build-installer.sh    # -> dist/checkin-$VERSION-setup.exe
#
# Ordering within a release matters and is not obvious:
#
#     build-windows -> sign-windows -> build-installer -> sign-installer
#
# The inner exe must be signed BEFORE it is compressed into the installer:
# Windows validates both signatures independently, the installer embeds a copy
# of whatever exe it is handed, and scripts/sign-windows.sh refuses to sign an
# already-signed file. Signing the installer afterwards cannot reach inside it.
# Hence the guard below, which fails rather than shipping an installer whose
# payload greets every customer with a SmartScreen prompt.
set -euo pipefail
# Scripts live in scripts/; every path below is relative to the repo root.
cd "$(dirname "$0")/.."

# Must match the Makefile's VERSION, which is the single source of truth.
VERSION="${VERSION:-0.0.9}"

for arg in "$@"; do
  echo "unknown option: $arg" >&2
  exit 2
done

EXE="dist/checkin.exe"
# Committed, not generated here: the exe build needs the same file, so building
# it in one script would make the other depend on that one having run first.
# Regenerate with scripts/make-icon.sh after changing Icon.png.
ICO="Icon.ico"
OUT="dist/checkin-$VERSION-setup.exe"

if [ ! -f "$EXE" ]; then
  echo "No such exe: $EXE" >&2
  echo "Build one first:  make build-windows" >&2
  exit 1
fi

if ! command -v makensis >/dev/null 2>&1; then
  echo "makensis not found. Install it:" >&2
  echo "  macOS:  brew install makensis" >&2
  echo "  Linux:  apt install nsis" >&2
  exit 1
fi

# --- Require a signed payload -----------------------------------------------
# Same reasoning as the guard in scripts/make-manifest.sh, one step earlier:
# Windows checks the inner exe's Authenticode signature when the installer
# writes it out and whenever it is launched thereafter. An unsigned payload
# inside a signed installer still shows "unknown developer" on first run, and by
# then the only fix is a new release.
if ! command -v osslsigncode >/dev/null 2>&1; then
  echo "osslsigncode not found, so the exe's signature cannot be checked." >&2
  echo "  macOS:  brew install osslsigncode" >&2
  echo "  Linux:  apt install osslsigncode" >&2
  exit 1
fi
# Output is captured before matching rather than piped into grep: `verify`
# always exits non-zero here (this chain is not publicly rooted), and under
# `set -o pipefail` a pipeline would inherit that and invert the test.
SIG_OUT="$(osslsigncode verify -in "$EXE" 2>&1 || true)"
if [[ "$SIG_OUT" == *"No signature found"* ]]; then
  echo "$EXE is UNSIGNED. Sign it before building the installer:" >&2
  echo "    make sign-windows" >&2
  echo "(The installer embeds this exe; signing the installer cannot sign it.)" >&2
  exit 1
fi

# --- Icon -------------------------------------------------------------------
if [ ! -f "$ICO" ]; then
  echo "No such icon: $ICO" >&2
  echo "Generate it:  ./scripts/make-icon.sh" >&2
  exit 1
fi

# --- Compile ----------------------------------------------------------------
# Paths are passed as defines rather than hardcoded in the .nsi so that the
# Makefile's VERSION stays the only place a version is written. NSIS resolves
# relative paths against the .nsi's own directory, hence the ../ prefixes.
echo "Compiling installer (LZMA over a ~$(du -h "$EXE" | cut -f1) payload; this takes a minute)"
makensis -V2 \
  "-DVERSION=$VERSION" \
  "-DSRC_EXE=..\\$EXE" \
  "-DOUT_FILE=..\\$OUT" \
  "-DICON=..\\$ICO" \
  installer/checkin.nsi

if [ ! -f "$OUT" ]; then
  echo "makensis reported success but $OUT is missing." >&2
  exit 1
fi

echo "Installer -> $OUT ($(du -h "$OUT" | cut -f1))"

# The installer is unsigned at this point, and an unsigned installer triggers
# SmartScreen even though its payload is signed -- the user meets the installer
# first, so this is the signature they actually see. Say so, for the same reason
# scripts/build-windows.sh does.
INST_SIG="$(osslsigncode verify -in "$OUT" 2>&1 || true)"
if [[ "$INST_SIG" == *"No signature found"* ]]; then
  echo
  echo "This installer is UNSIGNED. For a release, sign it:"
  echo "    make sign-installer"
fi
