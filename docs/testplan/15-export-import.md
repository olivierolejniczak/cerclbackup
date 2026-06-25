# Phase 15 — Export / Import .cbk Archive

A `.cbk` file is a gzip-compressed tar containing `manifest.json` and encrypted shard files. The shards remain AES-256-GCM encrypted — the archive is confidential without additional wrapping. It can be restored on any machine that has the matching keystore.

> ↔ **Coordination required** — A exports, then transfers to B who imports and restores locally.

---

## Step 15.1 — Export hello.txt to .cbk on A

**Machines:** A

```powershell
# Get the current latest file-id for hello.txt:
cerclbackup list --password <new-password-A>

# Export:
cerclbackup export `
  --file-id <FILE-ID-hello-latest> `
  --out "$env:USERPROFILE\cercltest\hello-export.cbk" `
  --password <new-password-A>
```

**Expected:** Creates `hello_vN_YYYYMMDD.cbk`. File size is slightly larger than the original (encrypted shards + manifest overhead).

```powershell
Get-Item "$env:USERPROFILE\cercltest\hello-export.cbk"
```

- [ ] PASS — .cbk file created
- [ ] FAIL — notes: ___

---

## Step 15.2 — Transfer .cbk to B

**Machines:** A → B

```powershell
# From A (requires OpenSSH or WinSCP):
scp "$env:USERPROFILE\cercltest\hello-export.cbk" <B-user>@<B-IP>:~/
```

Or use any other file transfer method (USB, shared folder, etc.).

**[B] — verify received:**
```bash
ls -lh ~/hello-export.cbk
```

- [ ] PASS — file on B
- [ ] FAIL — notes: ___

---

## Step 15.3 — Import on B

**Machines:** B

```bash
cerclbackup import --in ~/hello-export.cbk --password <password-B>
```

**Expected:** `Imported file-id: <UUID>`. Shards written to B's local store.

- [ ] PASS
- [ ] FAIL — notes: ___

---

## Step 15.4 — Verify in B's manifest

**Machines:** B

```bash
cerclbackup list --password <password-B>
```

**Expected:** `hello.txt` appears in B's manifest with the same version and file-id as the export.

- [ ] PASS — entry visible in B's manifest
- [ ] FAIL — notes: ___

---

## Step 15.5 — Restore from B's imported shards

**Machines:** B

No network needed — shards are local.

```bash
cerclbackup restore \
  --file "$HOME/cercltest/hello.txt" \
  --out /tmp/hello-from-import.txt \
  --password <password-B>

# Verify content:
cat /tmp/hello-from-import.txt
```

**Expected:** Restore succeeds from local shards only. Integrity check passed. Content matches what A backed up.

- [ ] PASS — restored locally from import, content correct
- [ ] FAIL — notes: ___
