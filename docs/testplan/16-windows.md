# Phase 16 — Windows-Specific: Desktop GUI & Task Scheduler

**Machine A only.** Requires the Windows GUI, PowerShell 5.1, and the MSI-installed binaries.

---

## Step 16.1 — Tray icon visible

**Machines:** A

After MSI install, `cerclbackup-gui.exe --hidden` starts at login via the HKCU Run registry entry (window stays hidden; only the tray icon shows).

```powershell
# Verify the process is running:
Get-Process cerclbackup-gui -ErrorAction SilentlyContinue | Select-Object Name, Id, StartTime
```

**Expected:** Process listed. A circle icon visible in the Windows notification area (expand hidden icons if needed).

- [ ] PASS — process running, icon visible
- [ ] FAIL — notes: ___

---

## Step 16.2 — Tray: show window

**Machines:** A

Right-click the tray icon → **Show window**.

**Expected:** The CerclBackup window opens, showing the Dashboard view with live health status (doctor + buddy + storage).

- [ ] PASS — window opens with dashboard data
- [ ] FAIL — notes: ___

---

## Step 16.3 — Tray: Backup now

**Machines:** A

In the window, go to the **Backup** tab and fill in a source path and buddy count, then close the window (it hides to tray instead of exiting).

Right-click the tray icon → **Backup now**.

**Expected:** The window reopens on the Backup tab. Running the backup from there streams progress lines live.

- [ ] PASS — tray action switches to Backup tab
- [ ] FAIL — notes: ___

---

## Step 16.4 — Window close hides to tray; Quit exits

**Machines:** A

Click the window's close (X) button, then right-click the tray icon → **Quit**.

**Expected:** Closing the window hides it (tray icon remains, process keeps running). Quit terminates the process entirely.

```powershell
Get-Process cerclbackup-gui -ErrorAction SilentlyContinue
```

**Expected after Quit:** No output (process gone).

- [ ] PASS — close hides to tray, Quit exits the process
- [ ] FAIL — notes: ___

---

## Step 16.5 — Task Scheduler: register watch task

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

## Step 16.6 — Task Scheduler: run manually and verify

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

## Step 16.7 — Task Scheduler: uninstall

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
