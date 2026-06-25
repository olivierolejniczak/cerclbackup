# Phase 06 — Fault Tolerance

Verify the RS 2/1 quorum rules:
- **1 machine down** → restore succeeds (parity covers the gap).
- **2 machines down** → restore fails gracefully (below quorum).

> ↔ **Coordination required** — stop/start serve processes on B and C on signal from A's tester.

---

## Step 06.1 — Take B offline, restore still works

**Machines:** B then A

**[B] — stop serve:**
```bash
kill <B-SERVE-PID>   # PID recorded in Step 04.1
```

Wait 5 seconds for A to detect the disconnection.

**[A] — attempt restore:**
```powershell
cerclbackup restore `
  --file "$env:USERPROFILE\cercltest\hello.txt" `
  --out "$env:TEMP\fault-test-1.txt" `
  --password <password-A>
```

**Expected:** Restore succeeds using shards from C only (parity shard compensates for missing shard on B). Integrity check passed.

- [ ] PASS — restored with B offline
- [ ] FAIL — notes: ___

---

## Step 06.2 — Take B and C offline, restore fails

**Machines:** C then A

**[C] — stop serve:**
```bash
kill <C-SERVE-PID>
```

Wait 5 seconds.

**[A] — attempt restore:**
```powershell
cerclbackup restore `
  --file "$env:USERPROFILE\cercltest\hello.txt" `
  --out "$env:TEMP\fault-test-2.txt" `
  --password <password-A>
```

**Expected:**
- Restore **fails** — cannot reach quorum (0 peers online).
- Output file `fault-test-2.txt` is **not created**.
- Exit code non-zero.

- [ ] PASS — failed as expected, no corrupted output file
- [ ] FAIL — notes: ___

---

## Step 06.3 — Restore B and C

**Machines:** B · C

**[B]:**
```bash
export CERCLBACKUP_PASSWORD='<password-B>'
cerclbackup serve &
```

**[C]:**
```bash
export CERCLBACKUP_PASSWORD='<password-C>'
cerclbackup serve &
```

**Expected:** Both daemons restart. `[A] cerclbackup buddy status` shows both online again.

- [ ] PASS — B back online
- [ ] PASS — C back online
- [ ] FAIL — notes: ___
