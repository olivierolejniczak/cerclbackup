# Phase 10 — Serve & Health Endpoint

The `serve` daemon exposes an optional HTTP endpoint on `--health-addr`. Test from a different machine to simulate real monitoring use.

> ↔ **Coordination required** — B runs the daemon, A queries it.

---

## Step 10.1 — Start serve with health endpoint on B

**Machines:** B

```bash
export CERCLBACKUP_PASSWORD='<password-B>'
cerclbackup serve --health-addr 0.0.0.0:7743
```

**Expected:**
- `health endpoint: http://0.0.0.0:7743/health` logged.
- `CerclBackup daemon running` with multiaddress.

- [ ] PASS
- [ ] FAIL — notes: ___

---

## Step 10.2 — Query /health from A

**Machines:** A

```powershell
Invoke-RestMethod http://<B-IP>:7743/health | ConvertTo-Json
# or with curl if installed:
curl http://<B-IP>:7743/health
```

**Expected:** HTTP 200. JSON body similar to:

```json
{
  "status": "ok",
  "version": "1.0.0",
  "peer_id": "12D3...",
  "peers": 1,
  "uptime_s": 12
}
```

- [ ] PASS — status "ok", version "1.0.0"
- [ ] FAIL — notes: ___

---

## Step 10.3 — Query /metrics from A

**Machines:** A

```powershell
curl http://<B-IP>:7743/metrics
```

**Expected:** Prometheus plaintext format with all four metrics:

```
cerclbackup_uptime_seconds N
cerclbackup_peers_connected N
cerclbackup_buddies_registered N
cerclbackup_shards_stored N
```

- [ ] PASS — all 4 metric lines present
- [ ] FAIL — notes: ___

---

## Step 10.4 — /health responds during active backup

**Machines:** A · B ↔

Start a backup on A while B's serve is running, then immediately query /health.

**[A] — start backup (background):**
```powershell
Start-Job {
    cerclbackup backup `
        --src "$env:USERPROFILE\cercltest\hello.txt" `
        --buddies 3 `
        --password <password-A>
}
# Immediately query health:
Invoke-RestMethod http://<B-IP>:7743/health
```

**Expected:** `/health` returns within 200 ms. `peers` reflects connected nodes.

- [ ] PASS — non-blocking response during backup
- [ ] FAIL — notes: ___

---

## Step 10.5 — Port reachability check

**Machines:** A

```powershell
Test-NetConnection -ComputerName <B-IP> -Port 7743
```

**Expected:** `TcpTestSucceeded: True`

- [ ] PASS
- [ ] FAIL — notes: ___
