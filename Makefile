# Build helpers for the checkin app.
#
# gocv links against OpenCV 4 via pkg-config. Everything here -- dev builds,
# tests and packaging alike -- links the slim STATIC OpenCV built once by
# `make opencv-static-darwin`, so no Homebrew opencv@4 is required and the
# binaries are self-contained. Build it before anything else:
#
#     make opencv-static-darwin        # once per arch, ~20-40 min
#
# ARCH selects the target (defaults to this machine's). ARCH=x86_64 on an Apple
# Silicon Mac cross-builds for Intel Macs; see build-darwin.sh.
OPENCV_VERSION := 5.0.0
ARCH ?= $(shell uname -m)
OPENCV_STATIC_PREFIX ?= $(HOME)/opencv-static/$(OPENCV_VERSION)-$(ARCH)
# PKG_CONFIG_LIBDIR, not PKG_CONFIG_PATH: PATH only *prepends* to pkg-config's
# built-in search path, so a Homebrew opencv@4 in /opt/homebrew/lib/pkgconfig
# would still be found -- silently linking dylibs and producing a binary that is
# not self-contained. LIBDIR replaces the search path outright, so the static
# OpenCV is the only one that can ever be picked up.
export PKG_CONFIG_LIBDIR := $(OPENCV_STATIC_PREFIX)/lib/pkgconfig
export CGO_ENABLED := 1

# opencvstatic:    selects third_party/gocv/cgo_static_darwin.go, which links
#                  OpenCV via `pkg-config --static opencv4`.
# migrated_fynedo: asserts that all UI mutations happen on the main goroutine or
#                  inside fyne.Do (see internal/ui/camera.go and holdbutton.go).
#                  It silences Fyne 2.8's migration warning and opts into the
#                  future default behaviour.
TAGS := opencvstatic,migrated_fynedo

# App version. Bump here (single source of truth); it is stamped into the binary
# via -ldflags -X and into the macOS bundle's Info.plist via --appVersion.
VERSION ?= 0.0.5
VERSION_LDFLAGS := -X github.com/pglass/checkin/internal/version.Version=$(VERSION)

# All Go sources: the checkin binary rebuilds only when one of these changes.
GO_SOURCES := $(shell find . -name '*.go')

.PHONY: run test generate package package-windows package-darwin opencv-static opencv-static-darwin check-opencv seed

# check-opencv is an order-only prerequisite (after the |) so it gates the build
# without forcing a rebuild on every invocation.
checkin: $(GO_SOURCES) | check-opencv
	go build -tags "$(TAGS)" -ldflags="$(VERSION_LDFLAGS) -extldflags=-Wl,-no_warn_duplicate_libraries" -o checkin ./cmd/checkin/

run: checkin
	./checkin -log-level DEBUG -log-file -

test: | check-opencv
	go test -tags "$(TAGS)" ./...

# Build the seed tool (no OpenCV needed; it only touches the DB).
seed:
	go build -o seed ./cmd/seed/

generate:
	sqlc generate

# Package a distributable .app bundle. Links the same static OpenCV as every
# other target, so the bundled binary is self-contained.
# --appVersion stamps CFBundleShortVersionString in Info.plist (the macOS native
# "About" panel) and makes fyne.CurrentApp().Metadata().Version return it at
# runtime, which is what the in-app About dialog and --version read.
package: check-opencv
	fyne package --tags "$(TAGS)" --src ./cmd/checkin --name Checkin --app-id com.pglass.checkin --icon $(CURDIR)/Icon.png \
		--appVersion $(VERSION)

# One-time (per OpenCV version): build the slim static OpenCV the static exe links.
# Windows only; the macOS counterpart is opencv-static-darwin below.
opencv-static:
	./build-opencv-static.sh

# macOS: build the slim static OpenCV that package-darwin links. Once per arch.
# ARCH=x86_64 builds for Intel Macs from an Apple Silicon machine.
opencv-static-darwin:
	./build-opencv-static-darwin.sh

# macOS: build a stripped, self-contained binary via build-darwin.sh, which also
# verifies with otool that nothing outside /usr/lib and /System/Library is
# linked. `make checkin` produces the same linkage but keeps debug symbols.
#   make package-darwin              -> ./checkin        (native arch)
#   ARCH=x86_64 make package-darwin  -> ./checkin-x86_64 (Intel Macs)
package-darwin:
	VERSION=$(VERSION) ./build-darwin.sh

# Windows packaging: build the single self-contained checkin.exe (no bundled
# DLLs). Requires `make opencv-static` to have been run once first. Shells out to
# the script; meant to be run from Git Bash on Windows (needs MSYS2), not macOS.
package-windows:
	./package-windows.sh

# Every build target needs the static OpenCV for $(ARCH); fail with the command
# that builds it rather than a pkg-config error from deep inside cgo.
check-opencv:
	@pkg-config --exists opencv5 || { \
		echo "No static OpenCV for $(ARCH) at $(OPENCV_STATIC_PREFIX)"; \
		echo ""; \
		echo "Build it first (once per arch, takes 20-40 min):"; \
		echo "  ARCH=$(ARCH) make opencv-static-darwin"; \
		exit 1; \
	}
