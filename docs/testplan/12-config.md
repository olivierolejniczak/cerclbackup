# Phase 12 — Config File

`config.yaml` provides defaults for all flags. CLI flags always override the config. The password field is supported but storing it in plaintext is discouraged — prefer `CERCLBACKUP_PASSWORD` env var.

---

## Step 12.1 — Generate sample config

**Machines:** A

```powershell
cerclbackup config init
```

**Expected:** Prints path of new config file (`%APPDATA%\CerclBackup\config.yaml`). File contains commented-out defaults.

```powershell
# Verify file exists:
Get-Content "$env:APPDATA\CerclBackup\config.yaml"
```

- [ ] PASS — file created with commented template
- [ ] FAIL — notes: ___

---

## Step 12.2 — Edit config: set src and exclude

**Machines:** A

Open `%APPDATA%\CerclBackup\config.yaml` in Notepad and uncomment/set these lines:

```yaml
src: C:\Users\olivier\cercltest
exclude: ".git,*.tmp,*.bak"
# Leave password commented — use env var
```

Save the file.

- [ ] PASS — file saved
- [ ] FAIL — notes: ___

---

## Step 12.3 — Verify config show

**Machines:** A

```powershell
cerclbackup config show
```

**Expected output:**

```
Config file: C:\Users\...\AppData\Roaming\CerclBackup\config.yaml

src         : C:\Users\olivier\cercltest
exclude     : .git,*.tmp,*.bak
password    : (not set)
upload_kbps : 0
health_addr : 127.0.0.1:7743
...
```

- [ ] PASS — src and exclude shown correctly, password masked
- [ ] FAIL — notes: ___

---

## Step 12.4 — Run backup using only config defaults (no --src flag)

**Machines:** A

```powershell
$env:CERCLBACKUP_PASSWORD = '<password-A>'
cerclbackup backup --buddies 3
```

**Expected:** Backup uses `src` from config. No "flag required" error. Backs up the cercltest directory. Exit code 0.

- [ ] PASS — config src used transparently
- [ ] FAIL — notes: ___

---

## Step 12.5 — CLI flag overrides config

**Machines:** A

```powershell
cerclbackup backup --src "$env:USERPROFILE\cercltest\notes.txt" --buddies 3 --password <password-A>
```

**Expected:** Backs up only `notes.txt`, ignoring `src` from config (explicit flag wins).

- [ ] PASS — CLI flag overrides config value
- [ ] FAIL — notes: ___
