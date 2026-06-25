# Phase 09 — Watch Mode

The watcher uses fsnotify with a configurable debounce. File changes trigger automatic backups. Run in one terminal; make changes from a second.

B and C must still be running `cerclbackup serve`.

---

## Step 09.1 — Start watcher on A

**Machines:** A (Terminal 1)

```powershell
cerclbackup watch `
  --src "$env:USERPROFILE\cercltest" `
  --debounce 5s `
  --buddies 3 `
  --password <password-A>
```

**Expected:** Logs `watching …\cercltest`. No immediate backup triggered. Process stays in foreground.

- [ ] PASS — watcher started, no spurious backup
- [ ] FAIL — notes: ___

---

## Step 09.2 — Modify an existing file

**Machines:** A (Terminal 2)

```powershell
Add-Content "$env:USERPROFILE\cercltest\notes.txt" "Watch test line"
```

**Expected (Terminal 1, after ~5 s debounce):**
- `[watch] file 1: …\notes.txt`
- Backup progress for notes.txt.
- Integrity check passed.

- [ ] PASS — watcher triggered, counter shows "file 1"
- [ ] FAIL — notes: ___

---

## Step 09.3 — Create a new file

**Machines:** A (Terminal 2)

```powershell
"New file content" | Out-File "$env:USERPROFILE\cercltest\newfile.txt"
```

**Expected (Terminal 1, after debounce):**
- `[watch] file 2: …\newfile.txt`
- Backup succeeds.

```powershell
# Terminal 2 — verify it appears in manifest:
cerclbackup list --password <password-A>
```

**Expected:** 3 files listed: `hello.txt`, `notes.txt`, `newfile.txt`.

- [ ] PASS — counter "file 2", newfile backed up
- [ ] FAIL — notes: ___

---

## Step 09.4 — Verify excluded file is not backed up

**Machines:** A (Terminal 2)

```powershell
"Temp junk" | Out-File "$env:USERPROFILE\cercltest\junk.tmp"
```

**Expected (Terminal 1):** No backup triggered for `junk.tmp` (default exclude pattern `*.tmp` should suppress it if `--exclude` was passed; otherwise confirm the watcher respects the pattern).

> Note: if `--exclude "*.tmp"` was not passed to `watch`, add it and restart for this step.

- [ ] PASS — .tmp file not backed up
- [ ] SKIP — exclude not configured for this run
- [ ] FAIL — notes: ___

---

## Step 09.5 — Stop watcher

**Machines:** A (Terminal 1)

```
Ctrl+C
```

**Expected:** `Shutting down.` logged. Process exits cleanly (exit code 0 or signal-terminated).

- [ ] PASS — clean shutdown
- [ ] FAIL — notes: ___
