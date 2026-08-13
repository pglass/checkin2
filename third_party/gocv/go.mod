// Trimmed fork of gocv.io/x/gocv v0.43.0. Only the root package is vendored,
// and only the wrapper files this app uses (core, imgproc, objdetect) are kept;
// wrappers for unused OpenCV modules (dnn, video, photo, videoio, imgcodecs,
// highgui, calib3d, features2d, aruco, svd, ...) were deleted so their OpenCV
// modules are not linked into the binary. See build-opencv-static.sh BUILD_LIST.
module gocv.io/x/gocv

go 1.21
