# Trimmed fork of gocv v0.43.0

This is a vendored, **module-trimmed** copy of [gocv](https://gocv.io)
`v0.43.0` (Apache-2.0; see `LICENSE.txt`). The main module points at it with a
`replace` directive:

```
replace gocv.io/x/gocv => ./third_party/gocv
```

## Why it exists

cgo compiles *every* `.cpp` in a package directory, so stock gocv's wrappers
reference `cv::dnn`, `cv::VideoCapture`, `cv::photo`, … whether the app calls
them or not. Those references drag the matching OpenCV modules — most expensively
`dnn` and its `protobuf` dependency — into the statically linked `checkin.exe`.

This app only uses `Mat`, the `imgproc` helpers, and `QRCodeDetector`. So the
fork keeps just the wrappers for the modules we link and **deletes the rest**.
With no wrapper referencing them, those OpenCV modules are dropped from the
custom static build too (see `scripts/build-opencv-static.sh`'s `BUILD_LIST`), taking
~11 MB off the final binary.

## What was changed vs. upstream v0.43.0

- Only the **root package** is vendored (no `contrib/`, `cuda/`, `openvino/`,
  `cmd/`, tests, or sample images).
- `go.mod` is reduced to the module path only — the root package has no
  external (non-stdlib) imports, so upstream's `require`s are dropped.
- **Deleted wrapper files** for unused modules: `aruco`, `asyncarray`,
  `calib3d`, `dnn` (+`dnn_async_openvino`, `dnn_ext`), `features2d`, `highgui`,
  `imgcodecs`, `photo`, `svd`, `video`, `videoio`, `persistence`, and their
  `*_string.go` / `.cpp` / `.h` companions.
- **Kept**: `core`, `imgproc`, `objdetect` (QRCodeDetector), `version`, and the
  shared plumbing (`gocv.go`, `cgo*.go`, `mat_*.go`).

No source in the kept files was modified; the trim is purely by deletion.
Deleting the `calib3d`/`features2d`/`flann` *wrappers* is safe even though those
OpenCV *modules* stay in the build — `objdetect` links them internally for QR
perspective correction; we just don't expose their Go APIs.

## Updating gocv

To move to a newer gocv: re-copy the root package's `.go`/`.cpp`/`.h` files from
the upstream release, re-apply the deletions above, restore this minimal
`go.mod`, and re-run `scripts/build-opencv-static.sh --clean`. The QR decode smoke test
(`internal/camera/qrdecode_test.go`) guards against a trim that breaks decoding.
