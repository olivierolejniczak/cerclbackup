# Phase 00 — Prerequisites & Network

Confirm all three machines can reach each other before any software is installed.  
If machines are on different LANs, port **4001/tcp** must be forwarded on each router.

---

## Step 00.1 — Record machine details

**Machines:** A · B · C (independent)

Run the following on each machine and fill in the table below.

**[B] and [C]:**
```bash
ip addr show | grep 'inet '
hostname
```

**[A]:**
```powershell
ipconfig | findstr "IPv4"
hostname
```

Fill in before proceeding:

| Machine | Hostname | LAN IP | Public IP (if NAT) |
|---------|----------|--------|--------------------|
| A | | | |
| B | | | |
| C | | | |

- [ ] PASS — table complete
- [ ] FAIL — notes: ___

---

## Step 00.2 — Ping B and C from A

**Machines:** A

```powershell
ping <B-IP> -n 4
ping <C-IP> -n 4
```

**Expected:** Replies from both IPs, no "Request timed out", RTT < 200 ms.

- [ ] PASS
- [ ] FAIL — notes: ___

---

## Step 00.3 — Ping A from B and C

**Machines:** B · C (independent)

**[B]:**
```bash
ping -c 4 <A-IP>
```

**[C]:**
```bash
ping -c 4 <A-IP>
```

**Expected:** Replies from A on both machines.

- [ ] PASS — B reaches A
- [ ] PASS — C reaches A
- [ ] FAIL — notes: ___

---

> **Note:** Port 4001 will be verified after `cerclbackup serve` is started in Phase 10.  
> If any machine is behind NAT, configure port forwarding now: `4001/tcp` → LAN IP of that machine.
