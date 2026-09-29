#!/usr/bin/env bash
# Regenerate Icon.ico from Icon.png.
#
# Icon.ico is committed rather than built on every release: it changes only when
# the artwork does, and both the exe build (goversioninfo -icon) and the
# installer need it, so generating it in one of them would mean the other
# depended on that one having run first.
#
# Run this after changing Icon.png, and commit the result:
#
#     ./scripts/make-icon.sh && git add Icon.ico
#
# Nothing on a stock macOS writes an .ico -- sips has no such output format and
# iconutil produces .icns -- so the container is assembled directly: a 6-byte
# header, a 16-byte directory entry per frame, then the frames themselves.
# Windows Vista and later accept PNG frames verbatim, so sips can produce them
# and no BMP conversion is needed.
set -euo pipefail
# Scripts live in scripts/; every path below is relative to the repo root.
cd "$(dirname "$0")/.."

SRC="Icon.png"
OUT="Icon.ico"

if [ ! -f "$SRC" ]; then
  echo "No such file: $SRC" >&2
  exit 1
fi

if ! command -v sips >/dev/null 2>&1; then
  echo "sips not found; this script needs macOS." >&2
  echo "Regenerate Icon.ico on a Mac and commit it, or use any tool that" >&2
  echo "writes a multi-resolution .ico (ImageMagick: magick Icon.png Icon.ico)." >&2
  exit 1
fi

# The six sizes Windows picks between: 16 in the title bar and tray, 32 in the
# taskbar, 48 in Explorer, 256 for large thumbnails, and 64/128 so the scaler
# never has to interpolate far on a HiDPI display.
TMP="$(mktemp -d -t checkin-ico)"
trap 'rm -rf "$TMP"' EXIT
FRAMES=()
for size in 16 32 48 64 128 256; do
  sips -z "$size" "$size" "$SRC" --out "$TMP/icon-$size.png" >/dev/null
  FRAMES+=("$TMP/icon-$size.png")
done

python3 - "$OUT" "${FRAMES[@]}" <<'PY'
import struct, sys

out, pngs = sys.argv[1], sys.argv[2:]
frames = []
for path in pngs:
    with open(path, "rb") as fh:
        data = fh.read()
    # PNG IHDR carries width and height at a fixed offset.
    width, height = struct.unpack(">II", data[16:24])
    # The directory entry stores each dimension in one byte, where 0 means 256.
    frames.append((0 if width >= 256 else width,
                   0 if height >= 256 else height,
                   data))

header = struct.pack("<HHH", 0, 1, len(frames))   # reserved, type 1 = icon, count
offset = len(header) + 16 * len(frames)
entries = b""
payloads = b""
for width, height, data in frames:
    # width, height, palette count, reserved, planes, bpp, byte size, offset
    entries += struct.pack("<BBBBHHII", width, height, 0, 0, 1, 32, len(data), offset)
    payloads += data
    offset += len(data)

with open(out, "wb") as fh:
    fh.write(header + entries + payloads)
PY

echo "Wrote $OUT ($(wc -c <"$OUT" | tr -d ' ') bytes, 6 frames)"
echo "Commit it:  git add $OUT"
