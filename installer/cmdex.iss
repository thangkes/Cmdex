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
; Adding Cmdex's folder to PATH tells open programs to read it again.
ChangesEnvironment=yes
RestartApplications=no

[Languages]
Name: "vi"; MessagesFile: "Vietnamese.isl"
Name: "en"; MessagesFile: "compiler:Default.isl"

[CustomMessages]
vi.Launch=Mở Cmdex
en.Launch=Open Cmdex
vi.DesktopIcon=Tạo biểu tượng trên màn hình
en.DesktopIcon=Create a desktop shortcut
vi.AddPath=Thêm Cmdex vào PATH (gõ lệnh cmdex trong terminal)
en.AddPath=Add Cmdex to PATH (type cmdex in a terminal)

[Tasks]
Name: "desktopicon"; Description: "{cm:DesktopIcon}"; Flags: unchecked
Name: "addpath"; Description: "{cm:AddPath}"

[Files]
Source: "..\build\bin\Cmdex.exe"; DestDir: "{app}"; Flags: ignoreversion
; cmdex.com: what `cmdex` runs in a terminal (it waits for Cmdex.exe)
Source: "..\build\bin\cmdex.com"; DestDir: "{app}"; Flags: ignoreversion
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

[Code]
// PATH: the user's (a per-user install) or the machine's (an install for
// everyone). Only Cmdex's own folder is added or removed; every other entry,
// %VARIABLES% included, is written back as it was.
function EnvRoot: Integer;
begin
  if IsAdminInstallMode then Result := HKEY_LOCAL_MACHINE else Result := HKEY_CURRENT_USER;
end;

function EnvKey: String;
begin
  if IsAdminInstallMode then Result := 'SYSTEM\CurrentControlSet\Control\Session Manager\Environment'
  else Result := 'Environment';
end;

// WithoutDir takes every copy of Dir (with or without a final \) out of
// Paths and leaves the rest exactly as it was, empty entries included.
function WithoutDir(Paths, Dir: String; var Found: Boolean): String;
var
  S, Pat: String;
  I, P: Integer;
begin
  S := ';' + Paths + ';';
  Found := False;
  for I := 0 to 1 do begin
    if I = 0 then Pat := ';' + Dir + ';' else Pat := ';' + Dir + '\;';
    repeat
      P := Pos(Uppercase(Pat), Uppercase(S));
      if P > 0 then begin
        Delete(S, P, Length(Pat) - 1); // keeps the ";" that follows
        Found := True;
      end;
    until P = 0;
  end;
  Result := Copy(S, 2, Length(S) - 2);
end;

// EditPath adds Dir at the end of PATH (Add) or takes it out again, so that
// installing then uninstalling gives back the very same PATH.
procedure EditPath(Dir: String; Add: Boolean);
var
  Paths, Rest: String;
  Found: Boolean;
begin
  if not RegQueryStringValue(EnvRoot, EnvKey, 'Path', Paths) then Paths := '';
  Rest := WithoutDir(Paths, Dir, Found);
  if Add and not Found then begin
    if Paths = '' then Rest := Dir
    else if Copy(Paths, Length(Paths), 1) = ';' then Rest := Paths + Dir + ';'
    else Rest := Paths + ';' + Dir;
    RegWriteExpandStringValue(EnvRoot, EnvKey, 'Path', Rest);
  end else if not Add and Found then
    RegWriteExpandStringValue(EnvRoot, EnvKey, 'Path', Rest);
end;

procedure CurStepChanged(CurStep: TSetupStep);
begin
  if CurStep = ssPostInstall then
    EditPath(ExpandConstant('{app}'), WizardIsTaskSelected('addpath'));
end;

procedure CurUninstallStepChanged(CurUninstallStep: TUninstallStep);
begin
  if CurUninstallStep = usPostUninstall then
    EditPath(ExpandConstant('{app}'), False);
end;
