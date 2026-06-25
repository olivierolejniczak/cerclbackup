# Phase 03 — Peering: Invite & Accept

Wire the full triangle: **A↔B**, **A↔C**, **B↔C**.  
Each invite token is single-use. Send tokens via Signal, email, or any out-of-band channel.

> ↔ **Coordination required** — the generating machine and the accepting machine must both run their commands within the token's validity window.

---

## Step 03.1 — A invites B

**Machines:** A then B ↔

**[A] — generate token:**
```powershell
cerclbackup invite generate --name oliviero --password <password-A>
```

Copy the token printed and send it to B.

**[B] — accept:**
```bash
cerclbackup invite accept --token <TOKEN-FROM-A> --password <password-B>
```

**Expected:**
- A prints a token string (single line, base64-ish).
- B prints `Connected to peer 12D3…` and saves A to its buddy list.

- [ ] PASS
- [ ] FAIL — notes: ___

---

## Step 03.2 — A invites C

**Machines:** A then C ↔

**[A]:**
```powershell
cerclbackup invite generate --name foxy --password <password-A>
```

**[C]:**
```bash
cerclbackup invite accept --token <TOKEN-FROM-A> --password <password-C>
```

**Expected:** C prints `Connected to peer 12D3…` and saves A.

- [ ] PASS
- [ ] FAIL — notes: ___

---

## Step 03.3 — B invites C

**Machines:** B then C ↔

**[B]:**
```bash
cerclbackup invite generate --name foxy --password <password-B>
```

**[C]:**
```bash
cerclbackup invite accept --token <TOKEN-FROM-B> --password <password-C>
```

**Expected:** C saves B. Full triangle now wired.

- [ ] PASS
- [ ] FAIL — notes: ___

---

## Step 03.4 — Verify buddy status on A

**Machines:** A

```powershell
cerclbackup buddy status --password <password-A>
```

**Expected:** Two entries — `oliviero` and `foxy` — both marked **online**. Exit code 0.

- [ ] PASS — both buddies online
- [ ] FAIL — notes: ___

---

## Step 03.5 — Verify buddy lists on B and C

**Machines:** B · C (independent)

**[B]:**
```bash
cerclbackup buddy list --password <password-B>
```

**[C]:**
```bash
cerclbackup buddy list --password <password-C>
```

**Expected:**
- B lists: A (olivier) and C (foxy).
- C lists: A (olivier) and B (oliviero).

- [ ] PASS — B lists correct buddies
- [ ] PASS — C lists correct buddies
- [ ] FAIL — notes: ___
