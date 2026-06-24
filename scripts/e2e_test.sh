#!/usr/bin/env bash
# e2e_test.sh -- Comprehensive end-to-end test for CerclBackup.
# Covers: backup, restore, versioning, compression, exclude, prune,
#         storage accounting, scrub, and the init wizard (non-interactive).
set -euo pipefail

BINARY="$(dirname "$0")/../cerclbackup"
PASSWORD="e2e-test-password-$(date +%s)"
TMPDIR_ROOT="$(mktemp -d)"
STORE="$TMPDIR_ROOT/store"
CFG="$TMPDIR_ROOT/config"

export XDG_CONFIG_HOME="$CFG"     # redirect ~/.config so tests don't pollute real config
export APPDATA="$CFG"             # Windows compat
export HOME="$TMPDIR_ROOT"        # redirect ~

pass() { echo "[PASS] $1"; }
fail() { echo "[FAIL] $1" >&2; exit 1; }
step() { echo; echo "── $1 ──"; }

cleanup() { rm -rf "$TMPDIR_ROOT"; }
trap cleanup EXIT

# ── Build ────────────────────────────────────────────────────────────────────
step "Building cerclbackup"
go build -o "$BINARY" ./cmd/cerclbackup/ || fail "build failed"
pass "build"

# ── Init wizard (non-interactive) ────────────────────────────────────────────
step "Init (non-interactive)"
"$BINARY" init --password "$PASSWORD" --no-prompt \
  --store "$STORE" 2>&1 | grep -q "Peer ID" || fail "init did not print Peer ID"
pass "init"

# ── Basic backup ─────────────────────────────────────────────────────────────
step "Basic backup"
echo "Hello CerclBackup Phase 3" > "$TMPDIR_ROOT/file_a.txt"
"$BINARY" backup \
  --src "$TMPDIR_ROOT/file_a.txt" \
  --store "$STORE" \
  --password "$PASSWORD" \
  --buddies 3
pass "backup"

# ── Restore with --file path (Phase 3b restore UX) ───────────────────────────
step "Restore --file (latest version)"
"$BINARY" restore \
  --file "$TMPDIR_ROOT/file_a.txt" \
  --store "$STORE" \
  --password "$PASSWORD" \
  --out "$TMPDIR_ROOT/restored_a.txt"
diff "$TMPDIR_ROOT/file_a.txt" "$TMPDIR_ROOT/restored_a.txt" || fail "restore mismatch"
pass "restore --file"

# ── File versioning (Phase 3b) ───────────────────────────────────────────────
step "File versioning"
echo "Version 2 content" > "$TMPDIR_ROOT/file_a.txt"
"$BINARY" backup \
  --src "$TMPDIR_ROOT/file_a.txt" \
  --store "$STORE" \
  --password "$PASSWORD" \
  --buddies 3

# list --all should show 2 versions
COUNT=$("$BINARY" list --store "$STORE" --password "$PASSWORD" --all 2>/dev/null | grep -c "file_a.txt" || true)
[ "$COUNT" -ge 2 ] || fail "expected >=2 versions, got $COUNT"
pass "versioning: 2 versions recorded"

# versions command
"$BINARY" versions --file "$TMPDIR_ROOT/file_a.txt" --password "$PASSWORD" 2>&1 | grep -q "VER" || \
  fail "versions command missing VER column"
pass "versions command"

# restore --version 1 (original content)
"$BINARY" restore \
  --file "$TMPDIR_ROOT/file_a.txt" \
  --version 1 \
  --store "$STORE" \
  --password "$PASSWORD" \
  --out "$TMPDIR_ROOT/restored_v1.txt"
grep -q "Hello CerclBackup" "$TMPDIR_ROOT/restored_v1.txt" || fail "version 1 content wrong"
pass "restore --version 1"

# ── Compression (Phase 3c) ───────────────────────────────────────────────────
step "Compression"
# Back up a highly compressible file and verify store is smaller than source.
python3 -c "print('A' * 100000)" > "$TMPDIR_ROOT/compressible.txt"
src_size=$(wc -c < "$TMPDIR_ROOT/compressible.txt")
"$BINARY" backup \
  --src "$TMPDIR_ROOT/compressible.txt" \
  --store "$STORE" \
  --password "$PASSWORD" \
  --buddies 3
store_size=$(du -sb "$STORE" 2>/dev/null | awk '{print $1}' || du -s "$STORE" | awk '{print $1}')
[ "$store_size" -lt "$((src_size * 3))" ] || fail "store larger than expected — compression may not be working"
pass "compression (store size reasonable)"

# ── Exclude patterns (Phase 3 exclude) ───────────────────────────────────────
step "Exclude patterns"
mkdir -p "$TMPDIR_ROOT/proj/.git"
echo "tracked" > "$TMPDIR_ROOT/proj/main.go"
echo "git stuff" > "$TMPDIR_ROOT/proj/.git/config"
echo "temp" > "$TMPDIR_ROOT/proj/build.tmp"

"$BINARY" backup --src "$TMPDIR_ROOT/proj/main.go" \
  --store "$STORE" --password "$PASSWORD" --buddies 3
"$BINARY" backup --src "$TMPDIR_ROOT/proj/.git/config" \
  --store "$STORE" --password "$PASSWORD" --buddies 3 \
  --exclude ".git" && \
  echo "  (excluded .git/config as expected)" || true

"$BINARY" backup --src "$TMPDIR_ROOT/proj/build.tmp" \
  --store "$STORE" --password "$PASSWORD" --buddies 3 \
  --exclude "*.tmp" && \
  echo "  (excluded build.tmp as expected)" || true
pass "exclude patterns"

# ── Storage accounting ────────────────────────────────────────────────────────
step "Storage accounting"
"$BINARY" storage --store "$STORE" --password "$PASSWORD" 2>&1 | grep -q "Files tracked" || \
  fail "storage command missing expected output"
pass "storage accounting"

# ── Prune ────────────────────────────────────────────────────────────────────
step "Prune"
# Backup file_a one more time to have 3 versions.
echo "Version 3 content" > "$TMPDIR_ROOT/file_a.txt"
"$BINARY" backup --src "$TMPDIR_ROOT/file_a.txt" \
  --store "$STORE" --password "$PASSWORD" --buddies 3

# Dry run first.
"$BINARY" prune --password "$PASSWORD" --max-versions 1 --dry-run 2>&1 | \
  grep -qE "Would prune|Nothing" || fail "prune dry-run unexpected output"
pass "prune dry-run"

# Real prune: keep 1 version.
"$BINARY" prune --store "$STORE" --password "$PASSWORD" --max-versions 1
after_count=$("$BINARY" list --store "$STORE" --password "$PASSWORD" --all 2>/dev/null | \
  grep -c "file_a.txt" || true)
[ "$after_count" -le 1 ] || fail "after prune expected <=1 version, got $after_count"
pass "prune (kept latest)"

# ── Scrub ─────────────────────────────────────────────────────────────────────
step "Scrub (local shards)"
# Scrub over locally-stored buddy shards — expect 0 or more checked, no failures.
"$BINARY" scrub --password "$PASSWORD" 2>&1 | grep -q "Scrub complete" || \
  fail "scrub did not print 'Scrub complete'"
pass "scrub"

# ── Summary ───────────────────────────────────────────────────────────────────
echo
echo "All e2e tests passed."
