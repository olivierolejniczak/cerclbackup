# Phase 05 — Restore & Integrity Verification

Restore files from peers. After reconstruction the binary computes the Merkle hash (SHA-256 of chunk SHA-256s) and compares it to the manifest. A mismatch deletes the output and exits non-zero.

B and C must still be running `cerclbackup serve` from Phase 04.

---

## Step 05.1 — Restore hello.txt (all peers online)

**Machines:** A

```powershell
cerclbackup restore `
  --file "$env:USERPROFILE\cercltest\hello.txt" `
  --out "$env:TEMP\hello-restored.txt" `
  --password <password-A>
```

**Expected:**
- Log: `[restore] integrity check passed`.
- Exit code 0.
- Output file created at `%TEMP%\hello-restored.txt`.

- [ ] PASS
- [ ] FAIL — notes: ___

---

## Step 05.2 — Compare restored content

**Machines:** A

```powershell
fc "$env:USERPROFILE\cercltest\hello.txt" "$env:TEMP\hello-restored.txt"
```

**Expected:** `FC: no differences encountered`

- [ ] PASS — files identical
- [ ] FAIL — notes: ___

---

## Step 05.3 — Restore notes.txt

**Machines:** A

```powershell
cerclbackup restore `
  --file "$env:USERPROFILE\cercltest\notes.txt" `
  --out "$env:TEMP\notes-restored.txt" `
  --password <password-A>

fc "$env:USERPROFILE\cercltest\notes.txt" "$env:TEMP\notes-restored.txt"
```

**Expected:** Integrity check passed. Files identical.

- [ ] PASS
- [ ] FAIL — notes: ___

---

## Step 05.4 — Restore by file-id (backward compat)

**Machines:** A

Use the file-id recorded in Step 04.3.

```powershell
cerclbackup restore `
  --file-id <FILE-ID-hello> `
  --out "$env:TEMP\hello-by-id.txt" `
  --password <password-A>

fc "$env:TEMP\hello-restored.txt" "$env:TEMP\hello-by-id.txt"
```

**Expected:** Identical content to the path-based restore. Integrity check passed.

- [ ] PASS
- [ ] FAIL — notes: ___
