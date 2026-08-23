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

---

## Step 16.8 — GUI: language toggle (Settings)

**Machines:** A

Unlock the app, go to **Settings**, switch the **Language** dropdown from English to Français.

**Expected:** Entire UI (nav sidebar, headers, buttons) re-renders in French immediately, no reload needed.

Quit the app fully (`Stop-Process -Name cerclbackup-gui -Force` or tray → Quit) and relaunch `cerclbackup-gui.exe`.

**Expected:** The pre-unlock welcome/setup screen also renders in French ("Bienvenue dans CerclBackup", "Déverrouiller", "Première configuration", "Récupérer à partir d'une phrase") — confirms the language preference is persisted server-side and applied before unlock, not just a client-side toggle.

- [x] PASS — language toggle applies instantly and persists across restart, including the pre-unlock screen

**Bugs found during this step:**

- **FAIL** — Settings view shows a raw `TypeError: (intermediate value) is not iterable` at the top of the page on load. Needs repro/root-cause (likely a failed data fetch feeding the settings view) and a fix before release.
- **FAIL** — The "Init config file" button (under "Fichier de configuration" / "Configuration file") does not have a French translation — it stays in English ("Init config file") when the rest of the UI is in French. Missing i18n key.

Fixed in `1cc7dc9` (Settings load crash, `settings.config.init` i18n key added to both locales). Code-verified: `settings.config.init` present in `en.json`/`fr.json`, Linux GUI builds clean with the fix. Still needs a real click-through re-test on the Windows machine to confirm the crash no longer appears live.

- [x] PASS — fix present in code and confirmed via `go build`/`wails build`
- [ ] PASS — re-verified live on Windows GUI
- [ ] FAIL — notes: ___

---

## Step 16.9 — GUI: Buddies tab

**Machines:** A

Unlock the app, go to **Buddies**. Confirm the empty state (no buddies yet): Name/Status/Latency table with no rows, "0/0 buddies online" matching the Dashboard.

**Invite a buddy:**
1. Leave Serve port at default (4001), click **Generate invite**.
2. Windows Firewall will prompt to allow public/private network access for `cerclbackup-gui` (starting the p2p listener) — click **Allow**.

**Expected:** Displays a 12-word invite phrase and a join multiaddr (`/ip4/<lan-ip>/tcp/4001/p2p/<peer-id>`) with a **Copy** button.

- [x] PASS — invite generated correctly after firewall prompt allowed

**Copy button check:** click **Copy**, then paste (Ctrl+V) into another field to confirm the clipboard actually received the address.

- [x] PASS — clipboard content correct
- **Minor issue:** no visual confirmation (e.g. "Copied!") is shown after clicking Copy — functions correctly but gives no user feedback.

**Join a buddy — validation and self-join:**
1. Paste the just-generated address + invite words + a friendly name into the "Join a buddy" fields on the same machine, click **Join**.
   - **Expected:** clear rejection, not a crash — `Error: ...failed to dial: dial to self attempted`.
   - [x] PASS
2. Clear all three "Join a buddy" fields and click **Join** again.
   - **Expected:** validation error, e.g. `Error: password, addr and words are required`.
   - [x] PASS

**Refresh button:** click **Refresh** at the top of the page.

- **Expected:** clears any prior error banner, buddy list stays empty (no buddies actually added). No crash.
- [x] PASS

**Minor issue — default window size:** at the app's default launch size, the two-column Invite/Join layout on this page overflows and requires horizontal scrolling to reach the "Join a buddy" panel and the right edge of "Buddy address"/"Invite words" fields. Confirmed fine once the window is maximized — likely just needs a smaller default width breakpoint or a slightly larger default window size.

Fixed in `1cc7dc9`: `.grid { min-width: 0 }` plus a `@media (max-width: 900px)` breakpoint that collapses the two-column Invite/Join layout to one column. Also added copy-button "Copied!" feedback (`common.copied` i18n key, used in Buddies.svelte). Code-verified via `wails build`; still needs a live re-check at default window size on Windows.

- [x] PASS — layout/copy-feedback fix present in code and confirmed via `wails build`
- [ ] PASS — full end-to-end buddy pairing tested across two real machines (this step only covered self-join validation on machine A)
- [ ] FAIL — notes: ___

---

## Step 16.10 — GUI: Maintenance tab

**Machines:** A

Unlock the app, go to **Maintenance**. Confirm all six panels render: Prune old versions, Scrub shard store, Rebalance shards across buddies, Audit shard integrity, Export a backup as a portable file, Import a portable backup file, plus "Recover manifest from a buddy" below.

**Run audit** (empty store, no args needed):

- **FAIL** — `Error: open store: storage: mkdir "": mkdir : The system cannot find the path specified.` The audit binding is being called with an empty store path instead of the resolved store path the Dashboard already uses correctly (`C:\Users\Docker\AppData\Roaming\CerclBackup\store`). Needs fix before release — audit is currently unusable from the GUI.

**Run scrub** / **Run rebalance** (empty store, no args needed):

- [x] PASS — both complete without error; result renders as raw JSON (e.g. `{"Checked":0,"OK":0,"Corrupted":0,"Revived":0,"Failed":0}`) in a **Result** panel

**Run prune** (defaults: keep-all 7d, keep-weekly 30d, max 10, dry-run checked):

- [x] PASS — completes without error

**Export / Import / Recover-manifest validation** (empty required fields):

- [x] PASS — clear, specific validation errors for each: `Error: password and file path are required` (Export, Import), `Error: password and addr are required` (Recover manifest). No crashes.

**Bugs / UX issues found during this step:**

- **FAIL** — `Run audit` fails with an empty-store-path error regardless of input (see above) — functional bug, not just a UX issue.
- **UX issue** — Result/error placement is inconsistent: validation **errors** render in a banner at the **top** of the page, while **success** results (Scrub/Rebalance JSON) render in a "Result" panel at the very **bottom** of the page, below the fold. Combined with the fact that every button click resets the scroll position, the success Result panel is very easy to miss — there is no toast, no scroll-into-view, and no visual diff (e.g. flash/highlight) to draw attention to a fresh result. Recommend: show all feedback (success and error) in one consistent, visible location (e.g. sticky banner at top, or auto-scroll to the Result panel).

Both fixed in `1cc7dc9`: all shard-store opens now route through a shared helper (`api.OpenStore`) that defaults an empty path to `storage.DefaultStorePath()` consistently across Audit/Storage/Doctor/Data — no more empty-path crash. Maintenance.svelte now renders both error and success feedback in one `.feedback` panel at the top of the page, with `scrollIntoView` on every run so a fresh result is never below the fold.

Regression-tested headlessly (no Windows display available in this pass): a temporary Go test called `api.Audit(pw, "")`, `api.Dashboard(pw, "")`, and `api.Storage(pw, "")` exactly as the GUI does — all three now succeed against a fresh empty store instead of erroring. Full `go test ./...` and `wails build -tags webkit2_41` both pass.

- [x] PASS — audit bug fixed and regression-tested at the API layer; feedback placement fix confirmed in source
- [ ] PASS — re-verified live by clicking through the Maintenance tab on the Windows GUI
- [ ] FAIL — notes: ___
