# Signs a published release: waits for GitHub Actions to add the Linux
# archive and its checksum, checks that every file listed in the release's
# SHA256SUMS.txt is the one built (the Windows installer in dist\, the Linux
# archive downloaded), then signs SHA256SUMS.txt with the release key
# (tools/sign) and uploads SHA256SUMS.txt.sig. Updaters from 0.1.6 on
# install nothing without that signature.
#
#   .\scripts\sign-release.ps1 0.1.6            (release.ps1 runs it)
#   .\scripts\sign-release.ps1 0.1.6 -NoUpload  everything but the upload (a check)
param(
    [Parameter(Mandatory = $true)][string]$Version,
    [switch]$NoUpload
)
$ErrorActionPreference = "Stop"
$root = Split-Path $PSScriptRoot -Parent
$gh = "C:\Program Files\GitHub CLI\gh.exe"
if (-not (Test-Path $gh)) { $gh = "gh" }
$tag = "v$Version"

Push-Location $root
try {
    Write-Host "== waiting for the Linux build of $tag ==" -ForegroundColor Cyan
    $deadline = (Get-Date).AddMinutes(30)
    $run = $null
    while ((Get-Date) -lt $deadline) {
        $runs = & $gh run list --workflow release-linux.yml --limit 10 --json databaseId,headBranch,status,conclusion | ConvertFrom-Json
        $run = $runs | Where-Object { $_.headBranch -eq $tag } | Select-Object -First 1
        if ($run -and $run.status -eq "completed") { break }
        Start-Sleep -Seconds 20
    }
    if (-not $run -or $run.status -ne "completed") { throw "the Linux build of $tag did not finish in 30 minutes: run this script again later" }
    if ($run.conclusion -ne "success") { throw "the Linux build of $tag failed: nothing signed" }

    $dir = Join-Path $root "dist\sign-$Version"
    if (Test-Path $dir) { Remove-Item $dir -Recurse -Force }
    New-Item -ItemType Directory -Force $dir | Out-Null
    & $gh release download $tag --dir $dir --pattern "SHA256SUMS.txt" --pattern "Cmdex-Linux-*-$Version.tar.gz"
    if ($LASTEXITCODE) { throw "download failed" }

    Write-Host "== checking every listed file ==" -ForegroundColor Cyan
    $listed = 0
    foreach ($line in Get-Content (Join-Path $dir "SHA256SUMS.txt")) {
        if (-not $line.Trim()) { continue }
        $hash, $name = -split $line
        $name = $name.TrimStart("*")
        $file = @((Join-Path $dir $name), (Join-Path $root "dist\$name")) | Where-Object { Test-Path $_ } | Select-Object -First 1
        if (-not $file) { throw "no copy of $name to check: nothing signed" }
        if ((Get-FileHash $file -Algorithm SHA256).Hash.ToLower() -ne $hash.ToLower()) { throw "$name does not match SHA256SUMS.txt: nothing signed" }
        Write-Host "  ok  $name"
        $listed++
    }
    if ($listed -lt 2) { throw "SHA256SUMS.txt lists $listed file(s), expected the Windows and Linux builds" }

    Write-Host "== signing ==" -ForegroundColor Cyan
    $sums = Join-Path $dir "SHA256SUMS.txt"
    go run ./tools/sign sign $sums
    if ($LASTEXITCODE) { throw "signing failed" }
    go run ./tools/sign verify $sums
    if ($LASTEXITCODE) { throw "the signature does not verify" }
    if ($NoUpload) { Write-Host "Checked and signed locally, not uploaded: $sums.sig" -ForegroundColor Yellow; return }
    & $gh release upload $tag "$sums.sig" --clobber
    if ($LASTEXITCODE) { throw "upload of the signature failed" }
    Write-Host "Signed $tag" -ForegroundColor Green
} finally {
    Pop-Location
}
