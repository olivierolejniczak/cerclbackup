# Phase 08 — Retention & Prune

Create extra versions to reach 6, verify the dry-run output, then apply prune and confirm only 3 remain.

---

## Step 08.1 — Create versions 3–6

**Machines:** A

```powershell
3..6 | ForEach-Object {
    Add-Content "$env:USERPROFILE\cercltest\hello.txt" "Version $_ content"
    cerclbackup backup `
        --src "$env:USERPROFILE\cercltest\hello.txt" `
        --buddies 3 `
        --password <password-A>
    Write-Host "Backed up version $_"
}
```

**Expected:** 4 backups complete. `cerclbackup versions --file … --password …` shows 6 rows.

- [ ] PASS — 6 versions present
- [ ] FAIL — notes: ___

---

## Step 08.2 — Dry-run prune (max 3 versions)

**Machines:** A

```powershell
cerclbackup prune `
  --dry-run `
  --max-versions 3 `
  --password <password-A>
```

**Expected:**
- Prints 3 file-ids that **would** be deleted (v1, v2, v3).
- No actual deletion.
- Exit code 0.

- [ ] PASS — 3 IDs printed, nothing deleted
- [ ] FAIL — notes: ___

---

## Step 08.3 — Apply prune

**Machines:** A

```powershell
cerclbackup prune `
  --max-versions 3 `
  --password <password-A>
```

**Expected:** 3 old versions pruned. Manifest updated. Corresponding shards cleaned up on B and C.

- [ ] PASS
- [ ] FAIL — notes: ___

---

## Step 08.4 — Verify only 3 versions remain

**Machines:** A

```powershell
cerclbackup versions `
  --file "$env:USERPROFILE\cercltest\hello.txt" `
  --password <password-A>
```

**Expected:** Exactly 3 versions listed — v4, v5, v6. v1/v2/v3 are gone.

- [ ] PASS — 3 versions remain (v4–v6)
- [ ] FAIL — notes: ___
