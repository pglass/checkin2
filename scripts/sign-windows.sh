#!/usr/bin/env bash
# Authenticode-sign a Windows exe in dist/ with the project's code-signing cert.
#
# Windows SmartScreen blocks unsigned downloads with an "unknown developer"
# prompt. This project signs with a self-issued certificate chain: a long-lived
# offline root CA that customers install into Trusted Root once, and a
# short-lived leaf that does the actual signing. Machines with the root
# installed then trust every build, including ones signed by a future leaf.
# Machines without it still see the prompt -- that is inherent to not paying a
# public CA, and is why the root fingerprint is published for verification.
#
# Usage:
#   ./scripts/sign-windows.sh                      # sign dist/checkin-$VERSION.exe
#   ./scripts/sign-windows.sh dist/other.exe       # sign a specific exe
#   ./scripts/sign-windows.sh --verify dist/x.exe  # only check an existing signature
#
# Certificate and password come from the environment so nothing secret is ever
# written into this repo:
#   CHECKIN_P12        path to the PKCS#12 bundle (default ~/.checkin-signing/checkin-signing.p12)
#   CHECKIN_P12_PASS   its export password; prompted for if unset
#
# Signing is deliberately a separate step from build-windows.sh rather than part
# of it: the build runs on every change and must work with no secrets present,
# while signing happens once per release and needs the key. Keeping them apart
# also means a rebuild cannot silently strip a signature it does not know about.
set -euo pipefail
# Scripts live in scripts/; every path below is relative to the repo root.
cd "$(dirname "$0")/.."

# Timestamping is what makes an expiring leaf certificate workable. The
# authority countersigns with the time, so Windows validates "was the cert valid
# when this was signed" rather than "is it valid now". Without it every exe ever
# shipped would start failing the day the leaf expires.
TIMESTAMP_URL="${CHECKIN_TIMESTAMP_URL:-http://timestamp.digicert.com}"

# Must match the Makefile's VERSION, which is the single source of truth.
VERSION="${VERSION:-0.0.7}"

P12="${CHECKIN_P12:-$HOME/.checkin-signing/checkin-signing.p12}"

VERIFY_ONLY=0
TARGET=""
for arg in "$@"; do
  case "$arg" in
    --verify) VERIFY_ONLY=1 ;;
    -*) echo "unknown option: $arg" >&2; exit 2 ;;
    *) TARGET="$arg" ;;
  esac
done
[ -n "$TARGET" ] || TARGET="dist/checkin-$VERSION.exe"

if [ ! -f "$TARGET" ]; then
  echo "No such exe: $TARGET" >&2
  echo "Build one first:  make build-windows" >&2
  exit 1
fi

if ! command -v osslsigncode >/dev/null 2>&1; then
  echo "osslsigncode not found. Install it:" >&2
  echo "  macOS:  brew install osslsigncode" >&2
  echo "  Linux:  apt install osslsigncode" >&2
  exit 1
fi

# --- verify-only ------------------------------------------------------------
if [ "$VERIFY_ONLY" -eq 1 ]; then
  echo "Verifying $TARGET"
  # Verification needs the root as the trust anchor, since this chain is not
  # publicly rooted. Without -CAfile osslsigncode reports the chain as untrusted
  # even when the signature itself is perfectly good.
  ROOT="${CHECKIN_ROOT_CRT:-$HOME/.checkin-signing/checkin-root.crt}"
  if [ -f "$ROOT" ]; then
    exec osslsigncode verify -CAfile "$ROOT" -in "$TARGET"
  fi
  echo "note: root cert not found at $ROOT; chain will report as untrusted." >&2
  exec osslsigncode verify -in "$TARGET"
fi

# --- sign -------------------------------------------------------------------
if [ ! -f "$P12" ]; then
  echo "Signing certificate not found: $P12" >&2
  echo "Set CHECKIN_P12 to your .p12 bundle, or place it at that path." >&2
  exit 1
fi

# Refuse to sign twice. osslsigncode would nest a second signature rather than
# replace the first, which is legal but confusing; re-signing a release should
# be a deliberate act starting from a fresh build.
#
# Presence of a signature is detected from the output text, not the exit status:
# `verify` exits non-zero both for an unsigned file and for a signed one whose
# chain it cannot anchor, and this chain is never anchored by default since the
# root is not publicly trusted. Only "No signature found" distinguishes them.
# The output is captured before matching rather than piped into grep: under
# `set -o pipefail` the pipeline would inherit osslsigncode's non-zero exit
# (it always exits non-zero here) and the test would read backwards.
VERIFY_OUT="$(osslsigncode verify -in "$TARGET" 2>&1 || true)"
if [[ "$VERIFY_OUT" != *"No signature found"* ]]; then
  echo "$TARGET is already signed. Rebuild before re-signing:" >&2
  echo "  make build-windows && ./scripts/sign-windows.sh" >&2
  exit 1
fi

# Read the password without echoing it, and never take it from the command line
# where it would land in the shell history and the process table.
if [ -z "${CHECKIN_P12_PASS:-}" ]; then
  read -r -s -p "Export password for $(basename "$P12"): " CHECKIN_P12_PASS
  echo
fi

# Sign to a temporary file: osslsigncode cannot sign in place, and writing
# straight to the final name would leave a truncated exe behind if it failed.
TMP_OUT="$(mktemp -t checkin-signed).exe"
trap 'rm -f "$TMP_OUT"' EXIT

echo "Signing $TARGET"
osslsigncode sign \
  -pkcs12 "$P12" \
  -readpass - \
  -h sha256 \
  -n "Checkin" \
  -i "https://github.com/pglass/checkin" \
  -ts "$TIMESTAMP_URL" \
  -in "$TARGET" \
  -out "$TMP_OUT" <<<"$CHECKIN_P12_PASS"

mv "$TMP_OUT" "$TARGET"
trap - EXIT

echo "Signed -> $TARGET"

# Verify what was just written, so a broken signature is caught here rather than
# on a customer's machine. A signing run that cannot be verified is a failure.
ROOT="${CHECKIN_ROOT_CRT:-$HOME/.checkin-signing/checkin-root.crt}"
if [ -f "$ROOT" ]; then
  osslsigncode verify -CAfile "$ROOT" -in "$TARGET"
else
  echo "note: root cert not found at $ROOT; verifying signature only." >&2
  osslsigncode verify -in "$TARGET"
fi
