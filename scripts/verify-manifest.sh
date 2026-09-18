#!/usr/bin/env bash
# Verify dist/manifest.json against its detached signature and timestamp.
#
# Needs no key material -- only the public root CA -- so anyone who downloads a
# release can run it. scripts/sign-manifest.sh also runs it on what it just
# produced, so a signature is always checked by the same code that validates
# one found in the wild.
#
# Two separate trust anchors are involved, and they are not interchangeable:
#
#   1. This project's root CA, which signs the leaf that signs the manifest.
#      The leaf travels inside the .p7s (CMS carries the signer chain), so only
#      the root is needed here. A client must PIN this root -- compile it in,
#      never fetch it alongside the manifest: whoever could swap one could swap
#      both.
#
#   2. The timestamp authority's own root, needed to check the .tsr. The TSA is
#      publicly trusted, so this comes from the system CA bundle and needs no
#      setup -- it is not part of this project's PKI.
#
# Usage:
#   ./scripts/verify-manifest.sh                    # verify dist/manifest.json
#   ./scripts/verify-manifest.sh --root ca.crt      # against a specific root
#   ./scripts/verify-manifest.sh path/to/manifest.json
#
# Environment:
#   CHECKIN_ROOT_CRT   root CA (default ~/.checkin-signing/checkin-root.crt)
#   CHECKIN_TSA_ROOTS  override the CA bundle used for the timestamp; normally
#                      unset, since the system bundle is found automatically
set -euo pipefail
# Scripts live in scripts/; every path below is relative to the repo root.
cd "$(dirname "$0")/.."

ROOT="${CHECKIN_ROOT_CRT:-$HOME/.checkin-signing/checkin-root.crt}"
MANIFEST=""

while [ $# -gt 0 ]; do
  case "$1" in
    --root)
      shift
      [ $# -gt 0 ] || { echo "--root needs a path" >&2; exit 2; }
      ROOT="$1"
      ;;
    -*) echo "unknown option: $1" >&2; exit 2 ;;
    *) MANIFEST="$1" ;;
  esac
  shift
done

[ -n "$MANIFEST" ] || MANIFEST="dist/manifest.json"
SIG="$MANIFEST.p7s"
TSR="$MANIFEST.tsr"

if [ ! -f "$MANIFEST" ]; then
  echo "No such manifest: $MANIFEST" >&2
  echo "Build one first:  make manifest" >&2
  exit 1
fi
if [ ! -f "$SIG" ]; then
  echo "No signature found: $SIG" >&2
  echo "Sign it first:  make sign-manifest" >&2
  exit 1
fi
if [ ! -f "$ROOT" ]; then
  echo "Root CA not found: $ROOT" >&2
  echo "Pass --root /path/to/checkin-root.crt (or set CHECKIN_ROOT_CRT)." >&2
  exit 1
fi

STATUS=0

# --- signature --------------------------------------------------------------
# -purpose any: the leaf carries the code-signing EKU, and OpenSSL's default
# purpose for CMS verification is S/MIME/e-mail, which that EKU does not
# satisfy. Without this a perfectly good signature is rejected for the wrong
# reason. Chain validity is still fully enforced against -CAfile.
echo "Verifying $MANIFEST"
echo "  root: $ROOT"
if openssl cms -verify -binary -in "$SIG" -inform DER \
    -content "$MANIFEST" -CAfile "$ROOT" -purpose any \
    -out /dev/null 2>/dev/null; then
  echo "  signature OK (chains to root)"
else
  echo "  SIGNATURE FAILED to verify against $ROOT" >&2
  STATUS=1
fi

# --- timestamp --------------------------------------------------------------
# Checked in two independent parts, and both matter:
#   - that the token really covers this manifest and the TSA's own signature is
#     good. The timestamp authority is publicly trusted, so unlike this
#     project's root this anchors to the system's normal CA bundle.
#   - what time the token asserts, which is only meaningful once the first
#     check passes. An unverified token's genTime is attacker-chosen.
#
# `openssl ts -verify` does not fall back to the default trust store the way
# most openssl subcommands do: with no -CAfile it fails with "unable to get
# local issuer certificate" even for a publicly-trusted TSA, and the default
# -CApath directory is empty on a typical Homebrew install. So the bundle has
# to be located and passed explicitly.
tsa_bundle() {
  local f
  if [ -n "${CHECKIN_TSA_ROOTS:-}" ]; then
    # An explicit override must not fail silently: if it is set but unusable,
    # say so rather than quietly verifying against a different trust store.
    if [ -f "$CHECKIN_TSA_ROOTS" ]; then
      echo "$CHECKIN_TSA_ROOTS"
      return 0
    fi
    return 1
  fi
  for f in \
    "$(openssl version -d 2>/dev/null | sed 's/OPENSSLDIR: //;s/"//g')/cert.pem" \
    /etc/ssl/cert.pem \
    /etc/ssl/certs/ca-certificates.crt \
    /etc/pki/tls/certs/ca-bundle.crt \
    /opt/homebrew/etc/ca-certificates/cert.pem \
    /usr/local/etc/ca-certificates/cert.pem
  do
    [ -f "$f" ] && { echo "$f"; return 0; }
  done
  return 1
}

if [ -f "$TSR" ]; then
  TS_TIME="$(openssl ts -reply -in "$TSR" -text 2>/dev/null \
    | sed -n 's/^Time stamp: *//p' | head -1)"
  if TSA_ROOTS="$(tsa_bundle)"; then
    if openssl ts -verify -data "$MANIFEST" -in "$TSR" \
        -CAfile "$TSA_ROOTS" >/dev/null 2>&1; then
      echo "  timestamp OK (${TS_TIME:-unknown time})"
    else
      echo "  TIMESTAMP FAILED to verify against $TSA_ROOTS" >&2
      STATUS=1
    fi
  elif [ -n "${CHECKIN_TSA_ROOTS:-}" ]; then
    echo "  CHECKIN_TSA_ROOTS is set but not a file: $CHECKIN_TSA_ROOTS" >&2
    STATUS=1
  else
    echo "  timestamp present (${TS_TIME:-unknown time}); not verified"
    echo "    no system CA bundle found; set CHECKIN_TSA_ROOTS to one"
  fi
else
  echo "  no timestamp ($TSR absent)"
fi

exit $STATUS
