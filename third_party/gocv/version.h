#ifndef _OPENCV3_VERSION_H_
#define _OPENCV3_VERSION_H_

#ifdef __cplusplus
#include <opencv2/opencv.hpp>
// OpenCV 5 moved contour/shape/transform helpers (approxPolyDP, convexHull,
// getPerspectiveTransform, minAreaRect, moments, ...) out of imgproc into the
// new geometry module, and opencv.hpp does not pull geometry.hpp in. Include it
// explicitly or those names are missing from namespace cv.
#include <opencv2/geometry.hpp>
extern "C" {
#endif

#include "core.h"

const char* openCVVersion();

#ifdef __cplusplus
}
#endif

#endif //_OPENCV3_VERSION_H_
