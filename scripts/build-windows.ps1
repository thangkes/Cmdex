# Build Cmdex for Windows: icon and version resources, Cmdex.exe, and the
# Inno Setup installer in dist\.
#
#   .\scripts\build-windows.ps1                 # version from VERSION
#   .\scripts\build-windows.ps1 -Version 0.1.0 -NoInstaller
param(
    [string]$Version,
    [switch]$NoInstaller
)
$ErrorActionPreference = "Stop"
$root = Split-Path $PSScriptRoot -Parent
$utf8 = New-Object Text.UTF8Encoding $false
if (-not $Version) { $Version = (Get-Content "$root\VERSION" -Raw).Trim() }
if ($Version -notmatch '^(\d+)\.(\d+)\.(\d+)') { throw "Bad version $Version" }
$num = "$($Matches[1]).$($Matches[2]).$($Matches[3]).0"

Push-Location $root
try {
    Write-Host "== resources ($Version) ==" -ForegroundColor Cyan
    $wr = [IO.File]::ReadAllText("$root\winres\winres.json", $utf8)
    $wr = $wr -replace '"version": "[^"]*"', """version"": ""$num""" `
              -replace '"file_version": "[^"]*"', """file_version"": ""$num""" `
              -replace '"product_version": "[^"]*"', """product_version"": ""$num""" `
              -replace '"FileVersion": "[^"]*"', """FileVersion"": ""$Version""" `
              -replace '"ProductVersion": "[^"]*"', """ProductVersion"": ""$Version"""
    [IO.File]::WriteAllText("$root\winres\winres.json", $wr, $utf8)
    go run ./tools/genicon build/appicon.png installer/app.ico winres/icon.png
    if ($LASTEXITCODE) { throw "genicon failed" }
    $winres = Join-Path (go env GOPATH) "bin\go-winres.exe"
    if (-not (Test-Path $winres)) { go install github.com/tc-hib/go-winres@latest }
    & $winres make --in winres/winres.json --out rsrc --arch amd64
    if ($LASTEXITCODE) { throw "go-winres failed" }

    Write-Host "== tests ==" -ForegroundColor Cyan
    go test ./...
    if ($LASTEXITCODE) { throw "tests failed" }

    Write-Host "== go build ==" -ForegroundColor Cyan
    $env:CGO_ENABLED = "0"
    go build -tags desktop,production -trimpath `
        -ldflags "-w -s -H windowsgui -X github.com/thangkes/cmdex/internal/app.Version=$Version" `
        -o build\bin\Cmdex.exe .
    if ($LASTEXITCODE) { throw "go build failed" }

    if (-not $NoInstaller) {
        Write-Host "== Inno Setup ==" -ForegroundColor Cyan
        $iscc = @("$env:LOCALAPPDATA\Programs\Inno Setup 6\ISCC.exe", "C:\Program Files (x86)\Inno Setup 6\ISCC.exe") | Where-Object { Test-Path $_ } | Select-Object -First 1
        if (-not $iscc) { throw "Inno Setup not found - winget install JRSoftware.InnoSetup" }
        & $iscc /Q "/DAppVersion=$Version" installer\cmdex.iss
        if ($LASTEXITCODE) { throw "ISCC failed ($LASTEXITCODE)" }
        $setup = "$root\dist\Cmdex-Windows-Setup-$Version.exe"
        "Installer: $setup ({0:N1} MB)" -f ((Get-Item $setup).Length / 1MB)
    }
} finally {
    Pop-Location
}
