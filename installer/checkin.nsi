; NSIS installer for Checkin. Built by scripts/build-installer.sh, which is the
; only intended entry point -- it supplies every /D define below and refuses to
; run against an unsigned or missing exe.
;
; Per-user install, on purpose. $LOCALAPPDATA needs no elevation, so neither the
; first install nor any update raises a UAC prompt. That matters more here than
; the tidier look of Program Files: the planned manifest-driven updater launches
; this installer itself, and an update that needs an administrator every time is
; an update people decline. The cost is that the install covers only the user who
; ran it, which is the right trade for a per-station kiosk app.
;
; Upgrades are a plain overwrite rather than uninstall-then-reinstall. The app is
; a single self-contained exe, so no file can be orphaned by a version that stops
; shipping it -- the case that would justify running the old uninstaller first.
; Overwriting also avoids the classic NSIS footgun where the spawned uninstaller
; returns immediately and races the new files it is meant to precede.

Unicode true

; Every define arrives from the build script so that the Makefile's VERSION
; stays the single source of truth. Defaults exist only so the script can be
; compiled by hand while being edited.
!ifndef VERSION
  !define VERSION "0.0.0"
!endif
!ifndef SRC_EXE
  !define SRC_EXE "..\dist\checkin.exe"
!endif
!ifndef OUT_FILE
  !define OUT_FILE "..\dist\checkin-${VERSION}-setup.exe"
!endif
!ifndef ICON
  !define ICON "..\Icon.ico"
!endif

!define APP_NAME    "Checkin"
!define PUBLISHER   "Paul Glass"
!define EXE_NAME    "checkin.exe"
!define REPO_URL    "https://github.com/pglass/checkin2"
; Where Windows lists installed programs. Under HKCU because this is a per-user
; install; a per-machine one would use the same path under HKLM.
!define UNINST_KEY  "Software\Microsoft\Windows\CurrentVersion\Uninstall\${APP_NAME}"

Name "${APP_NAME} ${VERSION}"
OutFile "${OUT_FILE}"
Icon "${ICON}"
UninstallIcon "${ICON}"

; Per-user: no elevation requested, so no UAC prompt. Windows would otherwise
; guess from the filename ("setup") and silently ask for admin anyway.
RequestExecutionLevel user
InstallDir "$LOCALAPPDATA\Programs\${APP_NAME}"
; An existing install wins over the default above, so an upgrade lands where the
; previous version actually went even if the user moved it.
InstallDirRegKey HKCU "Software\${APP_NAME}" "InstallDir"

; LZMA with a solid archive: the payload is one ~53 MB exe, so compression is
; most of the build time and all of the download size. Roughly halves it.
SetCompressor /SOLID lzma
SetCompressorDictSize 64

; Explorer's Properties -> Details for the installer itself. Mirrors the
; versioninfo that scripts/build-windows.sh stamps into the app exe.
VIProductVersion "${VERSION}.0"
VIAddVersionKey "ProductName"     "${APP_NAME}"
VIAddVersionKey "ProductVersion"  "${VERSION}"
VIAddVersionKey "FileVersion"     "${VERSION}"
VIAddVersionKey "FileDescription" "${APP_NAME} Installer"
VIAddVersionKey "CompanyName"     "${PUBLISHER}"
VIAddVersionKey "LegalCopyright"  "${PUBLISHER}"

!include "MUI2.nsh"
!include "LogicLib.nsh"
; GetSize, for the EstimatedSize value Add/Remove Programs displays. FileFunc
; defines its macros only when named, so the !insertmacro below is required
; before ${GetSize} resolves to anything.
!include "FileFunc.nsh"
!insertmacro GetSize

!define MUI_ICON   "${ICON}"
!define MUI_UNICON "${ICON}"
!define MUI_ABORTWARNING
; Offer to launch straight from the last page. Kiosk operators install and
; immediately want to see it come up.
!define MUI_FINISHPAGE_RUN "$INSTDIR\${EXE_NAME}"
!define MUI_FINISHPAGE_RUN_TEXT "Start ${APP_NAME}"

!insertmacro MUI_PAGE_WELCOME
!insertmacro MUI_PAGE_DIRECTORY
!insertmacro MUI_PAGE_INSTFILES
!insertmacro MUI_PAGE_FINISH

!insertmacro MUI_UNPAGE_CONFIRM
!insertmacro MUI_UNPAGE_INSTFILES

!insertmacro MUI_LANGUAGE "English"

; Abort if the app is running. Windows holds an exclusive lock on a running
; exe, so File would fail mid-section and leave a half-written install behind.
; Checking up front turns that into a clear message.
;
; tasklist is used rather than a process-listing plugin so the installer needs
; no plugin beyond the bundled nsExec: one less thing to ship and verify.
;
; The match is decided by `find`, not by inspecting tasklist's output here.
; tasklist prints "INFO: No tasks are running..." rather than nothing when its
; filter matches no process, so a non-empty-output test would always fire; and
; NSIS has no substring operator without pulling in StrFunc.nsh. Piping through
; `find` moves the comparison to a tool that reports it as an exit code: 0 when
; the name was found, non-zero otherwise. nsExec pushes the exit code then the
; output, so both are popped -- leaving either behind corrupts later stack reads.
!macro AbortIfRunning UN
Function ${UN}AbortIfRunning
  nsExec::ExecToStack 'cmd /c tasklist /FI "IMAGENAME eq ${EXE_NAME}" /NH | find /I "${EXE_NAME}"'
  Pop $0
  Pop $1
  ${If} $0 == 0
    MessageBox MB_OK|MB_ICONSTOP \
      "${APP_NAME} is running.$\n$\nClose it and run this installer again."
    Abort
  ${EndIf}
FunctionEnd
!macroend
!insertmacro AbortIfRunning ""
!insertmacro AbortIfRunning "un."

Function .onInit
  Call AbortIfRunning
FunctionEnd

Function un.onInit
  Call un.AbortIfRunning
FunctionEnd

Section "Install"
  SetOutPath "$INSTDIR"
  ; Default, but stated explicitly: replacing the exe in place is the whole
  ; upgrade path, so it should not be silently changed.
  SetOverwrite on
  File "/oname=${EXE_NAME}" "${SRC_EXE}"

  ; Remember the location so the next version's InstallDirRegKey finds it.
  WriteRegStr HKCU "Software\${APP_NAME}" "InstallDir" "$INSTDIR"

  CreateDirectory "$SMPROGRAMS\${APP_NAME}"
  CreateShortcut "$SMPROGRAMS\${APP_NAME}\${APP_NAME}.lnk" "$INSTDIR\${EXE_NAME}"
  CreateShortcut "$DESKTOP\${APP_NAME}.lnk" "$INSTDIR\${EXE_NAME}"

  WriteUninstaller "$INSTDIR\uninstall.exe"

  ; Add/Remove Programs. NSIS writes none of this on its own -- unlike an MSI,
  ; where Windows maintains it -- so every field an unattended uninstall or a
  ; version check might read has to be written here.
  WriteRegStr   HKCU "${UNINST_KEY}" "DisplayName"     "${APP_NAME}"
  WriteRegStr   HKCU "${UNINST_KEY}" "DisplayVersion"  "${VERSION}"
  WriteRegStr   HKCU "${UNINST_KEY}" "DisplayIcon"     "$INSTDIR\${EXE_NAME}"
  WriteRegStr   HKCU "${UNINST_KEY}" "Publisher"       "${PUBLISHER}"
  WriteRegStr   HKCU "${UNINST_KEY}" "URLInfoAbout"    "${REPO_URL}"
  WriteRegStr   HKCU "${UNINST_KEY}" "InstallLocation" "$INSTDIR"
  WriteRegStr   HKCU "${UNINST_KEY}" "UninstallString" '"$INSTDIR\uninstall.exe"'
  WriteRegStr   HKCU "${UNINST_KEY}" "QuietUninstallString" '"$INSTDIR\uninstall.exe" /S'
  ; A per-user install cannot be repaired or modified from Add/Remove, so grey
  ; those buttons out rather than offering something that does nothing.
  WriteRegDWORD HKCU "${UNINST_KEY}" "NoModify" 1
  WriteRegDWORD HKCU "${UNINST_KEY}" "NoRepair" 1
  ; Shown as the install size in Add/Remove. $0 is set by GetSize in KB.
  ${GetSize} "$INSTDIR" "/S=0K" $0 $1 $2
  IntFmt $0 "0x%08X" $0
  WriteRegDWORD HKCU "${UNINST_KEY}" "EstimatedSize" "$0"
SectionEnd

Section "Uninstall"
  Delete "$INSTDIR\${EXE_NAME}"
  Delete "$INSTDIR\uninstall.exe"
  ; RMDir without /r: anything else in there is user data or a file a later
  ; version put down, and a recursive delete of a directory the user may have
  ; pointed somewhere else is how uninstallers destroy databases. An empty
  ; directory goes; a non-empty one is left alone.
  RMDir "$INSTDIR"

  Delete "$SMPROGRAMS\${APP_NAME}\${APP_NAME}.lnk"
  RMDir  "$SMPROGRAMS\${APP_NAME}"
  Delete "$DESKTOP\${APP_NAME}.lnk"

  DeleteRegKey HKCU "${UNINST_KEY}"
  DeleteRegKey HKCU "Software\${APP_NAME}"
SectionEnd
