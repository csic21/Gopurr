; Same per-user identity, install directory, shortcuts and protocols as mygo.
Unicode true
ManifestDPIAware true
SetCompressor /SOLID lzma
Name "Gopurr"
OutFile "${OUTPUT}"
InstallDir "$LOCALAPPDATA\Programs\Gopurr"
RequestExecutionLevel user
BrandingText " "
!define MUI_ICON "${PAYLOAD}\Gopurr.ico"
!define MUI_UNICON "${PAYLOAD}\Gopurr.ico"
!define MUI_FINISHPAGE_RUN "$INSTDIR\Gopurr.exe"
!define MUI_FINISHPAGE_SHOWREADME
!define MUI_FINISHPAGE_SHOWREADME_TEXT "Create a desktop shortcut"
!define MUI_FINISHPAGE_SHOWREADME_FUNCTION CreateDesktopShortcut
!include "MUI2.nsh"
!insertmacro MUI_PAGE_DIRECTORY
!insertmacro MUI_PAGE_INSTFILES
!insertmacro MUI_PAGE_FINISH
!insertmacro MUI_UNPAGE_CONFIRM
!insertmacro MUI_UNPAGE_INSTFILES
!insertmacro MUI_LANGUAGE "English"
!insertmacro MUI_LANGUAGE "SimpChinese"

; Abort rather than overwrite mapped binaries or force-kill a proxy owner.
; NSH's private singleton requests a complete proxy/core shutdown first.
!macro CloseApp PREFIX
Function ${PREFIX}CloseApp
  IfFileExists "$INSTDIR\native-build.json" 0 check
  ExecWait '"$INSTDIR\Gopurr.exe" --native-quit'
  check:
  InitPluginsDir
  File /oname=$PLUGINSDIR\check-app.ps1 "${CHECK_SCRIPT}"
  System::Call 'Kernel32::SetEnvironmentVariable(t "GOPURR_INSTALL_DIR", t "$INSTDIR")'
  StrCpy $0 "$WINDIR\Sysnative\WindowsPowerShell\v1.0\powershell.exe"
  IfFileExists $0 +2
  StrCpy $0 "$SYSDIR\WindowsPowerShell\v1.0\powershell.exe"
  nsExec::Exec '"$0" -NoProfile -NonInteractive -ExecutionPolicy Bypass -File "$PLUGINSDIR\check-app.ps1"'
  Pop $1
  StrCmp $1 "0" done
  MessageBox MB_OK|MB_ICONSTOP "Quit Gopurr from its tray menu, then retry. Its network state must be restored before replacing application files." /SD IDOK
  SetErrorLevel 2
  Quit
  done:
FunctionEnd
!macroend
!insertmacro CloseApp ""
!insertmacro CloseApp "un."

Section
  Call CloseApp
  SetOutPath "$INSTDIR"
  File /r "${PAYLOAD}\*"
  WriteUninstaller "$INSTDIR\Uninstall.exe"
  CreateShortCut "$SMPROGRAMS\Gopurr.lnk" "$INSTDIR\Gopurr.exe"
  WriteRegStr HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\io.github.gopurr.app" "DisplayName" "Gopurr"
  WriteRegStr HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\io.github.gopurr.app" "DisplayVersion" "${VERSION}"
  WriteRegStr HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\io.github.gopurr.app" "Publisher" "Gopurr"
  WriteRegStr HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\io.github.gopurr.app" "DisplayIcon" "$INSTDIR\Gopurr.exe"
  WriteRegStr HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\io.github.gopurr.app" "InstallLocation" "$INSTDIR"
  WriteRegStr HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\io.github.gopurr.app" "UninstallString" '$"$INSTDIR\Uninstall.exe$"'
  WriteRegStr HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\io.github.gopurr.app" "QuietUninstallString" '$"$INSTDIR\Uninstall.exe$" /S'
  WriteRegDWORD HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\io.github.gopurr.app" "NoModify" 1
  WriteRegDWORD HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\io.github.gopurr.app" "NoRepair" 1
  WriteRegStr HKCU "Software\Classes\gopurr" "" "URL:Gopurr"
  WriteRegStr HKCU "Software\Classes\gopurr" "URL Protocol" ""
  WriteRegStr HKCU "Software\Classes\gopurr\shell\open\command" "" '$"$INSTDIR\Gopurr.exe$" $"%1$"'
  WriteRegStr HKCU "Software\Classes\clash" "" "URL:Gopurr"
  WriteRegStr HKCU "Software\Classes\clash" "URL Protocol" ""
  WriteRegStr HKCU "Software\Classes\clash\shell\open\command" "" '$"$INSTDIR\Gopurr.exe$" $"%1$"'
  System::Call 'shell32::SHChangeNotify(i 0x08000000, i 0, p 0, p 0)'
SectionEnd
Function CreateDesktopShortcut
  SetOutPath "$INSTDIR"
  CreateShortCut "$DESKTOP\Gopurr.lnk" "$INSTDIR\Gopurr.exe"
FunctionEnd
Section "Uninstall"
  Call un.CloseApp
  Delete "$SMPROGRAMS\Gopurr.lnk"
  Delete "$DESKTOP\Gopurr.lnk"
  DeleteRegKey HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\io.github.gopurr.app"
  DeleteRegKey HKCU "Software\Classes\gopurr"
  DeleteRegKey HKCU "Software\Classes\clash"
  System::Call 'shell32::SHChangeNotify(i 0x08000000, i 0, p 0, p 0)'
  ; %APPDATA%\Gopurr, migration snapshots and the managed service are
  ; intentionally retained, matching the established installer contract.
  RMDir /r "$INSTDIR"
SectionEnd
