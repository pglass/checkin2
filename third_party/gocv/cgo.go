package gocv

// cgo flags for linking OpenCV.
//
// This fork links exactly one way: the slim STATIC OpenCV built by
// build-opencv-static.sh (Windows) or build-opencv-static-darwin.sh (macOS),
// with every flag supplied through the CGO_* environment by opencv-env.sh.
// Upstream gocv ships four mutually exclusive variants of this file selected by
// the `customenv` and `opencvstatic` build tags (pkg-config vs hardcoded paths,
// per platform); all of them are gone. There is one target, so there are no
// build tags to get wrong -- which is what let a bare `go test` fail on Windows
// while succeeding on macOS.
//
// Nothing is declared here on purpose: with no #cgo directives, cgo takes the
// include and link flags entirely from CGO_CPPFLAGS / CGO_CXXFLAGS /
// CGO_LDFLAGS. See opencv-env.sh, which every build and test entry point
// sources.

/*
*/
import "C"
