#!/usr/bin/env bash
# Create a DRAFT GitHub release from the artifacts in dist/.
#
# Draft on purpose: the release is assembled and uploaded but stays invisible
# until you publish it from the web UI. That is the last chance to check the
# version, the notes, and the attached files against what you meant to ship --
# a published release is a URL other people may already have fetched, so it is
# not something to undo.
#
# Uploads:
#   checkin-$VERSION-windows-amd64.zip   the signed exe
#   manifest.json                        version + hashes
#   manifest.json.p7s                    detached CMS signature over the manifest
#   manifest.json.tsr                    RFC3161 timestamp (when one was obtained)
#
# Release notes come from the matching section of CHANGELOG.md.
#
# Usage:
#   ./scripts/release.sh            # draft release for $VERSION
#   ./scripts/release.sh --publish  # publish immediately (skips the draft step)
#
# Requires the GitHub CLI, authenticated:
#   brew install gh && gh auth login
set -euo pipefail
# Scripts live in scripts/; every path below is relative to the repo root.
cd "$(dirname "$0")/.."

# Must match the Makefile's VERSION, which is the single source of truth.
VERSION="${VERSION:-0.0.9}"

# Where the release lives. Shared with scripts/make-manifest.sh, which records
# the same base in the manifest's artifact URLs; the release notes link back to
# docs/ at the tag this creates.
REPO_URL="${CHECKIN_REPO_URL:-https://github.com/pglass/checkin}"

PUBLISH=0
for arg in "$@"; do
  case "$arg" in
    --publish) PUBLISH=1 ;;
    *) echo "unknown option: $arg" >&2; exit 2 ;;
  esac
done

# Tags in this repo are bare versions (0.0.7), not v-prefixed. The release
# name, the tag, and the URLs the manifest points at all derive from this.
TAG="$VERSION"
ZIP="dist/checkin-$VERSION-windows-amd64.zip"
MANIFEST="dist/manifest.json"
SIG="$MANIFEST.p7s"
TSR="$MANIFEST.tsr"

if ! command -v gh >/dev/null 2>&1; then
  echo "gh not found. Install and authenticate it:" >&2
  echo "  brew install gh && gh auth login" >&2
  exit 1
fi
if ! gh auth status >/dev/null 2>&1; then
  echo "gh is not authenticated. Run:  gh auth login" >&2
  exit 1
fi

# --- Require a complete, verified set ---------------------------------------
# Every artifact must exist before anything is uploaded. A half-uploaded
# release is worse than none: a client that fetches a manifest with no
# signature beside it cannot tell "not signed yet" from "signature stripped".
for f in "$ZIP" "$MANIFEST" "$SIG"; do
  if [ ! -f "$f" ]; then
    echo "Missing release artifact: $f" >&2
    echo "Build and sign first:" >&2
    echo "    make build-windows && make sign-windows" >&2
    echo "    make manifest && make sign-manifest" >&2
    exit 1
  fi
done

# A missing timestamp is survivable (the signature is still valid), but it
# means this release stops verifying once the signing leaf expires. Worth a
# deliberate decision rather than silent omission.
TSR_ARGS=()
if [ -f "$TSR" ]; then
  TSR_ARGS=("$TSR")
else
  echo "warning: no timestamp token ($TSR)." >&2
  echo "  This release will not verify after the signing certificate expires." >&2
  echo "  Re-run 'make sign-manifest' to retry the timestamp." >&2
fi

# Re-verify rather than trusting that sign-manifest ran: dist/ persists between
# builds, so the manifest here could predate the zip sitting next to it.
echo "Verifying signatures before upload"
./scripts/verify-manifest.sh

# The manifest's version must match what is being tagged, or clients comparing
# versions will draw the wrong conclusion from a correctly-signed file.
MANIFEST_VERSION="$(jq -r .version "$MANIFEST")"
if [ "$MANIFEST_VERSION" != "$VERSION" ]; then
  echo "Manifest version ($MANIFEST_VERSION) does not match VERSION ($VERSION)." >&2
  echo "Rebuild it:  make manifest && make sign-manifest" >&2
  exit 1
fi

# The manifest names the URL each artifact will live at, and that URL embeds
# the tag. Catch a mismatch here rather than shipping a manifest that points
# at a 404.
MANIFEST_ZIP="$(jq -r '.artifacts[0].filename' "$MANIFEST")"
if [ "$MANIFEST_ZIP" != "$(basename "$ZIP")" ]; then
  echo "Manifest names $MANIFEST_ZIP but the zip is $(basename "$ZIP")." >&2
  exit 1
fi

# Confirm the zip on disk is the one the manifest vouches for. Without this a
# stale zip could ship under a fresh, correctly-signed manifest -- the client
# would reject the download and there would be nothing to point at.
if command -v shasum >/dev/null 2>&1; then
  ZIP_SHA="$(shasum -a 256 "$ZIP" | cut -d' ' -f1)"
else
  ZIP_SHA="$(sha256sum "$ZIP" | cut -d' ' -f1)"
fi
MANIFEST_SHA="$(jq -r '.artifacts[0].sha256' "$MANIFEST")"
if [ "$ZIP_SHA" != "$MANIFEST_SHA" ]; then
  echo "Zip hash does not match the manifest:" >&2
  echo "  zip:      $ZIP_SHA" >&2
  echo "  manifest: $MANIFEST_SHA" >&2
  echo "Rebuild it:  make manifest && make sign-manifest" >&2
  exit 1
fi
echo "  manifest matches $(basename "$ZIP")"

if gh release view "$TAG" >/dev/null 2>&1; then
  echo "Release $TAG already exists." >&2
  echo "Delete it first (gh release delete $TAG) or bump the version." >&2
  exit 1
fi

# --- Release notes ----------------------------------------------------------
# Pull the section for this version out of CHANGELOG.md: from its own '# x.y.z'
# heading up to the next one. Falls back to a placeholder so a missing section
# cannot block a release -- the draft is editable before publishing anyway.
NOTES_FILE="$(mktemp -t checkin-notes)"
trap 'rm -f "$NOTES_FILE"' EXIT
awk -v ver="$VERSION" '
  $0 == "# " ver { found = 1; next }
  found && /^# / { exit }
  found { print }
' CHANGELOG.md > "$NOTES_FILE"

if [ ! -s "$NOTES_FILE" ]; then
  echo "warning: no '# $VERSION' section found in CHANGELOG.md." >&2
  echo "Release notes for $VERSION." > "$NOTES_FILE"
fi

# Say what the signature is and is not, and document how to check a download.
# Anyone installing the root is trusting everything it ever signs, so the
# self-issued nature belongs in the release itself, not only in the docs.
cat >> "$NOTES_FILE" <<NOTES

---

**Signing**

The binary and \`manifest.json\` are signed with a self-issued root CA, not a
certificate from a public CA. Trust at your own risk.

Root certificate: [\`docs/checkin-root.crt\`]($REPO_URL/blob/$TAG/docs/checkin-root.crt)
Fingerprint: [\`docs/CERT.md\`]($REPO_URL/blob/$TAG/docs/CERT.md#fingerprint)

**Verifying this download**

\`manifest.json\` lists the SHA-256 of each artifact and is signed by the
project's code-signing certificate (\`manifest.json.p7s\`, detached CMS, with
the signer chain embedded). Verify it against the root CA:

\`\`\`sh
openssl cms -verify -binary -in manifest.json.p7s -inform DER \\
  -content manifest.json -CAfile checkin-root.crt -purpose any
\`\`\`

Then check the zip against the \`sha256\` recorded in the manifest.
NOTES

# --- Create -----------------------------------------------------------------
# `[ ... ] && DRAFT_ARGS=()` would be the last command in the script's
# top-level flow when PUBLISH is 0, and under `set -e` a false test there exits
# 1. Use an if/else so the status never leaks.
if [ "$PUBLISH" -eq 1 ]; then
  DRAFT_ARGS=()
  DRAFT_LABEL=""
else
  DRAFT_ARGS=(--draft)
  DRAFT_LABEL="draft "
fi

echo "Creating ${DRAFT_LABEL}release $TAG"
gh release create "$TAG" \
  ${DRAFT_ARGS[@]+"${DRAFT_ARGS[@]}"} \
  --title "$TAG" \
  --notes-file "$NOTES_FILE" \
  "$ZIP" "$MANIFEST" "$SIG" ${TSR_ARGS[@]+"${TSR_ARGS[@]}"}

rm -f "$NOTES_FILE"
trap - EXIT

echo
if [ "$PUBLISH" -eq 1 ]; then
  echo "Published $TAG."
else
  echo "Draft created. Review it, then publish:"
  echo "    gh release view $TAG --web"
  echo "    gh release edit $TAG --draft=false"
fi
