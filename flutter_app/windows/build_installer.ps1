param(
  [string]$BaseUrl = "https://www.xyapi.top/codex"
)

$ErrorActionPreference = "Stop"
$projectRoot = Split-Path -Parent $PSScriptRoot
$installerScript = Join-Path $PSScriptRoot "installer\chat_codex.iss"
$releaseExe = Join-Path $projectRoot "build\windows\x64\runner\Release\chat_codex.exe"
$pubspec = Join-Path $projectRoot "pubspec.yaml"

$versionLine = Get-Content $pubspec | Where-Object { $_ -match '^version:\s*([^+\s]+)\+(\d+)\s*$' } | Select-Object -First 1
if (-not $versionLine -or $versionLine -notmatch '^version:\s*([^+\s]+)\+(\d+)\s*$') {
  throw "Could not read version from $pubspec"
}
$appVersion = $Matches[1]
$appBuild = $Matches[2]

Push-Location $projectRoot
try {
  flutter pub get
  dart run flutter_launcher_icons
  flutter build windows --release --dart-define="CHAT_CODEX_DEFAULT_BASE_URL=$BaseUrl"

  if (-not (Test-Path $releaseExe)) {
    throw "Release executable was not produced: $releaseExe"
  }

  $isccCandidates = @(
    (Join-Path $env:LOCALAPPDATA "Programs\Inno Setup 6\ISCC.exe"),
    (Join-Path ${env:ProgramFiles(x86)} "Inno Setup 6\ISCC.exe"),
    (Join-Path $env:ProgramFiles "Inno Setup 6\ISCC.exe")
  )
  $iscc = $isccCandidates | Where-Object { Test-Path $_ } | Select-Object -First 1
  if (-not $iscc) {
    throw "Inno Setup 6 compiler (ISCC.exe) was not found."
  }

  & $iscc "/DAppVersion=$appVersion" "/DAppBuild=$appBuild" $installerScript
  if ($LASTEXITCODE -ne 0) {
    throw "Inno Setup failed with exit code $LASTEXITCODE."
  }
} finally {
  Pop-Location
}
