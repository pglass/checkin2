#!/usr/bin/env bash
# Sign dist/manifest.json with the project's code-signing key, and timestamp
# the signature.
#
# The manifest lists each artifact's SHA-256, so signing the manifest is what
# makes those hashes trustworthy: a client that verifies this signature, then
# checks a download against the hash inside, has an authenticated download.
#
# The signature is detached and CMS/PKCS#7 (DER), written beside the manifest
# as manifest.json.p7s. CMS carries the signer's certificate chain inside the
# signature, so a client needs only the pinned root CA to verify -- there is no
# separate leaf certificate to ship or keep in sync.
#
# Checking a signature is a separate script, scripts/verify-manifest.sh, which
# needs no key material; this one runs it at the end on what it just produced.
#
# Usage:
#   ./scripts/sign-manifest.sh                 # sign dist/manifest.json
#   ./scripts/sign-manifest.sh --root ca.crt   # root used for the final check
#
# Environment (matching scripts/sign-windows.sh):
#   CHECKIN_P12        PKCS#12 bundle (default ~/.checkin-signing/checkin-signing.p12)
#   CHECKIN_P12_PASS   its export password; prompted for if unset
#   CHECKIN_ROOT_CRT   root CA used for the post-signing check
#   CHECKIN_TSA_URL    RFC3161 timestamp authority
set -euo pipefail
# Scripts live in scripts/; every path below is relative to the repo root.
cd "$(dirname "$0")/.."

# An RFC3161 authority countersigns a hash with the current time. That is what
# lets a signature outlive the short-lived leaf that made it: a client checks
# "was the leaf valid at the time the token asserts" rather than "is it valid
# now". Deliberately the same authority scripts/sign-windows.sh uses for the
# exe: the endpoint speaks plain RFC3161, so one TSA covers both signatures and
# there is only one service to keep working.
TSA_URL="${CHECKIN_TSA_URL:-http://timestamp.digicert.com}"

P12="${CHECKIN_P12:-$HOME/.checkin-signing/checkin-signing.p12}"

# Only forwarded to scripts/verify-manifest.sh for the post-signing check;
# signing itself needs the key, not the root.
ROOT_ARGS=()
while [ $# -gt 0 ]; do
  case "$1" in
    --root)
      shift
      [ $# -gt 0 ] || { echo "--root needs a path" >&2; exit 2; }
      ROOT_ARGS=(--root "$1")
      ;;
    *) echo "unknown option: $1" >&2; exit 2 ;;
  esac
  shift
done

MANIFEST="dist/manifest.json"
SIG="$MANIFEST.p7s"
TSR="$MANIFEST.tsr"

if [ ! -f "$MANIFEST" ]; then
  echo "No such manifest: $MANIFEST" >&2
  echo "Build one first:  make manifest" >&2
  exit 1
fi

if [ ! -f "$P12" ]; then
  echo "Signing certificate not found: $P12" >&2
  echo "Set CHECKIN_P12 to your .p12 bundle, or place it at that path." >&2
  exit 1
fi

# Refuse to sign twice, matching scripts/sign-windows.sh. Re-signing should
# start from a fresh manifest: an existing signature covers the old bytes, and
# leaving both around invites shipping a mismatched pair.
if [ -f "$SIG" ]; then
  echo "$SIG already exists. Rebuild the manifest before re-signing:" >&2
  echo "  make manifest && make sign-manifest" >&2
  exit 1
fi

# Read the password without echoing it, and never take it from the command line
# where it would land in the shell history and the process table.
if [ -z "${CHECKIN_P12_PASS:-}" ]; then
  read -r -s -p "Export password for $(basename "$P12"): " CHECKIN_P12_PASS
  echo
fi

# openssl cms cannot read a PKCS#12 directly, so the leaf and its key are
# extracted to a temporary file first. It holds the private key unencrypted, so
# it is created under a private umask and removed on every exit path.
TMP_PEM="$(umask 077; mktemp -t checkin-signing-pem)"
TMP_TSQ="$(mktemp -t checkin-tsq)"
trap 'rm -f "$TMP_PEM" "$TMP_TSQ"' EXIT

# -passin stdin rather than an argument, for the same reason as above.
openssl pkcs12 -in "$P12" -nodes -passin stdin -out "$TMP_PEM" \
  <<<"$CHECKIN_P12_PASS"

echo "Signing $MANIFEST"
# -binary: sign the file's exact bytes. Without it CMS applies S/MIME text
#   canonicalization (CRLF line endings), so the signature would cover bytes
#   that differ from the file every client actually downloads.
# The signature stays detached (no -nodetach), so manifest.json remains plain
#   readable JSON with the .p7s beside it.
# -certfile: include the chain from the bundle, so the .p7s carries the leaf
#   (and any intermediate) and a client needs only the pinned root.
openssl cms -sign -binary \
  -in "$MANIFEST" \
  -signer "$TMP_PEM" \
  -certfile "$TMP_PEM" \
  -outform DER \
  -out "$SIG"

rm -f "$TMP_PEM"
echo "Signature   -> $SIG"

# --- timestamp --------------------------------------------------------------
# Only a hash leaves this machine: the query carries the digest of the
# manifest, never its contents.
#
# A failure here is a warning, not an error. The signature above is complete
# and verifiable without a token; what a missing token costs is the ability to
# prove, after the leaf expires, that the signature predates the expiry. Worth
# retrying for, not worth discarding a good signature over.
echo "Requesting timestamp from $TSA_URL"
if openssl ts -query -data "$MANIFEST" -sha256 -cert -out "$TMP_TSQ" 2>/dev/null \
   && curl -sS -f -H 'Content-Type: application/timestamp-query' \
        --data-binary "@$TMP_TSQ" "$TSA_URL" -o "$TSR"; then
  TS_TIME="$(openssl ts -reply -in "$TSR" -text 2>/dev/null \
    | sed -n 's/^Time stamp: *//p' | head -1)"
  echo "Timestamp   -> $TSR (${TS_TIME:-unknown time})"
else
  rm -f "$TSR"
  echo "warning: could not obtain a timestamp from $TSA_URL." >&2
  echo "  The signature is still valid, but will not outlive the signing" >&2
  echo "  certificate. Re-run to retry, or set CHECKIN_TSA_URL." >&2
fi

rm -f "$TMP_TSQ"
trap - EXIT

# Verify what was just written, so a broken signature is caught here rather
# than on a customer's machine. A signing run that cannot be verified is a
# failure, and this script's exit status says so.
echo
exec ./scripts/verify-manifest.sh ${ROOT_ARGS[@]+"${ROOT_ARGS[@]}"}
