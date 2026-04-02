Unicode true

!include "MUI2.nsh"
!include "x64.nsh"
!include "nsDialogs.nsh"
!include "LogicLib.nsh"
!include "FileFunc.nsh"
!include "WinMessages.nsh"

!define INFO_PRODUCTNAME "SnmpLens"
!define INFO_COMPANYNAME "Geoffrey Lecoq"
!define INFO_PRODUCTVERSION "1.0.0"
!define INFO_COPYRIGHT "(c) 2026 Geoffrey Lecoq"
!define PRODUCT_EXECUTABLE "${INFO_PRODUCTNAME}.exe"
!define UNINST_KEY "Software\Microsoft\Windows\CurrentVersion\Uninstall\${INFO_PRODUCTNAME}"

Name "${INFO_PRODUCTNAME}"
OutFile "..\..\..\SnmpLens-${INFO_PRODUCTVERSION}-setup.exe"

; Launch without admin — elevation happens only if user picks "All Users"
RequestExecutionLevel user
InstallDir ""

; UI
!define MUI_ICON "..\icon.ico"
!define MUI_UNICON "..\icon.ico"
!define MUI_ABORTWARNING
!define MUI_FINISHPAGE_RUN "$INSTDIR\${PRODUCT_EXECUTABLE}"

; ─────────────────────────────────────────────────────────────
; Variables
; ─────────────────────────────────────────────────────────────

Var InstallMode      ; "user" or "admin"
Var RadioUser
Var RadioAdmin
Var IsElevated       ; "1" if relaunched with /ALLUSERS

; ─────────────────────────────────────────────────────────────
; Init — detect if we were relaunched elevated
; ─────────────────────────────────────────────────────────────

Function .onInit
    StrCpy $InstallMode "user"
    StrCpy $IsElevated "0"

    ; Check if relaunched with /ALLUSERS flag (elevated process)
    ${GetParameters} $0
    ${GetOptions} $0 "/ALLUSERS" $1
    ${IfNot} ${Errors}
        StrCpy $InstallMode "admin"
        StrCpy $IsElevated "1"
        SetShellVarContext all
        StrCpy $INSTDIR "$PROGRAMFILES\${INFO_PRODUCTNAME}"
    ${Else}
        StrCpy $INSTDIR "$LOCALAPPDATA\${INFO_PRODUCTNAME}"
    ${EndIf}
FunctionEnd

; ─────────────────────────────────────────────────────────────
; Custom page: choose Current User / All Users
; ─────────────────────────────────────────────────────────────

Function InstallModePage
    ; Skip this page if we're already elevated (relaunched)
    ${If} $IsElevated == "1"
        Abort
    ${EndIf}

    nsDialogs::Create 1018
    Pop $0

    ${NSD_CreateGroupBox} 10u 5u 280u 85u "Installation mode"
    Pop $0

    ${NSD_CreateRadioButton} 25u 22u 250u 14u "Current user only - no admin rights needed"
    Pop $RadioUser
    ${NSD_SetState} $RadioUser ${BST_CHECKED}

    ${NSD_CreateLabel} 40u 38u 240u 10u "Installs to: $LOCALAPPDATA\${INFO_PRODUCTNAME}"
    Pop $0

    ${NSD_CreateRadioButton} 25u 55u 250u 14u "All users - requires administrator rights"
    Pop $RadioAdmin

    ${NSD_CreateLabel} 40u 71u 240u 10u "Installs to: $PROGRAMFILES\${INFO_PRODUCTNAME}"
    Pop $0

    nsDialogs::Show
FunctionEnd

Function InstallModeLeave
    ${NSD_GetState} $RadioAdmin $0
    ${If} $0 == ${BST_CHECKED}
        ; Relaunch this installer elevated with /ALLUSERS flag
        ExecShell "runas" "$EXEPATH" "/ALLUSERS"
        Quit
    ${Else}
        StrCpy $InstallMode "user"
        StrCpy $INSTDIR "$LOCALAPPDATA\${INFO_PRODUCTNAME}"
    ${EndIf}
FunctionEnd

; ─────────────────────────────────────────────────────────────
; Pages
; ─────────────────────────────────────────────────────────────

!insertmacro MUI_PAGE_WELCOME
Page custom InstallModePage InstallModeLeave
!insertmacro MUI_PAGE_DIRECTORY
!insertmacro MUI_PAGE_INSTFILES
!insertmacro MUI_PAGE_FINISH

!insertmacro MUI_UNPAGE_CONFIRM
!insertmacro MUI_UNPAGE_INSTFILES

; Languages
!insertmacro MUI_LANGUAGE "English"
!insertmacro MUI_LANGUAGE "French"
!insertmacro MUI_LANGUAGE "German"
!insertmacro MUI_LANGUAGE "Spanish"
!insertmacro MUI_LANGUAGE "SimpChinese"

; ─────────────────────────────────────────────────────────────
; Install
; ─────────────────────────────────────────────────────────────

Section "Install"
    SetOutPath "$INSTDIR"

    ; Application
    File "..\..\..\build\bin\${PRODUCT_EXECUTABLE}"

    ; Uninstaller
    WriteUninstaller "$INSTDIR\uninstall.exe"

    ; Start menu
    CreateDirectory "$SMPROGRAMS\${INFO_PRODUCTNAME}"
    CreateShortcut "$SMPROGRAMS\${INFO_PRODUCTNAME}\${INFO_PRODUCTNAME}.lnk" "$INSTDIR\${PRODUCT_EXECUTABLE}"
    CreateShortcut "$SMPROGRAMS\${INFO_PRODUCTNAME}\Uninstall.lnk" "$INSTDIR\uninstall.exe"

    ; Desktop
    CreateShortcut "$DESKTOP\${INFO_PRODUCTNAME}.lnk" "$INSTDIR\${PRODUCT_EXECUTABLE}"

    ; Add/Remove Programs registry
    ${If} $InstallMode == "admin"
        WriteRegStr HKLM "${UNINST_KEY}" "DisplayName" "${INFO_PRODUCTNAME}"
        WriteRegStr HKLM "${UNINST_KEY}" "DisplayIcon" "$INSTDIR\${PRODUCT_EXECUTABLE}"
        WriteRegStr HKLM "${UNINST_KEY}" "DisplayVersion" "${INFO_PRODUCTVERSION}"
        WriteRegStr HKLM "${UNINST_KEY}" "Publisher" "${INFO_COMPANYNAME}"
        WriteRegStr HKLM "${UNINST_KEY}" "InstallLocation" "$INSTDIR"
        WriteRegStr HKLM "${UNINST_KEY}" "UninstallString" "$\"$INSTDIR\uninstall.exe$\""
        WriteRegDWORD HKLM "${UNINST_KEY}" "NoModify" 1
        WriteRegDWORD HKLM "${UNINST_KEY}" "NoRepair" 1
    ${Else}
        WriteRegStr HKCU "${UNINST_KEY}" "DisplayName" "${INFO_PRODUCTNAME}"
        WriteRegStr HKCU "${UNINST_KEY}" "DisplayIcon" "$INSTDIR\${PRODUCT_EXECUTABLE}"
        WriteRegStr HKCU "${UNINST_KEY}" "DisplayVersion" "${INFO_PRODUCTVERSION}"
        WriteRegStr HKCU "${UNINST_KEY}" "Publisher" "${INFO_COMPANYNAME}"
        WriteRegStr HKCU "${UNINST_KEY}" "InstallLocation" "$INSTDIR"
        WriteRegStr HKCU "${UNINST_KEY}" "UninstallString" "$\"$INSTDIR\uninstall.exe$\""
        WriteRegDWORD HKCU "${UNINST_KEY}" "NoModify" 1
        WriteRegDWORD HKCU "${UNINST_KEY}" "NoRepair" 1
    ${EndIf}
SectionEnd

; ─────────────────────────────────────────────────────────────
; Uninstall
; ─────────────────────────────────────────────────────────────

Section "Uninstall"
    ; Files
    Delete "$INSTDIR\${PRODUCT_EXECUTABLE}"
    Delete "$INSTDIR\uninstall.exe"
    RMDir "$INSTDIR"

    ; Shortcuts (try both contexts)
    SetShellVarContext current
    Delete "$SMPROGRAMS\${INFO_PRODUCTNAME}\${INFO_PRODUCTNAME}.lnk"
    Delete "$SMPROGRAMS\${INFO_PRODUCTNAME}\Uninstall.lnk"
    RMDir "$SMPROGRAMS\${INFO_PRODUCTNAME}"
    Delete "$DESKTOP\${INFO_PRODUCTNAME}.lnk"

    SetShellVarContext all
    Delete "$SMPROGRAMS\${INFO_PRODUCTNAME}\${INFO_PRODUCTNAME}.lnk"
    Delete "$SMPROGRAMS\${INFO_PRODUCTNAME}\Uninstall.lnk"
    RMDir "$SMPROGRAMS\${INFO_PRODUCTNAME}"
    Delete "$DESKTOP\${INFO_PRODUCTNAME}.lnk"

    ; Registry (try both)
    DeleteRegKey HKCU "${UNINST_KEY}"
    DeleteRegKey HKLM "${UNINST_KEY}"
SectionEnd
