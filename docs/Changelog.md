# Changelog

All notable changes are documented here, grouped by development phase.

---

## [Unreleased]

### Added
- `cerclbackup restore`: Merkle hash verification after reconstruct — mismatch deletes corrupted output and exits non-zero
- `cerclbackup serve --health-addr`: HTTP health endpoint (`/health` JSON + `/metrics` Prometheus plaintext)
- `README.md`: full command reference, architecture diagram, security model, project layout
- `cmd/cerclbackup-gui/`: full Wails desktop GUI (Svelte + TS frontend, French/English i18n) covering every CLI subcommand — setup, circle, backup/watch, restore, buddies, maintenance, settings — plus a system tray icon and hide-on-close behavior; replaces `cmd/cerclbackup-tray`
- `internal/api/`: extraction layer wrapping the CLI's business logic in JSON-serializable functions shared by both the CLI and the GUI
- `internal/api/dashboard.go`: aggregated health status (doctor + buddy + storage) for the GUI dashboard
- `cerclbackup serve --redial-interval`: `serve` now retries unreachable buddies (stored address, then DHT) on a periodic background loop (default 10 min), not just once at startup — a buddy's changed address is picked up automatically without restarting the daemon
- `docs/howto-windows-gui.md`: GUI-only walkthrough for non-technical Windows users (install, setup, pairing, backup, restore, tray)

### Removed
- `cmd/cerclbackup-tray/`: superseded by `cmd/cerclbackup-gui`, which owns both the window and the tray icon in a single process

---

## Phase 3e — Windows installer (WiX v4)

### Added
- `installer/cerclbackup.wxs`: per-machine MSI with WiX v4; MajorUpgrade removes previous version; Core feature installs both binaries + shortcuts
- `installer/license.rtf`: MIT license for WiX UI
- `scripts/build-installer.ps1`: cross-compiles both binaries with version ldflags, runs `wix build`, produces `CerclBackup-X.Y.Z.msi`
- HKCU Run entry registers systray binary to start at Windows login
- Start Menu shortcut for the systray binary
- `.github/workflows/release.yml`: triggers on `v*` tags; builds MSI and uploads to GitHub Release

---

## Phase 3d — Fyne systray

### Added
- `cmd/cerclbackup-tray/`: standalone systray binary using fyne.io/systray
- Programmatic 32x32 PNG icon (blue circle + white arrow) generated at runtime — no asset file required
- Menu: "CerclBackup vX.Y.Z", "Backup Now" (exec sibling binary), "Open Store…", "Quit"
- Status polling every 30s via `internal/tray/status.json` — shows last backup time and last file
- `internal/tray/status.go`: `Status{LastBackupAt, LastFile, Error}`; atomic write via tmp+rename
- `internal/version/version.go`: `AppVersion` variable stamped at build time via `-ldflags "-X ...AppVersion=X.Y.Z"`

---

## Phase 3c — zstd compression

### Added
- `internal/compress/compress.go`: thread-safe singleton zstd encoder/decoder; `Compress`, `Decompress`, `MaybeDecompress`
- `ManifestEntry.Compressed bool` field (backward compatible: missing = uncompressed)
- Backup pipeline compresses each chunk before RS encoding; restore decompresses after RS reconstruction
- `cerclbackup backup` reports compression ratio in log output

---

## Phase 3b — File versioning

### Added
- `ManifestEntry.Version int` (1-based) + `ManifestEntry.BackedAt time.Time`
- `manifest.Upsert` always creates a new FileID; `Version` = max(existing versions)+1
- `manifest.ListVersions(path)` — all versions sorted oldest → newest
- `manifest.Latest(path)` — most recent version for a path
- `manifest.PruneVersions(RetentionPolicy)` — keep-all window, keep-weekly window, monthly beyond, hard MaxVersions cap
- `manifest.DefaultRetentionPolicy()` — 30d keep-all, 90d keep-weekly, 50 max versions
- `cerclbackup restore --file <path>` — resolves latest version automatically
- `cerclbackup restore --version N` — picks a specific version
- `cerclbackup versions --file <path>` — lists all versions
- `cerclbackup list` deduplicates by path to show only latest; `--all` shows everything
- `cerclbackup prune` — `--dry-run`, `--max-versions`, `--keep-all-days`, `--keep-weekly-days`
- `cerclbackup diff --since <date>` — classifies changes as "new" or "updated"

---

## Phase 3a — Multi-circle isolation

### Added
- `internal/circle/circle.go`: `Circle{ID, Name, Salt, Scheme, CreatedAt}` with `Manager` backed by keystore extras `"circles_v1"`
- `bbcrypto.DeriveCircleKey(password, circleID, salt)` — Argon2id(password+"\x00"+circleID, circleSalt) per circle
- Circle key is independent of the master key; buddies in one circle cannot decrypt another circle's shards
- `cerclbackup circle list / add / rm` commands
- Default circle created automatically during `init`

---

## Phase 2 series — Core pipeline (completed before Phase 3)

### Phase 2i — Export / import / diff / auto-prune / doctor

- `cerclbackup export --file-id <uuid> --out <name.cbk>`: gzip+tar archive containing `manifest.json` + `shard-N.enc`
- `cerclbackup import --in <name.cbk>`: reverse; writes shards to store, inserts entry via `manifest.ImportEntry` (no-op if FileID exists)
- `cerclbackup diff --since <date>`: compares manifest entries to a cutoff date
- `--auto-prune` flag on `backup` and `watch`: calls `DefaultRetentionPolicy()` and deletes pruned shard sets from store
- `cerclbackup doctor`: 7-check health report (keystore, peer identity, store, manifest, last backup age, buddies parallel connect-with-timeout, disk space via `syscall.Statfs`)
- `internal/archive/archive.go`: `Write`, `Read`, `Filename`; nil shards written as zero-length tar entries so indices remain stable on import

### Phase 2h — Task Scheduler / buddy status / audit

- `scripts/install-task.ps1`: registers `CerclBackup-Watch` Windows scheduled task; AtLogon + hourly triggers; password stored in Credential Manager
- `cerclbackup buddy status`: parallel connect-with-timeout for each buddy; exits 2 if any offline
- `cerclbackup audit`: AES-256-GCM tag verification per shard; orphaned shard detection; exits 1 on corruption

### Phase 2g — Bandwidth throttle / e2e test / init wizard

- `internal/ratelimit/ratelimit.go`: token-bucket limiter; `Wait(n)`, `NewWriter(w, l)`; critical fix: `l.last` stamped after sleep to prevent double-crediting
- `--upload-kbps` flag wires `p2p.SetUploadRate`; uploads call `UploadLimiter.Wait(len(data))` before opening stream
- `cerclbackup init`: interactive first-run wizard; password prompt via `golang.org/x/term`; generates keystore, derives identity, shows BIP39 phrase, creates Default circle; `--no-prompt` for CI
- `scripts/e2e_test.sh`: 8-section end-to-end test covering init, backup, restore, versioning, compression ratio, exclude, storage, prune

### Phase 2f — Exclude patterns / prune CLI / storage accounting / scrub CLI

- `internal/exclude/exclude.go`: `Filter` matching basename, full path, every path component; `New(patterns)`, `Parse(csv)`, `Match(path)`, `Empty()`
- `cerclbackup backup --exclude` + `cerclbackup watch --exclude`
- `cerclbackup prune` command wired to `manifest.PruneVersions`; deletes pruned shard sets from store
- `cerclbackup storage`: manifest stats + on-disk walk + amplification ratio
- `cerclbackup scrub`: wires `scrubpkg.Manager.RunOnce`; exits 1 on any failed revival

### Phase 2e — CI workflows + directory watcher + restore UX

- `.github/workflows/ci.yml`: test (ubuntu + windows) + vet + cross-compile + artifact upload on every push
- `internal/watcher/watcher.go`: fsnotify watcher with per-path debounce; auto-watches new subdirs; Stop() cancels in-flight timers; WalkDir callback fixed to use `fs.DirEntry`
- `cerclbackup watch`: `--exclude`, `--auto-prune`, `--upload-kbps`, `--debounce`
- `cerclbackup restore --file <path>`: resolves latest version by path rather than requiring a UUID

### Phase 2d and earlier — Scrub, rebalance, manifest distribution, invite, email invite

- `internal/scrub`: periodic integrity check + silent revival from shard owners
- `internal/rebalance`: shard redistribution after buddy revocation
- `internal/manifdist`: push encrypted manifest blob to all connected buddies after backup
- `internal/invite`: signed invite tokens with expiry
- `internal/emailinvite`: dual-channel invite (payload via email, OOB words verbally)
- Offline queue: shards destined for offline buddies are queued and flushed on reconnect
- mDNS peer discovery (LAN) + DHT routing (Internet)

---

## Phase 1 — Foundation

### Added
- `pkg/protocol`: `ManifestEntry`, `RSScheme`, `Chunk`, `ShardLocation` wire types
- `pkg/wire`: 4-byte big-endian length-prefix + JSON framing
- `internal/crypto`: AES-256-GCM (`Encrypt`, `Decrypt`), HKDF (`DeriveFileKey`), Argon2id keystore, `DecryptShard`
- `internal/chunker`: 4 MB fixed-size chunker with per-chunk SHA-256
- `internal/codec`: Reed-Solomon wrapper (klauspost/reedsolomon); schemes 2+1, 3+2, 5+3, 6+4
- `internal/buddy`: shard `Store` (flat file, `ListAll`, `ShardRef`), peer `Registry`
- `internal/identity`: BIP39 12-word recovery phrase generation and verification
- `internal/manifest`: encrypted JSON index with `Upsert`, `Get`, `FindByPath`, `All`, `Remove`, `Latest`, `ListVersions`, `PruneVersions`, `ImportEntry`
- `internal/p2p`: libp2p host factory, ProtoPush, ProtoPull, ProtoInvite, ProtoManifest handlers
- `cmd/cerclbackup`: `backup`, `restore`, `list`, `serve`, `invite` commands; `--password` flag or `CERCLBACKUP_PASSWORD` env
- Go module: `github.com/cerclbackup/cerclbackup`; libp2p v0.48.0, BIP39, klauspost/reedsolomon, klauspost/compress
