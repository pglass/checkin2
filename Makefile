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

.PHONY: build run test generate package check-opencv

build: check-opencv
	go build -tags "$(TAGS)" -ldflags="-extldflags=-Wl,-no_warn_duplicate_libraries" ./cmd/checkin/

run: check-opencv
	go run -tags "$(TAGS)" ./cmd/checkin

test:
	go test -tags "$(TAGS)" ./...

generate:
	sqlc generate

# Package a distributable app bundle. NOTE: the resulting binary dynamically
# links opencv@4 dylibs by absolute Homebrew path; see README "Distribution".
package: check-opencv
	fyne package --tags "$(TAGS)" --src ./cmd/checkin --name Checkin --app-id com.pglass.checkin --icon $(CURDIR)/Icon.png

check-opencv:
	@if [ -z "$(OPENCV4_PREFIX)" ]; then \
		echo "opencv@4 not found. Install with: brew install opencv@4"; \
		exit 1; \
	fi
	@pkg-config --exists opencv4 || { echo "opencv4.pc not on PKG_CONFIG_PATH"; exit 1; }
