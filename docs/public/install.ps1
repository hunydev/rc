<#
.SYNOPSIS
    Install rc (Remote Control) on Windows
.DESCRIPTION
    Downloads the latest rc release from GitHub and installs it.
#>

$ErrorActionPreference = "Stop"
$REPO = "hunydev/rc"

function Get-Arch {
    $arch = [System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture
    switch ($arch) {
        "X64"   { return "amd64" }
        "Arm64" { return "arm64" }
        default { Write-Error "Unsupported architecture: $arch"; exit 1 }
    }
}

function Get-LatestVersion {
    $url = "https://api.github.com/repos/$REPO/releases/latest"
    $resp = Invoke-RestMethod -Uri $url -Headers @{ "User-Agent" = "rc-installer" }
    return $resp.tag_name
}

$arch = Get-Arch
$version = Get-LatestVersion
$archive = "rc_${version}_windows_${arch}.zip"
$url = "https://github.com/$REPO/releases/download/$version/$archive"
$installDir = "$env:LOCALAPPDATA\rc"
$binPath = "$installDir\rc.exe"

Write-Host "Installing rc $version (windows/$arch)..." -ForegroundColor Cyan

if (!(Test-Path $installDir)) {
    New-Item -ItemType Directory -Path $installDir -Force | Out-Null
}

$tmpZip = "$env:TEMP\$archive"
Write-Host "Downloading $url..."
Invoke-WebRequest -Uri $url -OutFile $tmpZip -UseBasicParsing

Expand-Archive -Path $tmpZip -DestinationPath $installDir -Force
Remove-Item $tmpZip -ErrorAction SilentlyContinue

# Add to PATH if not already present
$userPath = [Environment]::GetEnvironmentVariable("Path", "User")
if ($userPath -notlike "*$installDir*") {
    [Environment]::SetEnvironmentVariable("Path", "$userPath;$installDir", "User")
    Write-Host "Added $installDir to user PATH" -ForegroundColor Yellow
}

Write-Host ""
Write-Host "rc $version installed to $binPath" -ForegroundColor Green
Write-Host "Restart your terminal, then run: rc" -ForegroundColor Cyan
