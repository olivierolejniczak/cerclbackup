# CerclBackup v1.0.0 — Manual Test Plan

Full end-to-end test covering all features across three real machines.

---

## Machines

| ID | User | OS | Notes |
|----|------|----|-------|
| **A** | olivier.olejniczak@alticap.com | Windows 11 Pro | Primary user, GUI + systray |
| **B** | olivierolejniczak@gmail.com | Linux DietPi | Headless, CLI only |
| **C** | foxydelongvillers@gmail.com | Linux DietPi | Headless, CLI only |

## Network topology

All three machines form a full triangle (A↔B, A↔C, B↔C).  
RS scheme: **2+1** (2 data shards + 1 parity = 3 total). Tolerates losing **1 of 3** machines.

```
        A (Win11)
       / \
      /   \
     B --- C
  (DietPi) (DietPi)
```

## Conventions

- `[A]` — run on olivier's Windows machine
- `[B]` — run on oliviero's DietPi
- `[C]` — run on foxy's DietPi
- `[A+B]` — run on both machines (independently unless noted)
- `↔` — coordination required: both machines active at the same time
- `<password-A>` / `<password-B>` / `<password-C>` — fill in the actual passwords once and keep consistent
- `<new-password-A>` — password set during Phase 13 (passwd change); used from Phase 14 onward

## Checkbox usage

Each step has:
```
- [ ] PASS
- [ ] FAIL — notes: ___
```
GitHub renders these as interactive checkboxes when viewing the file.

## Phases

| # | File | Description |
|---|------|-------------|
| 00 | [00-prerequisites.md](00-prerequisites.md) | Network reachability, record IPs |
| 01 | [01-installation.md](01-installation.md) | MSI on Windows, binary on DietPi |
| 02 | [02-init.md](02-init.md) | First-run wizard, BIP39 phrases |
| 03 | [03-peering.md](03-peering.md) | Invite tokens, full triangle wiring |
| 04 | [04-backup.md](04-backup.md) | First backup, shard distribution |
| 05 | [05-restore.md](05-restore.md) | Restore + integrity hash check |
| 06 | [06-fault-tolerance.md](06-fault-tolerance.md) | Quorum failure scenarios |
| 07 | [07-versioning.md](07-versioning.md) | Multiple versions, selective restore |
| 08 | [08-prune.md](08-prune.md) | Retention policy, dry-run, apply |
| 09 | [09-watch.md](09-watch.md) | fsnotify watcher, debounce, counter |
| 10 | [10-serve.md](10-serve.md) | Daemon + /health + /metrics |
| 11 | [11-maintenance.md](11-maintenance.md) | Doctor, audit, scrub, storage, corruption |
| 12 | [12-config.md](12-config.md) | config.yaml, show, init, flag defaults |
| 13 | [13-passwd.md](13-passwd.md) | Password change, old rejected, new works |
| 14 | [14-buddy-rm.md](14-buddy-rm.md) | Remove C, auto-rebalance to B |
| 15 | [15-export-import.md](15-export-import.md) | .cbk archive, transfer to B, restore |
| 16 | [16-windows.md](16-windows.md) | Systray, Task Scheduler (A only) |

**Total: ~55 steps across 17 phases.**
