#!/usr/bin/env bash
# Run `go test` on Windows (Git Bash / MSYS) against the same custom slim STATIC
# OpenCV that build-windows.sh links, via gocv's `customenv` build tag. A bare
# `go test` fails to compile gocv because cgo can't find OpenCV -- this sets up
# the toolchain + CGO_* env first (see opencv-env-windows.sh), the Windows
# counterpart of the Makefile's `make test`.
#
# Usage (args are passed straight through to `go test`):
#   ./test-windows.sh                                  # test ./...
#   ./test-windows.sh ./internal/camera/               # one package
#   ./test-windows.sh ./internal/camera/ -run '^$' -bench BenchmarkQRDetect -benchtime 2s
set -euo pipefail
cd "$(dirname "$0")"

# MinGW toolchain + static-OpenCV cgo env, identical to build-windows.sh.
source ./opencv-env-windows.sh

# customenv: gocv takes all cgo flags from the CGO_* env above.
# migrated_fynedo: opts into Fyne 2.8's future main-goroutine behaviour.
TAGS="customenv,migrated_fynedo"

# Default to the whole module when no package/flags are given.
if [ "$#" -eq 0 ]; then
  set -- ./...
fi

go test -tags "$TAGS" "$@"
