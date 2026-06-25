# Phase 01 — Installation

Install the v1.0.0 binary on each machine from the GitHub Releases page.

---

## Step 01.1 — Windows: run MSI installer

**Machines:** A

Download `CerclBackup-1.0.0.msi` from the GitHub Releases page and run it as a standard user (no admin required).

```
GitHub → https://github.com/olivierolejniczak/cerclbackup/releases/tag/v1.0.0
→ Download: CerclBackup-1.0.0.msi
→ Double-click → Accept license → Install
```

**Expected:**
- Installer completes without error.
- `cerclbackup.exe` and `cerclbackup-tray.exe` are present in `%ProgramFiles%\CerclBackup\`.
- Systray icon appears in the Windows notification area.

- [ ] PASS
- [ ] FAIL — notes: ___

---

## Step 01.2 — DietPi: download Linux binary

**Machines:** B · C (independent)

**[B] and [C]:**
```bash
wget https://github.com/olivierolejniczak/cerclbackup/releases/download/v1.0.0/cerclbackup-linux-amd64 \
  -O cerclbackup
chmod +x cerclbackup
sudo mv cerclbackup /usr/local/bin/
```

**Expected:** Binary installed to `/usr/local/bin/cerclbackup` with execute permission.

```bash
which cerclbackup   # should print /usr/local/bin/cerclbackup
```

- [ ] PASS — B installed
- [ ] PASS — C installed
- [ ] FAIL — notes: ___

---

## Step 01.3 — Verify version on all machines

**Machines:** A · B · C (independent)

**[A]:**
```powershell
cerclbackup version
```

**[B] and [C]:**
```bash
cerclbackup version
```

**Expected:** All three print `cerclbackup v1.0.0`.

- [ ] PASS — A shows v1.0.0
- [ ] PASS — B shows v1.0.0
- [ ] PASS — C shows v1.0.0
- [ ] FAIL — notes: ___
