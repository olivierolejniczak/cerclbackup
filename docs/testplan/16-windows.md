# Phase 16 — Windows-Specific: Systray & Task Scheduler

**Machine A only.** Requires the Windows GUI, PowerShell 5.1, and the MSI-installed binaries.

---

## Step 16.1 — Systray icon visible

**Machines:** A

After MSI install, `cerclbackup-tray.exe` starts at login via the HKCU Run registry entry.

```powershell
# Verify the process is running:
Get-Process cerclbackup-tray -ErrorAction SilentlyContinue | Select-Object Name, Id, StartTime
```

**Expected:** Process listed. A circle icon visible in the Windows notification area (expand hidden icons if needed).

- [ ] PASS — process running, icon visible
- [ ] FAIL — notes: ___

---

## Step 16.2 — Systray: status display

**Machines:** A

Right-click the systray icon.

**Expected:**
- Menu shows `CerclBackup v1.0.0`.
- If a backup has been run, tooltip or menu shows last backup time.

- [ ] PASS — version shown in menu
- [ ] FAIL — notes: ___

---

## Step 16.3 — Systray: Backup Now

**Machines:** A

First, set the required environment variables as **user** (not system) variables so the tray process can read them.

```powershell
[System.Environment]::SetEnvironmentVariable(
    "CERCLBACKUP_SRC",
    "$env:USERPROFILE\cercltest",
    "User"
)
[System.Environment]::SetEnvironmentVariable(
    "CERCLBACKUP_PASSWORD",
    "<new-password-A>",
    "User"
)

# Restart the tray to pick up the new env vars:
Stop-Process -Name cerclbackup-tray -Force -ErrorAction SilentlyContinue
Start-Process "$env:ProgramFiles\CerclBackup\cerclbackup-tray.exe"
Start-Sleep 2
```

Right-click the systray icon → **Backup Now**.

**Expected:** A backup is triggered. After completion the status updates to `Last backup: just now` (or similar).

- [ ] PASS — backup triggered from systray
- [ ] FAIL — notes: ___

---

## Step 16.4 — Task Scheduler: register watch task

**Machines:** A

```powershell
Set-Location "$env:ProgramFiles\CerclBackup"
.\scripts\install-task.ps1 -SrcDir "$env:USERPROFILE\cercltest"
```

The script will:
1. Prompt for the keystore password → store it in Windows Credential Manager.
2. Register `CerclBackup-Watch` with AtLogon + hourly triggers.

**Expected:** `Scheduled task 'CerclBackup-Watch' registered.` No errors.

```powershell
Get-ScheduledTask -TaskName "CerclBackup-Watch" | Select-Object TaskName, State
```

- [ ] PASS — task registered, State = Ready
- [ ] FAIL — notes: ___

---

## Step 16.5 — Task Scheduler: run manually and verify

**Machines:** A

```powershell
Start-ScheduledTask -TaskName "CerclBackup-Watch"
Start-Sleep -Seconds 8

Get-ScheduledTaskInfo -TaskName "CerclBackup-Watch" |
    Select-Object LastRunTime, LastTaskResult
```

**Expected:** `LastTaskResult = 0` (success). `LastRunTime` shows the current time.

- [ ] PASS — task ran successfully (result 0)
- [ ] FAIL — notes: ___

---

## Step 16.6 — Task Scheduler: uninstall

**Machines:** A

```powershell
Set-Location "$env:ProgramFiles\CerclBackup"
.\scripts\install-task.ps1 -Uninstall
```

When prompted to remove the stored credential, choose **Y**.

**Expected:** `Scheduled task 'CerclBackup-Watch' removed.` Task no longer visible in Task Scheduler (`taskschd.msc`).

```powershell
Get-ScheduledTask -TaskName "CerclBackup-Watch" -ErrorAction SilentlyContinue
```

**Expected:** No output (task gone).

- [ ] PASS — task removed, credential deleted
- [ ] FAIL — notes: ___
