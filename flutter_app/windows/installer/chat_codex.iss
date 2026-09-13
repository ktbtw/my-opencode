#define AppName "Chat Codex"
#ifndef AppVersion
  #define AppVersion "0.0.0"
#endif
#ifndef AppBuild
  #define AppBuild "0"
#endif
#define AppExeName "chat_codex.exe"

[Setup]
AppId={{B679FF60-EB0E-473C-80AA-4E8295F788E7}
AppName={#AppName}
AppVersion={#AppVersion}
AppPublisher=Chat Codex
AppPublisherURL=https://www.xyapi.top/codex
AppSupportURL=https://www.xyapi.top/codex
DefaultDirName={localappdata}\Programs\Chat Codex
DefaultGroupName=Chat Codex
DisableProgramGroupPage=yes
PrivilegesRequired=lowest
PrivilegesRequiredOverridesAllowed=dialog
ArchitecturesAllowed=x64compatible
ArchitecturesInstallIn64BitMode=x64compatible
OutputDir=..\..\build\windows\installer
OutputBaseFilename=ChatCodex-{#AppVersion}-windows-x64-setup
SetupIconFile=..\runner\resources\app_icon.ico
UninstallDisplayIcon={app}\{#AppExeName}
Compression=lzma2/max
SolidCompression=yes
WizardStyle=modern
CloseApplications=yes
RestartApplications=no
VersionInfoVersion={#AppVersion}.{#AppBuild}
VersionInfoCompany=Chat Codex
VersionInfoDescription=Chat Codex Windows Installer
VersionInfoProductName={#AppName}
VersionInfoProductVersion={#AppVersion}

[Tasks]
Name: "desktopicon"; Description: "创建桌面快捷方式"; GroupDescription: "附加任务："; Flags: unchecked

[Files]
Source: "..\..\build\windows\x64\runner\Release\*"; DestDir: "{app}"; Flags: ignoreversion recursesubdirs createallsubdirs

[Icons]
Name: "{group}\Chat Codex"; Filename: "{app}\{#AppExeName}"
Name: "{autodesktop}\Chat Codex"; Filename: "{app}\{#AppExeName}"; Tasks: desktopicon

[Run]
Filename: "{app}\{#AppExeName}"; Description: "启动 Chat Codex"; Flags: nowait postinstall skipifsilent

[Code]
function PrepareToInstall(var NeedsRestart: Boolean): String;
var
  ResultCode: Integer;
begin
  { The old client may still hold chat_codex.exe when upgrading from a
    version that does not exit after launching the installer. }
  Exec(ExpandConstant('{cmd}'), '/C taskkill /F /IM chat_codex.exe /T', '',
    SW_HIDE, ewWaitUntilTerminated, ResultCode);
  Result := '';
end;
