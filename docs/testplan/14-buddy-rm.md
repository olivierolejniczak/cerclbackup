# Phase 14 — Buddy Removal & Rebalance

Remove C from A's buddy list. The binary automatically rebalances all shards previously stored on C to the remaining buddy (B), restoring full redundancy.

> **Use `<new-password-A>` from Phase 13 onward.**

> ↔ **Coordination required** — B must be running `cerclbackup serve` to receive rebalanced shards.

---

## Step 14.1 — Get C's peer ID

**Machines:** A

```powershell
cerclbackup buddy list --password <new-password-A>
```

**Expected:** Table shows B (oliviero) and C (foxy) with their peer IDs.

**Record peer ID of C:** `12D3...` _______________

- [ ] PASS — C peer ID noted
- [ ] FAIL — notes: ___

---

## Step 14.2 — Remove C and rebalance

**Machines:** A (B must be serving)

```powershell
cerclbackup buddy rm `
  --peer-id <C-PEER-ID> `
  --password <new-password-A>
```

**Expected:**
```
Buddy 12D3... removed.
Rebalancing shards across remaining buddies...
```
Rebalance pushes all shards previously assigned to C to B. May take a few seconds depending on shard count.

- [ ] PASS — removed and rebalanced
- [ ] FAIL — notes: ___

---

## Step 14.3 — Verify buddy list updated

**Machines:** A

```powershell
cerclbackup buddy list --password <new-password-A>
```

**Expected:** Only B (oliviero) listed. C is gone.

- [ ] PASS — C not listed
- [ ] FAIL — notes: ___

---

## Step 14.4 — Restore with C removed

**Machines:** A (B serving, C offline or simply unregistered)

```powershell
cerclbackup restore `
  --file "$env:USERPROFILE\cercltest\hello.txt" `
  --out "$env:TEMP\hello-after-rm.txt" `
  --password <new-password-A>

fc "$env:USERPROFILE\cercltest\hello.txt" "$env:TEMP\hello-after-rm.txt"
```

**Expected:** Restore succeeds using B only. C is not contacted. Integrity check passed. Files identical.

- [ ] PASS — restore works with only B
- [ ] FAIL — notes: ___
