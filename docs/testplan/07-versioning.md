# Phase 07 — File Versioning

Each backup creates a new immutable version. Older versions are preserved until explicitly pruned. Versions are 1-based and stored with their timestamp.

---

## Step 07.1 — Create version 2 of hello.txt

**Machines:** A

```powershell
Add-Content "$env:USERPROFILE\cercltest\hello.txt" "Version 2 content added"

cerclbackup backup `
  --src "$env:USERPROFILE\cercltest\hello.txt" `
  --buddies 3 `
  --password <password-A>
```

**Expected:** Backup succeeds. Log shows `version 2`.

- [ ] PASS
- [ ] FAIL — notes: ___

---

## Step 07.2 — List all versions of hello.txt

**Machines:** A

```powershell
cerclbackup versions `
  --file "$env:USERPROFILE\cercltest\hello.txt" `
  --password <password-A>
```

**Expected:** Two rows — version 1 and version 2 — with distinct timestamps and file-ids.

- [ ] PASS — 2 versions shown
- [ ] FAIL — notes: ___

---

## Step 07.3 — `list` shows only latest by default

**Machines:** A

```powershell
cerclbackup list --password <password-A>
```

**Expected:** `hello.txt` appears once at version 2. `notes.txt` at version 1.

```powershell
cerclbackup list --all --password <password-A>
```

**Expected:** `hello.txt` appears twice (v1 and v2).

- [ ] PASS — default deduplicates
- [ ] PASS — `--all` shows both
- [ ] FAIL — notes: ___

---

## Step 07.4 — Restore specific version (v1)

**Machines:** A

```powershell
cerclbackup restore `
  --file "$env:USERPROFILE\cercltest\hello.txt" `
  --version 1 `
  --out "$env:TEMP\hello-v1.txt" `
  --password <password-A>
```

**Expected:** Restores original content — "Version 2 content added" line is absent. Integrity check passed.

```powershell
Get-Content "$env:TEMP\hello-v1.txt"
```

- [ ] PASS — v1 content correct (no v2 line)
- [ ] FAIL — notes: ___

---

## Step 07.5 — Diff since a date

**Machines:** A

```powershell
# Use yesterday's date or the date before the first backup:
cerclbackup diff --since 2026-06-20 --password <password-A>
```

**Expected:** `hello.txt` marked as **updated**, `notes.txt` marked as **new**.

- [ ] PASS
- [ ] FAIL — notes: ___
