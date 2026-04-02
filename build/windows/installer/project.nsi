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
!define APP_RUN_KEY "Software\Microsoft\Windows\CurrentVersion\Run"

Name "${INFO_PRODUCTNAME}"
OutFile "..\..\..\SnmpLens-${INFO_PRODUCTVERSION}-setup.exe"

; Launch without admin - elevation happens only if user picks "All Users"
RequestExecutionLevel user
InstallDir ""

; Installer compression
SetCompressor /SOLID lzma
SetCompressorDictSize 32

; UI branding
!define MUI_ICON "..\icon.ico"
!define MUI_UNICON "..\icon.ico"
!define MUI_ABORTWARNING
BrandingText "${INFO_PRODUCTNAME} v${INFO_PRODUCTVERSION} - ${INFO_COMPANYNAME}"

; Finish page: run app + link to GitHub
!define MUI_FINISHPAGE_RUN "$INSTDIR\${PRODUCT_EXECUTABLE}"
!define MUI_FINISHPAGE_RUN_TEXT "Launch ${INFO_PRODUCTNAME}"
!define MUI_FINISHPAGE_LINK "Visit ${INFO_PRODUCTNAME} on GitHub"
!define MUI_FINISHPAGE_LINK_LOCATION "https://github.com/Wasabules/SnmpLens"

; =====================================================================
; Variables
; =====================================================================

Var InstallMode      ; "user" or "admin"
Var RadioUser
Var RadioAdmin
Var IsElevated       ; "1" if relaunched with /ALLUSERS

Var ChkDesktop       ; Checkbox: Desktop shortcut
Var ChkStartMenu     ; Checkbox: Start Menu shortcut
Var ChkAutoStart     ; Checkbox: Launch on Windows startup

Var OptDesktop
Var OptStartMenu
Var OptAutoStart

; =====================================================================
; Init - detect previous install / elevated relaunch
; =====================================================================

Function .onInit
    StrCpy $InstallMode "user"
    StrCpy $IsElevated "0"
    StrCpy $OptDesktop "1"
    StrCpy $OptStartMenu "1"
    StrCpy $OptAutoStart "0"

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

    ; Detect existing installation and pre-fill install dir
    ReadRegStr $0 HKCU "${UNINST_KEY}" "InstallLocation"
    ${If} $0 != ""
    ${AndIf} $IsElevated != "1"
        StrCpy $INSTDIR $0
    ${EndIf}
    ReadRegStr $0 HKLM "${UNINST_KEY}" "InstallLocation"
    ${If} $0 != ""
    ${AndIf} $IsElevated != "1"
        StrCpy $INSTDIR $0
        StrCpy $InstallMode "admin"
    ${EndIf}
FunctionEnd

; =====================================================================
; Custom page: Install mode (Current User / All Users)
; =====================================================================

Function InstallModePage
    ; Skip if already elevated
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
        ExecShell "runas" "$EXEPATH" "/ALLUSERS"
        Quit
    ${Else}
        StrCpy $InstallMode "user"
        StrCpy $INSTDIR "$LOCALAPPDATA\${INFO_PRODUCTNAME}"
    ${EndIf}
FunctionEnd

; =====================================================================
; Custom page: Options (shortcuts, autostart)
; =====================================================================

Function OptionsPage
    nsDialogs::Create 1018
    Pop $0

    ${NSD_CreateGroupBox} 10u 5u 280u 105u "Options"
    Pop $0

    ${NSD_CreateCheckBox} 25u 22u 250u 14u "Create desktop shortcut"
    Pop $ChkDesktop
    ${NSD_SetState} $ChkDesktop ${BST_CHECKED}

    ${NSD_CreateCheckBox} 25u 42u 250u 14u "Create Start Menu shortcut"
    Pop $ChkStartMenu
    ${NSD_SetState} $ChkStartMenu ${BST_CHECKED}

    ${NSD_CreateCheckBox} 25u 62u 250u 14u "Launch ${INFO_PRODUCTNAME} on Windows startup"
    Pop $ChkAutoStart

    ${NSD_CreateLabel} 25u 84u 250u 10u "These options can be changed later in Windows settings."
    Pop $0

    nsDialogs::Show
FunctionEnd

Function OptionsPageLeave
    ${NSD_GetState} $ChkDesktop $OptDesktop
    ${NSD_GetState} $ChkStartMenu $OptStartMenu
    ${NSD_GetState} $ChkAutoStart $OptAutoStart
FunctionEnd

; =====================================================================
; Pages
; =====================================================================

!insertmacro MUI_PAGE_WELCOME
Page custom InstallModePage InstallModeLeave
!insertmacro MUI_PAGE_LICENSE "..\..\..\LICENSE"
!insertmacro MUI_PAGE_DIRECTORY
Page custom OptionsPage OptionsPageLeave
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

; =====================================================================
; Install
; =====================================================================

Section "Install"
    ; Kill running instance before install/upgrade
    nsExec::ExecToLog 'taskkill /F /IM "${PRODUCT_EXECUTABLE}"'

    SetOutPath "$INSTDIR"

    ; Application
    File "..\..\..\build\bin\${PRODUCT_EXECUTABLE}"

    ; Uninstaller
    WriteUninstaller "$INSTDIR\uninstall.exe"

    ; Desktop shortcut (optional)
    ${If} $OptDesktop == ${BST_CHECKED}
        CreateShortcut "$DESKTOP\${INFO_PRODUCTNAME}.lnk" "$INSTDIR\${PRODUCT_EXECUTABLE}"
    ${EndIf}

    ; Start Menu shortcut (optional)
    ${If} $OptStartMenu == ${BST_CHECKED}
        CreateDirectory "$SMPROGRAMS\${INFO_PRODUCTNAME}"
        CreateShortcut "$SMPROGRAMS\${INFO_PRODUCTNAME}\${INFO_PRODUCTNAME}.lnk" "$INSTDIR\${PRODUCT_EXECUTABLE}"
        CreateShortcut "$SMPROGRAMS\${INFO_PRODUCTNAME}\Uninstall ${INFO_PRODUCTNAME}.lnk" "$INSTDIR\uninstall.exe"
    ${EndIf}

    ; Auto-start on Windows boot (optional)
    ${If} $OptAutoStart == ${BST_CHECKED}
        ${If} $InstallMode == "admin"
            WriteRegStr HKLM "${APP_RUN_KEY}" "${INFO_PRODUCTNAME}" "$INSTDIR\${PRODUCT_EXECUTABLE}"
        ${Else}
            WriteRegStr HKCU "${APP_RUN_KEY}" "${INFO_PRODUCTNAME}" "$INSTDIR\${PRODUCT_EXECUTABLE}"
        ${EndIf}
    ${EndIf}

    ; Calculate installed size
    ${GetSize} "$INSTDIR" "/S=0K" $0 $1 $2
    IntFmt $0 "0x%08X" $0

    ; Add/Remove Programs registry
    ${If} $InstallMode == "admin"
        WriteRegStr HKLM "${UNINST_KEY}" "DisplayName" "${INFO_PRODUCTNAME}"
        WriteRegStr HKLM "${UNINST_KEY}" "DisplayIcon" "$INSTDIR\${PRODUCT_EXECUTABLE},0"
        WriteRegStr HKLM "${UNINST_KEY}" "DisplayVersion" "${INFO_PRODUCTVERSION}"
        WriteRegStr HKLM "${UNINST_KEY}" "Publisher" "${INFO_COMPANYNAME}"
        WriteRegStr HKLM "${UNINST_KEY}" "URLInfoAbout" "https://github.com/Wasabules/SnmpLens"
        WriteRegStr HKLM "${UNINST_KEY}" "InstallLocation" "$INSTDIR"
        WriteRegStr HKLM "${UNINST_KEY}" "UninstallString" "$\"$INSTDIR\uninstall.exe$\""
        WriteRegDWORD HKLM "${UNINST_KEY}" "EstimatedSize" $0
        WriteRegDWORD HKLM "${UNINST_KEY}" "NoModify" 1
        WriteRegDWORD HKLM "${UNINST_KEY}" "NoRepair" 1
    ${Else}
        WriteRegStr HKCU "${UNINST_KEY}" "DisplayName" "${INFO_PRODUCTNAME}"
        WriteRegStr HKCU "${UNINST_KEY}" "DisplayIcon" "$INSTDIR\${PRODUCT_EXECUTABLE},0"
        WriteRegStr HKCU "${UNINST_KEY}" "DisplayVersion" "${INFO_PRODUCTVERSION}"
        WriteRegStr HKCU "${UNINST_KEY}" "Publisher" "${INFO_COMPANYNAME}"
        WriteRegStr HKCU "${UNINST_KEY}" "URLInfoAbout" "https://github.com/Wasabules/SnmpLens"
        WriteRegStr HKCU "${UNINST_KEY}" "InstallLocation" "$INSTDIR"
        WriteRegStr HKCU "${UNINST_KEY}" "UninstallString" "$\"$INSTDIR\uninstall.exe$\""
        WriteRegDWORD HKCU "${UNINST_KEY}" "EstimatedSize" $0
        WriteRegDWORD HKCU "${UNINST_KEY}" "NoModify" 1
        WriteRegDWORD HKCU "${UNINST_KEY}" "NoRepair" 1
    ${EndIf}
SectionEnd

; =====================================================================
; Uninstall
; =====================================================================

Section "Uninstall"
    ; Kill running instance
    nsExec::ExecToLog 'taskkill /F /IM "${PRODUCT_EXECUTABLE}"'

    ; Files
    Delete "$INSTDIR\${PRODUCT_EXECUTABLE}"
    Delete "$INSTDIR\uninstall.exe"
    RMDir "$INSTDIR"

    ; Shortcuts (try both contexts)
    SetShellVarContext current
    Delete "$SMPROGRAMS\${INFO_PRODUCTNAME}\${INFO_PRODUCTNAME}.lnk"
    Delete "$SMPROGRAMS\${INFO_PRODUCTNAME}\Uninstall ${INFO_PRODUCTNAME}.lnk"
    RMDir "$SMPROGRAMS\${INFO_PRODUCTNAME}"
    Delete "$DESKTOP\${INFO_PRODUCTNAME}.lnk"

    SetShellVarContext all
    Delete "$SMPROGRAMS\${INFO_PRODUCTNAME}\${INFO_PRODUCTNAME}.lnk"
    Delete "$SMPROGRAMS\${INFO_PRODUCTNAME}\Uninstall ${INFO_PRODUCTNAME}.lnk"
    RMDir "$SMPROGRAMS\${INFO_PRODUCTNAME}"
    Delete "$DESKTOP\${INFO_PRODUCTNAME}.lnk"

    ; Auto-start (remove from both)
    DeleteRegValue HKCU "${APP_RUN_KEY}" "${INFO_PRODUCTNAME}"
    DeleteRegValue HKLM "${APP_RUN_KEY}" "${INFO_PRODUCTNAME}"

    ; Registry (remove from both)
    DeleteRegKey HKCU "${UNINST_KEY}"
    DeleteRegKey HKLM "${UNINST_KEY}"
SectionEnd
