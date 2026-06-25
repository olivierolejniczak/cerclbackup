# Phase 02 — First Run: Init

Each machine generates its own Ed25519 peer identity, AES-256 master key, and 12-word BIP39 recovery phrase.  
Run independently on each machine. **Write down the recovery phrase on paper — it cannot be recovered if lost.**

---

## Step 02.1 — Init on A (Windows, interactive)

**Machines:** A

```powershell
cerclbackup init
```

The wizard will:
1. Prompt for a password (enter twice to confirm).
2. Print the 12-word BIP39 recovery phrase.
3. Print the peer ID (starts with `12D3…`).
4. Create the Default circle.

**Record phrase for A:**

> ___ ___ ___ ___ ___ ___ ___ ___ ___ ___ ___ ___

**Record peer ID of A:**

> `12D3...` _______________

- [ ] PASS — phrase recorded, peer ID noted
- [ ] FAIL — notes: ___

---

## Step 02.2 — Init on B (DietPi, non-interactive)

**Machines:** B

```bash
export CERCLBACKUP_PASSWORD='<password-B>'
cerclbackup init --no-prompt
```

**Record phrase for B:**

> ___ ___ ___ ___ ___ ___ ___ ___ ___ ___ ___ ___

**Record peer ID of B:**

> `12D3...` _______________

- [ ] PASS — phrase recorded, peer ID noted
- [ ] FAIL — notes: ___

---

## Step 02.3 — Init on C (DietPi, non-interactive)

**Machines:** C

```bash
export CERCLBACKUP_PASSWORD='<password-C>'
cerclbackup init --no-prompt
```

**Record phrase for C:**

> ___ ___ ___ ___ ___ ___ ___ ___ ___ ___ ___ ___

**Record peer ID of C:**

> `12D3...` _______________

- [ ] PASS — phrase recorded, peer ID noted
- [ ] FAIL — notes: ___

---

## Step 02.4 — Verify keystore created

**Machines:** A · B · C (independent)

**[A]:**
```powershell
dir "$env:APPDATA\CerclBackup\keystore.enc"
```

**[B] and [C]:**
```bash
ls -lh ~/.config/cerclbackup/keystore.enc
```

**Expected:**
- File exists on each machine, size 200–500 bytes.
- Permissions on Linux: `-rw-------` (mode 0600).

- [ ] PASS — A keystore present
- [ ] PASS — B keystore present, mode 0600
- [ ] PASS — C keystore present, mode 0600
- [ ] FAIL — notes: ___
