#!/usr/bin/env bash
# Package the signed Windows exe into a release zip and describe it in a
# manifest.json that a future updater can read.
#
# The manifest is the machine-readable half of a release: it names the current
# version and, for each artifact, the URL to fetch and the SHA-256 to check.
# A client compares the manifest's version to its own, downloads the zip, and
# verifies the hash before unpacking. scripts/sign-manifest.sh then signs the
# manifest itself, so the client can trust what it just read.
#
# Usage:
#   ./scripts/make-manifest.sh    # -> dist/checkin-$VERSION-windows-amd64.zip
#                                 #    dist/manifest.json
#
# Two artifacts are described: the installer (kind "installer") and the portable
# zip (kind "zip"). Both carry the same exe. A client picks by `kind` rather than
# by position, so the order here is not load-bearing.
#
# Each hash covers the file that gets downloaded -- the zip, or the setup.exe --
# not the exe inside it. That is the point: the download is what must be verified
# before anything unpacks or executes it. The inner `contains.sha256` is
# diagnostic, for a human comparing an installed file against a release.
set -euo pipefail
# Scripts live in scripts/; every path below is relative to the repo root.
cd "$(dirname "$0")/.."

# Must match the Makefile's VERSION, which is the single source of truth.
VERSION="${VERSION:-0.0.10}"

# Where the release will live once scripts/release.sh uploads it. The manifest
# has to carry absolute URLs: a client fetching the *latest* manifest needs to
# reach an artifact of a version it does not know yet, so relative paths would
# leave it nothing to resolve against.
REPO_URL="${CHECKIN_REPO_URL:-https://github.com/pglass/checkin}"

for arg in "$@"; do
  echo "unknown option: $arg" >&2
  exit 2
done

EXE="dist/checkin.exe"
ZIP="dist/checkin-$VERSION-windows-amd64.zip"
SETUP="dist/checkin-$VERSION-setup.exe"
MANIFEST="dist/manifest.json"

if [ ! -f "$EXE" ]; then
  echo "No such exe: $EXE" >&2
  echo "Build one first:  make build-windows" >&2
  exit 1
fi

# --- Require a signed exe ---------------------------------------------------
# Not a security property of the manifest: the SHA-256 below is signed, so the
# download is already tamper-evident whether or not the exe carries an
# Authenticode signature. The reason to insist is who does the checking.
# Windows checks Authenticode automatically, for every user, including everyone
# who grabs the exe straight off the releases page and never goes near the
# updater. An unsigned exe ships a SmartScreen "unknown developer" prompt to
# those people, and since `make build-windows` silently discards any existing
# signature, shipping one unsigned is an easy mistake to make and an expensive
# one to notice. Fail here rather than on a customer's machine.
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
  echo "$EXE is UNSIGNED. Sign it before building a release:" >&2
  echo "    make sign-windows" >&2
  exit 1
fi

# --- Require a signed installer ---------------------------------------------
# The installer is the artifact most people actually download, so it gets the
# same treatment as the exe: it must exist, and it must be signed. An unsigned
# installer shows SmartScreen's "unknown developer" prompt even though the exe
# inside it is signed -- the installer is what the user launches first.
if [ ! -f "$SETUP" ]; then
  echo "No such installer: $SETUP" >&2
  echo "Build one first:  make installer-windows" >&2
  exit 1
fi
SETUP_SIG_OUT="$(osslsigncode verify -in "$SETUP" 2>&1 || true)"
if [[ "$SETUP_SIG_OUT" == *"No signature found"* ]]; then
  echo "$SETUP is UNSIGNED. Sign it before building a release:" >&2
  echo "    make sign-installer" >&2
  exit 1
fi

# --- Zip --------------------------------------------------------------------
# -j drops directory names so the archive is a flat checkin.exe rather than
# dist/checkin.exe. -X omits the extra file attributes (uid/gid, timestamps
# beyond the DOS field) that would otherwise vary between machines.
rm -f "$ZIP"
zip -q -j -X "$ZIP" "$EXE"
echo "Release zip -> $ZIP ($(du -h "$ZIP" | cut -f1))"

# --- Hashes -----------------------------------------------------------------
# shasum is present on macOS and in Git Bash; sha256sum is the Linux spelling.
sha256_of() {
  if command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$1" | cut -d' ' -f1
  else
    sha256sum "$1" | cut -d' ' -f1
  fi
}

ZIP_SHA="$(sha256_of "$ZIP")"
EXE_SHA="$(sha256_of "$EXE")"
ZIP_SIZE="$(wc -c <"$ZIP" | tr -d ' ')"
SETUP_SHA="$(sha256_of "$SETUP")"
SETUP_SIZE="$(wc -c <"$SETUP" | tr -d ' ')"

# --- Manifest ---------------------------------------------------------------
# Built with jq rather than a heredoc so that every value is correctly escaped
# and the output is byte-stable. Stability matters more than it looks: this
# exact byte sequence is what gets signed and timestamped, so a formatting
# change between runs would invalidate a signature made moments earlier.
#
# released_at is the fallback for checking whether the signing leaf was valid
# when the release was made. It is only as trustworthy as the signature over
# it; scripts/sign-manifest.sh also fetches an RFC3161 token, which is what
# makes the claim hold up against the signer as well.
#
# exe_sha256 is diagnostic only. The client verifies zip_sha256 before it
# unpacks anything; the inner hash is there for a human comparing a file on
# disk to a release.
jq -n \
  --arg version "$VERSION" \
  --arg released_at "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
  --arg notes_url "$REPO_URL/releases/tag/$VERSION" \
  --arg filename "$(basename "$ZIP")" \
  --arg url "$REPO_URL/releases/download/$VERSION/$(basename "$ZIP")" \
  --argjson size "$ZIP_SIZE" \
  --arg sha256 "$ZIP_SHA" \
  --arg exe_sha256 "$EXE_SHA" \
  --arg setup_filename "$(basename "$SETUP")" \
  --arg setup_url "$REPO_URL/releases/download/$VERSION/$(basename "$SETUP")" \
  --argjson setup_size "$SETUP_SIZE" \
  --arg setup_sha256 "$SETUP_SHA" \
  '{
    schema: 1,
    version: $version,
    released_at: $released_at,
    notes_url: $notes_url,
    artifacts: [
      {
        os: "windows",
        arch: "amd64",
        kind: "installer",
        filename: $setup_filename,
        url: $setup_url,
        size: $setup_size,
        sha256: $setup_sha256,
        contains: { filename: "checkin.exe", sha256: $exe_sha256 }
      },
      {
        os: "windows",
        arch: "amd64",
        kind: "zip",
        filename: $filename,
        url: $url,
        size: $size,
        sha256: $sha256,
        contains: { filename: "checkin.exe", sha256: $exe_sha256 }
      }
    ]
  }' >"$MANIFEST"

echo "Manifest    -> $MANIFEST"
echo "  version   $VERSION"
echo "  setup     sha256:$SETUP_SHA"
echo "  zip       sha256:$ZIP_SHA"
echo "  exe       sha256:$EXE_SHA"
echo
echo "Next: sign the manifest, then draft the release:"
echo "    make sign-manifest"
echo "    make release"
