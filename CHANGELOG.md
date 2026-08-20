# Unreleased

Webcam latency

* Fixed multi-second webcam lag by reading frames flat-out and decoupling
  capture from QR detection.

# 0.0.3

Centers

* Added Centers: separate databases and log files per Center, selected at
  startup. Shared `settings.ini` stays at the top of the app directory.
* Enforce one process per Center via an exclusive lock file, so a second
  instance can't open the same database and diverge its check-in state.

# 0.0.2

Webcam enumeration and selection

* Replaced opencv videocapture with pion/mediadevices
* Vendored and trimmed gocv / opencv, now only for QR code detection
* Strip debug symbols in package executable
* Reduced Windows binary size to around 45 MB

# 0.0.1

Initial version

* Video Capture (using gocv / opencv)
* QR Code detection (using gocv / opencv)
* QR Code generation to pdf
* History retrieval
* Settings file and logging in application directory
