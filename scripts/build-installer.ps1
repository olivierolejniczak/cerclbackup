#Requires -Version 5.1
<#
.SYNOPSIS
    Builds CerclBackup Windows binaries and the WiX v4 MSI installer.

.DESCRIPTION
    1. Cross-compiles cerclbackup.exe and cerclbackup-tray.exe for windows/amd64
       (runs on Linux/macOS CI as well as Windows with Go installed).
    2. Calls `wix build` (WiX Toolset v4, must be on PATH) to produce
       dist\cerclbackup-setup-<version>.msi.

.PARAMETER Version
    Semantic version string stamped into the binary and the MSI (default: 0.0.0-dev).

.PARAMETER BinDir
    Directory where compiled .exe files are written (default: dist).

.EXAMPLE
    .\scripts\build-installer.ps1 -Version 1.2.0
#>
param(
    [string]$Version = "0.0.0-dev",
    [string]$BinDir  = "dist"
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

$Root = Split-Path -Parent $PSScriptRoot

Push-Location $Root
try {

    # ── 0. Validate semver (WiX requires Major.Minor.Patch) ──────────────────
    if ($Version -notmatch '^\d+\.\d+\.\d+') {
        Write-Error "Version must start with Major.Minor.Patch (got '$Version')."
    }
    $MsiVersion = ($Version -split '-')[0]   # strip pre-release suffix for MSI

    # ── 1. Create output directory ────────────────────────────────────────────
    New-Item -ItemType Directory -Force -Path $BinDir | Out-Null

    # ── 2. Cross-compile Go binaries ─────────────────────────────────────────
    $LdBase  = "-s -w -X github.com/cerclbackup/cerclbackup/internal/version.AppVersion=$Version"

    Write-Host "==> Compiling cerclbackup.exe (windows/amd64) ..."
    $env:GOOS   = "windows"
    $env:GOARCH = "amd64"
    & go build -ldflags $LdBase `
               -o "$BinDir\cerclbackup.exe" `
               .\cmd\cerclbackup
    if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }

    Write-Host "==> Compiling cerclbackup-tray.exe (windows/amd64, windowsgui) ..."
    & go build -ldflags "$LdBase -H=windowsgui" `
               -o "$BinDir\cerclbackup-tray.exe" `
               .\cmd\cerclbackup-tray
    if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }

    Remove-Item Env:GOOS, Env:GOARCH -ErrorAction SilentlyContinue

    # ── 3. Build MSI with WiX v4 ─────────────────────────────────────────────
    $MsiOut = "$BinDir\cerclbackup-setup-$Version.msi"

    Write-Host "==> Installing WiX UI extension ..."
    & wix extension add WixToolset.UI.wixext/4.0.5 --global
    if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }

    Write-Host "==> Building MSI $MsiOut ..."
    & wix build installer\cerclbackup.wxs `
         -ext WixToolset.UI.wixext/4.0.5 `
         -d "BinDir=$BinDir" `
         -d "ProductVersion=$MsiVersion" `
         -o $MsiOut
    if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }

    Write-Host ""
    Write-Host "Done. Installer: $MsiOut"
    Write-Host "      CLI:       $BinDir\cerclbackup.exe"
    Write-Host "      Tray:      $BinDir\cerclbackup-tray.exe"

} finally {
    Pop-Location
}
