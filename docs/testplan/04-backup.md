# Phase 04 — First Backup

A backs up test files. Shards are distributed to B and C over libp2p.  
B and C must be running `cerclbackup serve` so A can push shards to them.

> ↔ **Coordination required** — start serve on B and C before running backup on A.

---

## Step 04.1 — Start serve on B and C

**Machines:** B · C (independent, keep running)

**[B]:**
```bash
export CERCLBACKUP_PASSWORD='<password-B>'
cerclbackup serve &
echo "B serve PID: $!"
```

**[C]:**
```bash
export CERCLBACKUP_PASSWORD='<password-C>'
cerclbackup serve &
echo "C serve PID: $!"
```

**Expected:** Each prints `CerclBackup daemon running`, peer ID, and multiaddress(es). Note the PIDs — you will need them in Phase 06.

**Record B serve PID:** ___  
**Record C serve PID:** ___

- [ ] PASS — B daemon running
- [ ] PASS — C daemon running
- [ ] FAIL — notes: ___

---

## Step 04.2 — Create test data on A

**Machines:** A

```powershell
New-Item -ItemType Directory -Force -Path "$env:USERPROFILE\cercltest"
"Hello CerclBackup v1.0.0`nSensitive data line 1" | Out-File "$env:USERPROFILE\cercltest\hello.txt" -Encoding utf8
"Project notes go here" | Out-File "$env:USERPROFILE\cercltest\notes.txt" -Encoding utf8
```

**Expected:** Two files created — `hello.txt` (~50 bytes) and `notes.txt` (~25 bytes).

```powershell
Get-ChildItem "$env:USERPROFILE\cercltest"
```

- [ ] PASS — both files visible
- [ ] FAIL — notes: ___

---

## Step 04.3 — Back up hello.txt

**Machines:** A

```powershell
cerclbackup backup `
  --src "$env:USERPROFILE\cercltest\hello.txt" `
  --buddies 3 `
  --password <password-A>
```

**Expected:**
- Log shows RS scheme `2 data / 1 parity`.
- Shards pushed to B and C.
- Prints `file-id: <UUID>`.
- Exit code 0.

**Record file-id for hello.txt:** _______________

- [ ] PASS
- [ ] FAIL — notes: ___

---

## Step 04.4 — Back up notes.txt

**Machines:** A

```powershell
cerclbackup backup `
  --src "$env:USERPROFILE\cercltest\notes.txt" `
  --buddies 3 `
  --password <password-A>
```

**Expected:** Same success pattern. Distinct file-id from hello.txt.

**Record file-id for notes.txt:** _______________

- [ ] PASS
- [ ] FAIL — notes: ___

---

## Step 04.5 — List backed-up files

**Machines:** A

```powershell
cerclbackup list --password <password-A>
```

**Expected:** Table shows `hello.txt` and `notes.txt`, each at version 1 with today's date.

- [ ] PASS — both files listed at v1
- [ ] FAIL — notes: ___

---

## Step 04.6 — Verify shards received on B and C

**Machines:** B · C (independent)

**[B] and [C]:**
```bash
# Path may vary; try both:
ls -lh ~/.config/cerclbackup/store/ 2>/dev/null
ls -lh ~/.local/share/cerclbackup/store/ 2>/dev/null
```

**Expected:** `.enc` shard files present. Total size > 0.

- [ ] PASS — B has shard files
- [ ] PASS — C has shard files
- [ ] FAIL — notes: ___
