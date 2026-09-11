# Unreleased

Backup support

* Add `backup_dir` and `backup_count` settings
* Each backup is zip of all Center databases + a manifest in the `backup_dir`
* Backup retention: keep `backup_count` recent archives plus one per calendar month
* Add Browse/Restore screen for backups verification and restore
* Add last backup time and Backup Now button to startup screen
* Support renaming and deactivating Centers
* Switch to single top-level log file instead of per-center log files

# 0.0.6

* Switch to QRCodeDetectorAruco. On synthetic tests:
  * 20-30% faster on synthetic images with a QR code
  * 60-80% faster on synthetic images without a QR code
  * Observed slightly lower CPU usage with camera running
* Update to OpenCV 5
* Support cross-compiling to Windows from macOS
* Support full screen "kiosk" mode

# 0.0.5

* Require authorized adult to check in/out each student
* History results name the authorized adult ("... checked in by Jane Smith")
* Scans always show the confirmation pop-up; removed the `confirm_scan` setting
* Use First Name and Last Name fields instead of combined Name field
* Fix QR Code PDF layout
* Remove database pruner. Keep data forever.
* Fix sorting in some views

# 0.0.4

* Added status bar and feedback bar to main view
* Added `confirm_scan` setting to toggle the scan confirmation pop-up
* Added settings window. Changed settings take effect on next startup.
* Added static macOS build (`make package-darwin`) linking slim OpenCV
* Support cross-build for Intel Macs with `ARCH=x86_64`.
* Fixed camera reopen failure due to ignored error from the device
* Sort main list by today's most recent check in/out and then by name
* Show at most one scan pop-up at a time (no stacked pop-ups)
* Fixed camera lag by decoupling capture from QR detection
* Rename Admin menu to File and combine version and license into About menu
* Add Camera menu for camera management
* Open a Center by clicking its name in the startup window (no Open button)
* Fix intermittent crash on click by resuing startup window for main view

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
