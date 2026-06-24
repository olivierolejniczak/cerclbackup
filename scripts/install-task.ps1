#Requires -Version 5.1
<#
.SYNOPSIS
    Registers CerclBackup as a Windows Scheduled Task.

.DESCRIPTION
    Creates two triggers:
      * AtLogon  -- runs immediately when the current user logs in
      * Hourly   -- repeats every hour while the user is logged in

    The task runs cerclbackup.exe watch in the background (window hidden).
    The keystore password is read from the Windows Credential Manager entry
    "CerclBackup" (target: generic credential, username: "password").
    If no credential exists yet, the script prompts and stores it now.

.PARAMETER BinDir
    Directory containing cerclbackup.exe (default: same folder as this script).

.PARAMETER SrcDir
    Directory tree to watch and back up (required).

.PARAMETER Exclude
    Comma-separated glob exclusion patterns (default: .git,node_modules,*.tmp,*.swp).

.PARAMETER UploadKbps
    Upload rate cap in KB/s  (0 = unlimited, default: 0).

.PARAMETER Uninstall
    Remove the scheduled task and optionally the stored credential.

.EXAMPLE
    .\scripts\install-task.ps1 -SrcDir C:\Users\alice\Documents
    .\scripts\install-task.ps1 -Uninstall
#>
param(
    [string]$BinDir     = (Split-Path -Parent $PSScriptRoot),
    [string]$SrcDir     = "",
    [string]$Exclude    = ".git,node_modules,*.tmp,*.swp",
    [int]   $UploadKbps = 0,
    [switch]$Uninstall
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

$TaskName   = "CerclBackup-Watch"
$CredTarget = "CerclBackup"

# ── Uninstall ─────────────────────────────────────────────────────────────────
if ($Uninstall) {
    if (Get-ScheduledTask -TaskName $TaskName -ErrorAction SilentlyContinue) {
        Unregister-ScheduledTask -TaskName $TaskName -Confirm:$false
        Write-Host "Scheduled task '$TaskName' removed."
    } else {
        Write-Host "Task '$TaskName' not found."
    }
    $choice = Read-Host "Also remove stored credential? [y/N]"
    if ($choice -match '^[Yy]') {
        cmdkey /delete:$CredTarget | Out-Null
        Write-Host "Credential removed."
    }
    exit 0
}

# ── Validate ──────────────────────────────────────────────────────────────────
$exe = Join-Path $BinDir "cerclbackup.exe"
if (-not (Test-Path $exe)) {
    Write-Error "cerclbackup.exe not found at '$exe'. Build it first or pass -BinDir."
}
if (-not $SrcDir) {
    Write-Error "-SrcDir is required.  Example: -SrcDir C:\Users\$env:USERNAME\Documents"
}
if (-not (Test-Path $SrcDir)) {
    Write-Error "SrcDir '$SrcDir' does not exist."
}

# ── Credential ────────────────────────────────────────────────────────────────
# Try to read an existing credential.
$credLine = (cmdkey /list:$CredTarget 2>$null) -join " "
if ($credLine -notmatch $CredTarget) {
    Write-Host "No stored credential found for '$CredTarget'."
    $sec = Read-Host "Enter your CerclBackup keystore password" -AsSecureString
    $plain = [System.Runtime.InteropServices.Marshal]::PtrToStringAuto(
        [System.Runtime.InteropServices.Marshal]::SecureStringToBSTR($sec))
    cmdkey /generic:$CredTarget /user:password /pass:$plain | Out-Null
    Write-Host "Password stored in Windows Credential Manager."
}

# The task action retrieves the password at runtime via cmdkey / PowerShell.
# We embed a small inline PS snippet that reads the credential and exports it
# as CERCLBACKUP_PASSWORD before launching cerclbackup.exe watch.
$uploadArg = if ($UploadKbps -gt 0) { "--upload-kbps $UploadKbps" } else { "" }

$actionScript = @"
`$cred = Get-StoredCredential -Target '$CredTarget' -ErrorAction SilentlyContinue
if (-not `$cred) {
    # Fallback: parse cmdkey output
    `$raw = (cmdkey /list:$CredTarget 2>`$null | Select-String 'User:').ToString()
}
`$env:CERCLBACKUP_PASSWORD = `$cred.GetNetworkCredential().Password
& '$exe' watch --src '$SrcDir' --exclude '$Exclude' $uploadArg --password `$env:CERCLBACKUP_PASSWORD
"@

# Write the launcher script next to the binary.
$launcher = Join-Path $BinDir "cerclbackup-watch-launcher.ps1"
Set-Content -Path $launcher -Value $actionScript -Encoding UTF8

# ── Task definition ───────────────────────────────────────────────────────────
$action = New-ScheduledTaskAction `
    -Execute "powershell.exe" `
    -Argument "-NonInteractive -WindowStyle Hidden -ExecutionPolicy Bypass -File `"$launcher`""

$triggerLogon = New-ScheduledTaskTrigger -AtLogOn -User $env:USERNAME
$triggerHourly = New-ScheduledTaskTrigger -RepetitionInterval (New-TimeSpan -Hours 1) `
    -Once -At (Get-Date).Date

$settings = New-ScheduledTaskSettingsSet `
    -ExecutionTimeLimit (New-TimeSpan -Hours 23) `
    -MultipleInstances IgnoreNew `
    -StartWhenAvailable `
    -RunOnlyIfNetworkAvailable

$principal = New-ScheduledTaskPrincipal `
    -UserId $env:USERNAME `
    -LogonType Interactive `
    -RunLevel Limited

# Unregister if already exists so we can update.
if (Get-ScheduledTask -TaskName $TaskName -ErrorAction SilentlyContinue) {
    Unregister-ScheduledTask -TaskName $TaskName -Confirm:$false
}

Register-ScheduledTask `
    -TaskName $TaskName `
    -Action $action `
    -Trigger @($triggerLogon, $triggerHourly) `
    -Settings $settings `
    -Principal $principal `
    -Description "CerclBackup: watch '$SrcDir' and back up changes" | Out-Null

Write-Host ""
Write-Host "Scheduled task '$TaskName' registered."
Write-Host "  Source : $SrcDir"
Write-Host "  Binary : $exe"
Write-Host "  Runs   : at logon + every hour"
Write-Host ""
Write-Host "To start now:   Start-ScheduledTask -TaskName '$TaskName'"
Write-Host "To uninstall:   .\scripts\install-task.ps1 -Uninstall"
