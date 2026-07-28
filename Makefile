# Build helpers for the checkin app.
#
# gocv links against OpenCV 4 via pkg-config. Homebrew's default `opencv`
# formula is now OpenCV 5 (incompatible with gocv), so we use the keg-only
# `opencv@4` formula and point pkg-config at it here.

OPENCV4_PREFIX := $(shell brew --prefix opencv@4 2>/dev/null)
export PKG_CONFIG_PATH := $(OPENCV4_PREFIX)/lib/pkgconfig:$(PKG_CONFIG_PATH)
export CGO_ENABLED := 1

# migrated_fynedo asserts that all UI mutations happen on the main goroutine or
# inside fyne.Do (see internal/ui/camera.go and holdbutton.go). It silences
# Fyne 2.8's migration warning and opts into the future default behaviour.
TAGS := migrated_fynedo

# All Go sources: the checkin binary rebuilds only when one of these changes.
GO_SOURCES := $(shell find . -name '*.go')

.PHONY: run test generate package package-windows package-windows-static opencv-static check-opencv seed

# check-opencv is an order-only prerequisite (after the |) so it gates the build
# without forcing a rebuild on every invocation.
checkin: $(GO_SOURCES) | check-opencv
	go build -tags "$(TAGS)" -ldflags="-extldflags=-Wl,-no_warn_duplicate_libraries" -o checkin ./cmd/checkin/

run: checkin
	./checkin -log-level DEBUG -log-file -

test:
	go test -tags "$(TAGS)" ./...

# Build the seed tool (no OpenCV needed; it only touches the DB).
seed:
	go build -o seed ./cmd/seed/

generate:
	sqlc generate

# Package a distributable app bundle. NOTE: the resulting binary dynamically
# links opencv@4 dylibs by absolute Homebrew path; see README "Distribution".
package: check-opencv
	fyne package --tags "$(TAGS)" --src ./cmd/checkin --name Checkin --app-id com.pglass.checkin --icon $(CURDIR)/Icon.png

# Windows packaging: build checkin.exe and zip it with its MinGW DLLs.
# These targets just shell out to the scripts; meant to be run from Git Bash on
# Windows (needs MSYS2), not macOS. Kept here as a convenience.
package-windows:
	./package-windows.sh

# One-time (per OpenCV version): build the slim static OpenCV the static exe links.
opencv-static:
	./build-opencv-static.sh

# Single self-contained checkin.exe (no bundled DLLs). Requires `make opencv-static`
# to have been run once first.
package-windows-static:
	./package-windows.sh --static

check-opencv:
	@if [ -z "$(OPENCV4_PREFIX)" ]; then \
		echo "opencv@4 not found. Install with: brew install opencv@4"; \
		exit 1; \
	fi
	@pkg-config --exists opencv4 || { echo "opencv4.pc not on PKG_CONFIG_PATH"; exit 1; }
