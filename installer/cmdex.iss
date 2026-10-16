; Cmdex for Windows: Inno Setup script.
; Built by scripts\build-windows.ps1 (which passes /DAppVersion=x.y.z).

#ifndef AppVersion
  #define AppVersion "0.1.0"
#endif

[Setup]
AppId={{6B2D1F7A-6C1E-4E5B-9A43-0E7C2B1D8F55}
AppName=Cmdex
AppVersion={#AppVersion}
AppVerName=Cmdex {#AppVersion}
AppPublisher=Cmdex
AppPublisherURL=https://github.com/thangkes/Cmdex
VersionInfoVersion={#AppVersion}
DefaultDirName={autopf}\Cmdex
DefaultGroupName=Cmdex
DisableProgramGroupPage=yes
DisableDirPage=auto
OutputDir=..\dist
OutputBaseFilename=Cmdex-Windows-Setup-{#AppVersion}
SetupIconFile=app.ico
UninstallDisplayIcon={app}\Cmdex.exe
UninstallDisplayName=Cmdex
Compression=lzma2/ultra64
SolidCompression=yes
ArchitecturesAllowed=x64compatible
ArchitecturesInstallIn64BitMode=x64compatible
MinVersion=10.0
; A per-user install: no administrator rights, so updates need no UAC prompt.
PrivilegesRequired=lowest
PrivilegesRequiredOverridesAllowed=dialog
WizardStyle=modern
ShowLanguageDialog=no
CloseApplications=yes
RestartApplications=no

[Languages]
Name: "vi"; MessagesFile: "Vietnamese.isl"
Name: "en"; MessagesFile: "compiler:Default.isl"

[CustomMessages]
vi.Launch=Mở Cmdex
en.Launch=Open Cmdex
vi.DesktopIcon=Tạo biểu tượng trên màn hình
en.DesktopIcon=Create a desktop shortcut

[Tasks]
Name: "desktopicon"; Description: "{cm:DesktopIcon}"; Flags: unchecked

[Files]
Source: "..\build\bin\Cmdex.exe"; DestDir: "{app}"; Flags: ignoreversion
Source: "..\LICENSE"; DestDir: "{app}"; DestName: "LICENSE.txt"; Flags: ignoreversion
Source: "..\THIRD-PARTY-NOTICES.txt"; DestDir: "{app}"; Flags: ignoreversion

[Registry]
; "Open Cmdex when I sign in" (Settings) writes this value; uninstalling removes it.
Root: HKCU; Subkey: "Software\Microsoft\Windows\CurrentVersion\Run"; ValueType: none; ValueName: "Cmdex"; Flags: uninsdeletevalue dontcreatekey

[Icons]
Name: "{autoprograms}\Cmdex"; Filename: "{app}\Cmdex.exe"
Name: "{autodesktop}\Cmdex"; Filename: "{app}\Cmdex.exe"; Tasks: desktopicon

[Run]
Filename: "{app}\Cmdex.exe"; Description: "{cm:Launch}"; Flags: nowait postinstall skipifsilent
; A silent run is an update started from Cmdex: open it again afterwards.
Filename: "{app}\Cmdex.exe"; Flags: nowait; Check: WizardSilent

; The library (%APPDATA%\Cmdex) belongs to the user and is kept on uninstall.
