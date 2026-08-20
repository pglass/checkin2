# Unreleased

Settings window

* Added a Settings window for editing settings.ini (under Admin on Windows and
  Linux; macOS moves it to the application menu): every setting shows its config
  key, an input, and a description, with per-field validation and a red error
  message for bad input. Saving writes settings.ini in the same format as
  startup. Changed settings take effect the next time the app is started, as
  noted at the bottom of the window.

Camera

* Fixed the camera failing to reopen with "invalid state: driver is already
  opened", which left the app running without a camera until it was restarted.
  A device that is not closed is now closed before being reopened, and a failed
  close is logged instead of being silently dropped. This also affected
  switching directly between two cameras in the device picker.

Main list ordering

* Sorted the main list by today's most recent check-in or check-out, newest
  first, with students who have no activity today following alphabetically.
  The list re-sorts after every check-in, check-out, and student added.

Check-in/out pop-ups

* Show at most one scan pop-up at a time: a QR code scanned while a pop-up is
  already showing is ignored instead of stacking a second one.

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
