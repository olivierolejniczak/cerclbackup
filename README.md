# CerclBackup

P2P encrypted backup between trusted friends — no cloud service, no subscription, no single point of failure.

Your files are split into chunks, compressed with zstd, protected by Reed-Solomon erasure coding, encrypted with AES-256-GCM, and distributed to buddies over libp2p. Recovery works as long as a minimum quorum of buddies is reachable.

> **AGPL-3.0** — any SaaS built on this must publish its source code.

---

## How it works

```
  Your machine                                   Buddy A           Buddy B
  ┌──────────┐  chunk   zstd    Reed-Solomon     ┌──────────┐      ┌──────────┐
  │  file    │────────────────────────────────►  │ shard-0  │      │ shard-1  │
  └──────────┘  ↓                                │ .enc     │      │ .enc     │
          AES-256-GCM                            └──────────┘      └──────────┘
          HKDF key per file                      Buddy C
                                                 ┌──────────┐
                                                 │ shard-2  │
                                                 │ .enc     │  (parity)
                                                 └──────────┘
```

1. Files are chunked (4 MB), compressed (zstd), and encoded with Reed-Solomon (default 3+2).
2. Each shard is encrypted with AES-256-GCM using a key derived per file via HKDF.
3. Shards push to buddies over libp2p (mDNS on LAN, DHT on Internet, offline queue).
4. The manifest (encrypted file index with full version history) is distributed to buddies.
5. Restore works from any DataShards-of-TotalShards subset of online buddies.
6. Every restore verifies the Merkle hash of reconstructed chunks against the manifest.

---

## Quick start

```bash
# 1. Initialise — generates Ed25519 identity, AES master key, 12-word recovery phrase
cerclbackup init

# 2. Connect two machines
cerclbackup invite generate --name alice   # on machine A: outputs a token
cerclbackup invite accept --token <TOKEN>  # on machine B: accepts and connects

# 3. Back up a directory tree
cerclbackup backup --src ~/Documents

# 4. Restore a file (latest version)
cerclbackup restore --file ~/Documents/report.pdf --out /tmp/report.pdf
```

---

## Installation

### Pre-built Windows installer

Download `CerclBackup-X.Y.Z.msi` from [Releases](../../releases) and run it.  
The installer registers the systray binary to start at login and creates a Start Menu shortcut.

### Build from source

Requires **Go 1.21+**.

```bash
git clone https://github.com/cerclbackup/cerclbackup
cd cerclbackup
go build ./cmd/cerclbackup/        # CLI daemon
go build ./cmd/cerclbackup-tray/   # systray (GOOS=windows for cross-compile)
```

### Windows MSI (build)

```powershell
# Requires: dotnet tool install --global wix
.\scripts\build-installer.ps1 -Version 1.0.0
```

---

## Command reference

### First run

| Command | Description |
|---|---|
| `cerclbackup init` | Interactive wizard: generate keys, show BIP39 phrase, create Default circle |
| `cerclbackup init --no-prompt` | Non-interactive (CI): reads password from `CERCLBACKUP_PASSWORD` env var |

### Backup

| Command | Description |
|---|---|
| `cerclbackup backup --src <dir>` | Back up all files in a directory |
| `cerclbackup backup --src <dir> --exclude ".git,*.tmp"` | Exclude glob patterns (comma-separated) |
| `cerclbackup backup --src <dir> --upload-kbps 500` | Cap upload bandwidth in KB/s |
| `cerclbackup backup --src <dir> --auto-prune` | Apply retention policy after backup |
| `cerclbackup watch --src <dir>` | fsnotify watcher — debounces changes and backs up automatically |
| `cerclbackup watch --src <dir> --debounce 10s` | Override debounce interval (default 3s) |

### Restore

| Command | Description |
|---|---|
| `cerclbackup restore --file <path> --out <dst>` | Restore latest version of a file |
| `cerclbackup restore --file <path> --version N --out <dst>` | Restore specific version number |
| `cerclbackup restore --file-id <uuid> --out <dst>` | Restore by exact file ID (advanced) |

Restore always verifies the Merkle hash of reconstructed chunks. A mismatch deletes the output and exits non-zero.

### Listing and versioning

| Command | Description |
|---|---|
| `cerclbackup list` | Latest version of every backed-up file |
| `cerclbackup list --all` | Every version of every file |
| `cerclbackup versions --file <path>` | All versions for a specific file with dates and sizes |
| `cerclbackup diff --since 2026-01-01` | Files added or updated since a date (YYYY-MM-DD or RFC3339) |

### Retention

| Command | Description |
|---|---|
| `cerclbackup prune` | Apply default retention policy: 30d keep-all, 90d keep-weekly, monthly beyond |
| `cerclbackup prune --dry-run` | Show what would be deleted without deleting |
| `cerclbackup prune --max-versions 10` | Hard cap per file path |
| `cerclbackup prune --keep-all-days 7 --keep-weekly-days 30` | Override retention windows |

### Buddy management

| Command | Description |
|---|---|
| `cerclbackup buddy status` | Check reachability of all registered buddies (parallel, exits 2 if any offline) |
| `cerclbackup buddy list` | List registered buddies and their addresses |
| `cerclbackup buddy add --addr <multiaddr>` | Manually register a buddy |
| `cerclbackup buddy rm --peer-id <id>` | Remove a buddy |

### Invite

| Command | Description |
|---|---|
| `cerclbackup invite generate --name <name>` | Generate invite token (share the token to your buddy) |
| `cerclbackup invite accept --token <token>` | Accept an invite and register the peer |

### Circles

Circles let you isolate key material across groups of buddies. Each circle uses an independent Argon2id-derived key so buddies in one circle cannot decrypt data from another.

| Command | Description |
|---|---|
| `cerclbackup circle list` | List all circles |
| `cerclbackup circle add --name <name> --scheme 3/2` | Create a circle with a specific RS scheme |
| `cerclbackup circle rm --name <name> --confirm-name <name>` | Remove a circle (destructive, requires confirmation) |

### Daemon

| Command | Description |
|---|---|
| `cerclbackup serve` | Run background daemon: libp2p server, mDNS, DHT, scrub every 6h |
| `cerclbackup serve --health-addr 127.0.0.1:7743` | Enable HTTP health and metrics endpoint |
| `cerclbackup serve --upload-kbps 500` | Cap upload bandwidth |

#### Health endpoint

```
GET /health  →  {"status":"ok","version":"1.0.0","peer_id":"12D3...","peers":3,"uptime_s":3600}
GET /metrics →  Prometheus-style plaintext (cerclbackup_uptime_seconds, _peers_connected, _buddies_registered, _shards_stored)
```

### Maintenance

| Command | Description |
|---|---|
| `cerclbackup doctor` | 7-check report: keystore, peer identity, store, manifest, last backup age, buddies, disk space |
| `cerclbackup scrub` | Verify all locally-stored shards; attempts silent revival from owners if corrupt |
| `cerclbackup audit` | Validate AES-GCM tags for every shard; detects orphans; exits 1 on corruption |
| `cerclbackup storage` | Manifest stats, on-disk usage, and amplification ratio |

### Import / export

| Command | Description |
|---|---|
| `cerclbackup export --file-id <uuid> --out archive.cbk` | Export a backed-up file as a portable `.cbk` archive |
| `cerclbackup import --in archive.cbk` | Import a `.cbk` archive into the local store |

`.cbk` archives are gzip-compressed tarballs containing `manifest.json` and `shard-N.enc` entries. Shards remain AES-encrypted — the archive is confidential without additional wrapping.

---

## Reed-Solomon schemes

| Scheme | Total buddies | Tolerated failures | Storage overhead |
|---|---|---|---|
| 2+1 | 3 | 1 | +50% |
| 3+2 | 5 | 2 | +67% |
| 5+3 | 8 | 3 | +60% |
| 6+4 | 10 | 4 | +67% |

---

## Security model

- **Client-side encryption.** The master key never leaves your machine; keystore is AES-256-GCM encrypted at rest with Argon2id key derivation from your password.
- **Per-file key isolation.** Each file gets `HKDF(masterKey, fileID || shardIndex)` — a buddy holding multiple shards of the same file cannot link them.
- **Zero-knowledge buddies.** Buddies store only unidentifiable encrypted blobs.
- **Recovery phrase.** A 12-word BIP39 phrase regenerates the master key offline; write it down, keep it safe.
- **Integrity on restore.** The Merkle hash (SHA-256 of chunk SHA-256s) is verified post-restore. Corrupted output is deleted before it can mislead.
- **Proactive scrub.** Every 6 hours the daemon verifies shard integrity and silently revives corrupted shards from their owners — before a restore ever needs them.
- **No telemetry.** The binary contacts no external server except the buddies you registered.

---

## Configuration

State directory: `$XDG_CONFIG_HOME/cerclbackup/` (Linux/macOS) or `%APPDATA%\CerclBackup\` (Windows).

| File / directory | Purpose |
|---|---|
| `keystore.enc` | Encrypted master key, peer identity, circle metadata |
| `manifest.enc` | Encrypted file index with version history |
| `buddies.json` | Registered peer addresses |
| `shards/` | Shards stored on behalf of your buddies |
| `queue.json` | Delivery queue for offline buddies |

### Environment variables

| Variable | Purpose |
|---|---|
| `CERCLBACKUP_PASSWORD` | Keystore password (for CI, scripts, scheduled tasks) |
| `CERCLBACKUP_SRC` | Source directory (used by the systray "Backup Now" action) |

---

## Windows Task Scheduler

For always-on backup without the systray GUI:

```powershell
# Register: runs at logon + hourly, password stored in Credential Manager
.\scripts\install-task.ps1 -SrcDir C:\Users\alice\Documents

# Uninstall
.\scripts\install-task.ps1 -Uninstall
```

---

## Development

```bash
# Unit tests (all packages)
go test ./...

# Lint
go vet ./...

# End-to-end test (Linux)
go build -o /tmp/cerclbackup ./cmd/cerclbackup/
bash scripts/e2e_test.sh
```

CI (GitHub Actions) runs on every push across Linux and Windows. Release MSIs are built automatically on `v*` tags.

---

## Project layout

```
cerclbackup/
├── cmd/
│   ├── cerclbackup/          # CLI — all commands
│   └── cerclbackup-tray/     # Windows systray binary
├── internal/
│   ├── archive/              # .cbk portable archive format
│   ├── buddy/                # Buddy registry + shard store
│   ├── chunker/              # 4 MB chunker with hash
│   ├── circle/               # Multi-circle key isolation (Argon2id per circle)
│   ├── codec/                # Reed-Solomon (klauspost/reedsolomon)
│   ├── compress/             # zstd compression (singleton encoder/decoder)
│   ├── crypto/               # AES-256-GCM, HKDF, Argon2id, keystore
│   ├── emailinvite/          # Email invite (dual-channel: payload + OOB words)
│   ├── exclude/              # Glob exclusion filter
│   ├── identity/             # BIP39 recovery phrase generation
│   ├── invite/               # Invite token lifecycle
│   ├── manifdist/            # Manifest distribution to buddies
│   ├── manifest/             # Encrypted file index with version history
│   ├── p2p/                  # libp2p host, push/pull protocols, mDNS, DHT, queue
│   ├── ratelimit/            # Token-bucket bandwidth limiter
│   ├── rebalance/            # Shard redistribution after buddy revocation
│   ├── scrub/                # Periodic integrity check + silent revival
│   ├── storage/              # On-disk accounting
│   ├── tray/                 # Status file for systray IPC
│   ├── version/              # AppVersion stamped at build time
│   └── watcher/              # fsnotify directory watcher with debounce
├── installer/                # WiX v4 MSI descriptor
├── pkg/
│   ├── protocol/             # Shared wire types (ManifestEntry, RSScheme, …)
│   └── wire/                 # 4-byte length-prefix + JSON framing
├── scripts/
│   ├── build-installer.ps1   # Cross-compile + wix build
│   ├── e2e_test.sh           # End-to-end integration test
│   └── install-task.ps1      # Windows Task Scheduler registration
└── docs/
    └── Changelog.md          # Per-phase history
```
