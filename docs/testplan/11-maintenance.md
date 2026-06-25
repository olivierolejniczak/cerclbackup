# Phase 11 — Maintenance: Doctor, Audit, Scrub, Storage

Doctor is the go/no-go check — all 7 items must show ✓ before declaring the installation healthy. Run on all machines.

---

## Step 11.1 — Doctor on all machines

**Machines:** A · B · C (independent)

**[A]:**
```powershell
cerclbackup doctor --password <password-A>
```

**[B]:**
```bash
cerclbackup doctor --password <password-B>
```

**[C]:**
```bash
cerclbackup doctor --password <password-C>
```

**Expected:** All 7 checks show ✓ on each machine. Exit code 0.

Checks: keystore · peer identity · store · manifest · last backup age · buddies reachable · disk space.

> Any ✗ is a blocker — investigate before continuing.

- [ ] PASS — A: 7/7 ✓
- [ ] PASS — B: 7/7 ✓
- [ ] PASS — C: 7/7 ✓
- [ ] FAIL — notes: ___

---

## Step 11.2 — Storage accounting on A

**Machines:** A

```powershell
cerclbackup storage --password <password-A>
```

**Expected:** Prints file count, total logical size, on-disk size (shards), and amplification ratio. No errors.

- [ ] PASS
- [ ] FAIL — notes: ___

---

## Step 11.3 — Audit on A

**Machines:** A

```powershell
cerclbackup audit --password <password-A>
```

**Expected:** All shards pass AES-GCM tag validation. No orphaned shards. Exit code 0.

- [ ] PASS
- [ ] FAIL — notes: ___

---

## Step 11.4 — Scrub on B and C

**Machines:** B · C (independent)

**[B]:**
```bash
cerclbackup scrub --password <password-B>
```

**[C]:**
```bash
cerclbackup scrub --password <password-C>
```

**Expected:** All stored shards verified. No corruption detected. Exit code 0.

- [ ] PASS — B: clean
- [ ] PASS — C: clean
- [ ] FAIL — notes: ___

---

## Step 11.5 — Simulate corruption, verify scrub detects it

**Machines:** B

Find a shard file on B and corrupt its last 16 bytes, then run scrub.

```bash
# List shard files:
ls ~/.config/cerclbackup/store/

# Pick one .enc file and corrupt it:
python3 -c "
import sys
path = sys.argv[1]
with open(path, 'r+b') as f:
    f.seek(-16, 2)
    f.write(b'\\xde\\xad\\xbe\\xef' * 4)
" <SHARD-PATH>

# Run scrub:
cerclbackup scrub --password <password-B>
```

**Expected:** Scrub logs `shard <id> corrupt — attempting revival`. Scrub attempts to fetch the correct shard from the owner (A). Exit code 1 if revival fails (expected if A is not serving); exit code 0 if revival succeeds.

> Restore the original shard by running another backup from A after this step.

- [ ] PASS — corruption detected and reported
- [ ] FAIL — notes: ___
