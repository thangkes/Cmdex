# Publish a new Cmdex version on GitHub in one go:
#   bump VERSION -> build + test the Windows installer -> SHA256SUMS -> commit
#   -> tag v<v> -> push -> GitHub Release.
# The Linux build is made by GitHub Actions when the release is published
# (.github/workflows/release-linux.yml); it adds its archive and checksum.
#
#   .\scripts\release.ps1 0.1.0 -NotesFile dist\notes-0.1.0.md
#   .\scripts\release.ps1 0.2.0-beta.1 -NotesFile notes.md   # "-" = pre-release
param(
    [Parameter(Mandatory = $true)][string]$Version,
    [Parameter(Mandatory = $true)][string]$NotesFile
)
$ErrorActionPreference = "Stop"
$root = Split-Path $PSScriptRoot -Parent
$gh = "C:\Program Files\GitHub CLI\gh.exe"
if (-not (Test-Path $gh)) { $gh = "gh" }
$utf8 = New-Object Text.UTF8Encoding $false

if ($Version -notmatch '^(\d+)\.(\d+)\.(\d+)(-[0-9A-Za-z.]+)?$') { throw "Version must look like 1.2.3 or 1.2.3-beta.1" }
$pre = [bool]$Matches[4]
$tag = "v$Version"
$notes = (Resolve-Path $NotesFile).Path

Push-Location $root
try {
    if (git status --porcelain) { throw "Working tree is not clean - commit or stash first." }
    if ((git branch --show-current) -ne "main") { throw "Release from the main branch." }
    if (git tag -l $tag) { throw "Tag $tag already exists." }
    & $gh auth status *> $null
    if ($LASTEXITCODE) { throw "gh is not logged in (gh auth login)." }

    [IO.File]::WriteAllText("$root\VERSION", "$Version`n", $utf8)
    & "$PSScriptRoot\build-windows.ps1" -Version $Version
    if (-not $?) { throw "Windows build failed" }
    $ErrorActionPreference = "Continue" # git prints to stderr

    $out = "$root\dist\release-$Version"
    if (Test-Path $out) { Remove-Item $out -Recurse -Force }
    New-Item -ItemType Directory -Force $out | Out-Null
    Copy-Item "$root\dist\Cmdex-Windows-Setup-$Version.exe" $out
    $sums = Get-ChildItem $out -File | ForEach-Object {
        "{0}  {1}" -f (Get-FileHash $_.FullName -Algorithm SHA256).Hash.ToLower(), $_.Name
    }
    [IO.File]::WriteAllText("$out\SHA256SUMS.txt", (($sums -join "`n") + "`n"), $utf8)
    Get-Content "$out\SHA256SUMS.txt"

    Write-Host "== publish $tag ==" -ForegroundColor Cyan
    git add VERSION winres/winres.json
    git commit -q -m "Release $tag"
    if ($LASTEXITCODE) { throw "git commit failed" }
    git tag -a $tag -m "Cmdex $Version"
    git push -q origin main
    if ($LASTEXITCODE) { throw "git push failed" }
    git push -q origin $tag
    if ($LASTEXITCODE) { throw "git push tag failed" }

    $ghArgs = @("release", "create", $tag, "--title", "Cmdex $Version", "--notes-file", $notes)
    if ($pre) { $ghArgs += "--prerelease" }
    $ghArgs += (Get-ChildItem $out -File).FullName
    & $gh @ghArgs
    if ($LASTEXITCODE) { throw "gh release create failed" }
    Write-Host "Released $tag - the Linux archive is added by GitHub Actions in a few minutes." -ForegroundColor Green
} finally {
    Pop-Location
}
