; VKVM Windows installer. Build from the repository root after `go build`
; has written dist\vkvm.exe and `go run ./packaging/icon dist\icon` has
; written the icon:
;
;   makensis /DAPP_VERSION=x.y.z packaging\windows\vkvm.nsi
;
; VKVM runs as a normal per-user app (tray + window). The installer only
; needs admin rights for Program Files and the firewall rule, so the app
; itself never has to ask for elevation.
Unicode true
!include MUI2.nsh
!include LogicLib.nsh
!include WordFunc.nsh

!define APP_NAME "VKVM"
!ifndef APP_VERSION
  !define APP_VERSION "0.0.0"
!endif
!define UNINST_KEY "Software\Microsoft\Windows\CurrentVersion\Uninstall\VKVM"
!define RUN_KEY "Software\Microsoft\Windows\CurrentVersion\Run"
!define FW_RULE "VKVM"

Name "${APP_NAME}"
OutFile "..\..\VKVM-Setup.exe"
InstallDir "$PROGRAMFILES64\VKVM"
InstallDirRegKey HKLM "Software\VKVM" "InstallDir"
RequestExecutionLevel admin
BrandingText "VKVM ${APP_VERSION}"
VIProductVersion "${APP_VERSION}.0"
VIAddVersionKey /LANG=1033 "ProductName" "VKVM"
VIAddVersionKey /LANG=1033 "ProductVersion" "${APP_VERSION}"
VIAddVersionKey /LANG=1033 "FileVersion" "${APP_VERSION}.0"
VIAddVersionKey /LANG=1033 "FileDescription" "VKVM monitor switcher setup"
VIAddVersionKey /LANG=1033 "CompanyName" "VKVM"
VIAddVersionKey /LANG=1033 "LegalCopyright" "MIT License"

!define MUI_ICON "..\..\dist\icon\vkvm.ico"
!define MUI_UNICON "..\..\dist\icon\vkvm.ico"
!define MUI_ABORTWARNING
!define MUI_FINISHPAGE_RUN
!define MUI_FINISHPAGE_RUN_TEXT "Open VKVM now"
!define MUI_FINISHPAGE_RUN_FUNCTION LaunchUnelevated

!insertmacro MUI_PAGE_WELCOME
!insertmacro MUI_PAGE_DIRECTORY
!insertmacro MUI_PAGE_INSTFILES
!insertmacro MUI_PAGE_FINISH
!insertmacro MUI_UNPAGE_CONFIRM
!insertmacro MUI_UNPAGE_INSTFILES
!insertmacro MUI_LANGUAGE "English"

; The installer is elevated; start VKVM through Explorer so it runs as the
; signed-in user, not as administrator.
Function LaunchUnelevated
  Exec '"$WINDIR\explorer.exe" "$INSTDIR\vkvm.exe"'
FunctionEnd

; The 0.2.x installer stored the folder as "...\\VKVM" (a literal double
; backslash). Normalise it so the firewall rule matches the real path.
Function .onInit
  ${WordReplace} "$INSTDIR" "\\" "\" "+" $INSTDIR
FunctionEnd

; Stop a running VKVM and remove what VKVM 0.2.x installed: the VKVMCore
; service, the --companion startup entry and the port-only firewall rule.
!macro StopVKVM
  nsExec::Exec '"$SYSDIR\taskkill.exe" /F /IM vkvm.exe'
  Pop $0
  nsExec::Exec '"$SYSDIR\sc.exe" stop VKVMCore'
  Pop $0
  nsExec::Exec '"$SYSDIR\sc.exe" delete VKVMCore'
  Pop $0
  Sleep 800
!macroend

Section "VKVM" SEC_MAIN
  SetShellVarContext current
  !insertmacro StopVKVM
  ${WordReplace} "$INSTDIR" "\\" "\" "+" $INSTDIR

  SetOutPath "$INSTDIR"
  File "..\..\dist\vkvm.exe"
  File "..\..\dist\icon\vkvm.ico"

  ; Shared configuration store (kept from earlier versions).
  ReadEnvStr $1 "ProgramData"
  ${If} $1 != ""
    CreateDirectory "$1\VKVM"
    nsExec::Exec '"$SYSDIR\icacls.exe" "$1\VKVM" /grant *S-1-5-32-545:(OI)(CI)M /inheritance:e'
    Pop $0
  ${EndIf}

  ; Allow VKVM through the firewall so the two computers can find and reach
  ; each other. Every VKVM network request is authenticated, and the rule is
  ; limited to vkvm.exe. Home Wi-Fi is often classed as "Public", so the rule
  ; covers all profiles.
  nsExec::Exec '"$SYSDIR\netsh.exe" advfirewall firewall delete rule name="${FW_RULE}"'
  Pop $0
  nsExec::Exec '"$SYSDIR\netsh.exe" advfirewall firewall delete rule name="VKVM Remote Switch"'
  Pop $0
  nsExec::ExecToLog '"$SYSDIR\netsh.exe" advfirewall firewall add rule name="${FW_RULE}" dir=in action=allow program="$INSTDIR\vkvm.exe" enable=yes profile=any'
  Pop $0

  ; The app adds its own startup entry when "Start VKVM when I log in" is on
  ; (the default in the setup window). Drop the old --companion entry.
  DeleteRegValue HKCU "${RUN_KEY}" "VKVM"

  CreateDirectory "$SMPROGRAMS\VKVM"
  CreateShortCut "$SMPROGRAMS\VKVM\VKVM.lnk" "$INSTDIR\vkvm.exe" "" "$INSTDIR\vkvm.ico" 0
  CreateShortCut "$DESKTOP\VKVM.lnk" "$INSTDIR\vkvm.exe" "" "$INSTDIR\vkvm.ico" 0

  WriteUninstaller "$INSTDIR\uninstall.exe"
  WriteRegStr HKLM "Software\VKVM" "InstallDir" "$INSTDIR"
  WriteRegStr HKLM "${UNINST_KEY}" "DisplayName" "VKVM"
  WriteRegStr HKLM "${UNINST_KEY}" "DisplayVersion" "${APP_VERSION}"
  WriteRegStr HKLM "${UNINST_KEY}" "DisplayIcon" "$INSTDIR\vkvm.ico"
  WriteRegStr HKLM "${UNINST_KEY}" "Publisher" "VKVM"
  WriteRegStr HKLM "${UNINST_KEY}" "InstallLocation" "$INSTDIR"
  WriteRegStr HKLM "${UNINST_KEY}" "UninstallString" '"$INSTDIR\uninstall.exe"'
  WriteRegDWORD HKLM "${UNINST_KEY}" "NoModify" 1
  WriteRegDWORD HKLM "${UNINST_KEY}" "NoRepair" 1
SectionEnd

Section "Uninstall"
  SetShellVarContext current
  !insertmacro StopVKVM
  nsExec::Exec '"$SYSDIR\netsh.exe" advfirewall firewall delete rule name="${FW_RULE}"'
  Pop $0
  DeleteRegValue HKCU "${RUN_KEY}" "VKVM"
  DeleteRegKey HKLM "${UNINST_KEY}"
  DeleteRegKey HKLM "Software\VKVM"
  Delete "$SMPROGRAMS\VKVM\VKVM.lnk"
  RMDir "$SMPROGRAMS\VKVM"
  Delete "$DESKTOP\VKVM.lnk"
  Delete "$INSTDIR\vkvm.exe"
  Delete "$INSTDIR\vkvm.ico"
  Delete "$INSTDIR\install.log"
  Delete "$INSTDIR\uninstall.exe"
  RMDir "$INSTDIR"
SectionEnd
