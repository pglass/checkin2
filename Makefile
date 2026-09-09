# Build helpers for the checkin app.
#
# There is exactly ONE way this project links OpenCV: the slim STATIC OpenCV 5
# built once by `make opencv`, with all cgo flags supplied by
# ./opencv-env.sh. Every target here, both build scripts and both packaging
# scripts source that one file, so builds and tests can never disagree about
# how OpenCV is linked. No Homebrew opencv is required and the binaries are
# self-contained.
#
# Build OpenCV before anything else (once; ~20-40 min):
#
#     make opencv           # for this machine
#     make opencv-windows   # cross-compile for Windows
#
# ARCH selects the target on macOS (defaults to this machine's). ARCH=x86_64 on
# an Apple Silicon Mac cross-builds for Intel Macs.
#
# Every Go target runs through `scripts/with-opencv-env`, a thin wrapper that
# sources opencv-env.sh and then execs its arguments. Make runs each recipe line
# in its own shell, so the environment cannot be exported once at the top.

ARCH ?= $(shell uname -m)
# Sources opencv-env.sh, then runs the rest of the command line with that env.
GO_ENV := ARCH=$(ARCH) ./scripts/with-opencv-env

# App version. Bump here (single source of truth); it is stamped into the binary
# via -ldflags -X and into the macOS bundle's Info.plist via --appVersion. The
# build scripts read it from the environment, so this stays the only copy.
VERSION ?= 0.0.7
VERSION_LDFLAGS := -X github.com/pglass/checkin/internal/version.Version=$(VERSION)

# All Go sources: the checkin binary rebuilds only when one of these changes.
GO_SOURCES := $(shell find . -name '*.go')

.PHONY: build-windows build-darwin-release run test bench vet generate \
        seed bundle opencv opencv-windows bump-version clean

# --- Development ------------------------------------------------------------

# Dev build for this machine: keeps debug symbols. `make build-darwin-release`
# is the stripped, verified distributable.
checkin: $(GO_SOURCES)
	$(GO_ENV) go build -ldflags="$(VERSION_LDFLAGS) -extldflags=-Wl,-no_warn_duplicate_libraries" -o checkin ./cmd/checkin/

run: checkin
	./checkin -log-level DEBUG -log-file -

# Args pass through: `make test ARGS="./internal/camera/ -run TestQRDetect -v"`
test:
	$(GO_ENV) go test $(if $(ARGS),$(ARGS),./...)

# Detector benchmarks. `make bench ARGS=-benchtime=3s` to run them longer.
bench:
	$(GO_ENV) go test ./internal/camera/ -run '^$$' -bench . $(ARGS)

vet:
	$(GO_ENV) go vet ./...

generate:
	sqlc generate

# The seed tool touches only the DB, so it needs no OpenCV env.
seed:
	go build -o seed ./cmd/seed/

# Bump the app version in the Makefile and both build scripts.
#   make bump-version                 # patch bump (x.y.z -> x.y.z+1)
#   VERSION=1.2.3 make bump-version   # explicit version
# Refuses to move the version backwards.
#
# `VERSION ?=` above means $(VERSION) is always set, so the script cannot tell an
# explicit VERSION=x.y.z from the file's own default. $(origin) can: it reports
# "file" for the default and "environment"/"command line" when the user set it.
bump-version:
	@VERSION="$(if $(filter-out file,$(origin VERSION)),$(VERSION))" ./scripts/bump-version

clean:
	rm -f checkin checkin-* checkin.exe seed

# --- Distribution -----------------------------------------------------------

# macOS .app bundle. --appVersion stamps CFBundleShortVersionString in
# Info.plist (the native "About" panel) and makes fyne.CurrentApp().Metadata()
# .Version return it at runtime, which is what the in-app About dialog and
# --version read.
bundle:
	$(GO_ENV) fyne package --src ./cmd/checkin --name Checkin \
		--app-id com.pglass.checkin --icon $(CURDIR)/Icon.png --appVersion $(VERSION)

# macOS: stripped, self-contained binary. Also verifies with otool that nothing
# outside /usr/lib and /System/Library is linked. `make build` produces the same
# linkage but keeps debug symbols.
#   make build-darwin-release              -> ./checkin        (native arch)
#   ARCH=x86_64 make build-darwin-release  -> ./checkin-x86_64 (Intel Macs)
build-darwin:
	ARCH=$(ARCH) VERSION=$(VERSION) ./build-darwin.sh

# Cross-compile checkin.exe from macOS/Linux (or build it natively on Windows).
# Verifies the exe is self-contained and copies it to dist/. Requires
# `make opencv-windows` once first, plus `brew install mingw-w64` when cross-
# compiling. A cross-built exe cannot be run here -- verify it on Windows.
build-windows:
	VERSION=$(VERSION) ./build-windows.sh

# --- One-time OpenCV build --------------------------------------------------

# Builds the slim static OpenCV every other target links. Picks the right script
# for this platform. Once per OpenCV version (and per arch on macOS).
opencv:
ifeq ($(shell uname -s),Darwin)
	ARCH=$(ARCH) ./build-opencv-static-darwin.sh
else
	./build-opencv-static.sh
endif

# The Windows OpenCV that `make build-windows` links. On Windows this is the
# same thing as `make opencv`; on macOS/Linux it cross-compiles into a
# -windows-suffixed prefix alongside the native one.
opencv-windows:
	./build-opencv-static.sh
