# Phase 13 — Password Change

`cerclbackup passwd` re-encrypts only the keystore wrapper. The master key (which encrypts manifests and derives shard keys) is unchanged — no re-encryption of backup data is required.

> **From Phase 14 onward, use `<new-password-A>` wherever A's password is required.**

---

## Step 13.1 — Change keystore password on A

**Machines:** A

```powershell
cerclbackup passwd --old <password-A> --new <new-password-A>
```

Or interactively (omit flags — prompts for old, new, confirm):

```powershell
cerclbackup passwd
```

**Expected:** `Keystore password changed successfully.` Exit code 0.

- [ ] PASS
- [ ] FAIL — notes: ___

---

## Step 13.2 — Verify old password is rejected

**Machines:** A

```powershell
cerclbackup list --password <password-A>
```

**Expected:** `wrong password or corrupted keystore` error. Exit code non-zero. No data displayed.

- [ ] PASS — old password rejected
- [ ] FAIL — notes: ___

---

## Step 13.3 — Verify new password works for restore

**Machines:** A

```powershell
cerclbackup restore `
  --file "$env:USERPROFILE\cercltest\notes.txt" `
  --out "$env:TEMP\notes-after-passwd.txt" `
  --password <new-password-A>

fc "$env:USERPROFILE\cercltest\notes.txt" "$env:TEMP\notes-after-passwd.txt"
```

**Expected:** Restore succeeds. Integrity check passed. Files identical. Manifest and shard keys unchanged.

- [ ] PASS — new password works, data intact
- [ ] FAIL — notes: ___
