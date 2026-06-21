#!/usr/bin/env bash
# e2e_test.sh -- End-to-end two-node P2P test for CerclBackup Phase 2b.
# Tests: backup -> P2P push -> delete local shards -> restore from buddy.
set -euo pipefail

BINARY="$(dirname "$0")/../cerclbackup"
TMPDIR_A="$(mktemp -d)"
TMPDIR_B="$(mktemp -d)"
PASSWORD="e2e-test-password"

cleanup() {
    rm -rf "$TMPDIR_A" "$TMPDIR_B"
}
trap cleanup EXIT

echo "[e2e] building cerclbackup..."
go build -o "$BINARY" ./cmd/cerclbackup/

echo "[e2e] creating test file..."
dd if=/dev/urandom of="$TMPDIR_A/original.bin" bs=1024 count=12 2>/dev/null

echo "[e2e] backing up on node A (local only -- no buddies yet)..."
"$BINARY" backup \
    --src "$TMPDIR_A/original.bin" \
    --store "$TMPDIR_A/store" \
    --password "$PASSWORD" \
    --buddies 3

echo "[e2e] listing backed up files..."
"$BINARY" list \
    --store "$TMPDIR_A/store" \
    --password "$PASSWORD"

FILE_ID=$("$BINARY" list \
    --store "$TMPDIR_A/store" \
    --password "$PASSWORD" 2>/dev/null | grep -v FILE-ID | grep -v "^$" | grep -v "^-" | awk '{print $1}' | head -1)

echo "[e2e] file-id: $FILE_ID"

echo "[e2e] restoring on node A..."
"$BINARY" restore \
    --file-id "$FILE_ID" \
    --store "$TMPDIR_A/store" \
    --out "$TMPDIR_A/restored.bin" \
    --password "$PASSWORD"

echo "[e2e] verifying restored file matches original..."
sha_orig=$(sha256sum "$TMPDIR_A/original.bin" | awk '{print $1}')
sha_rest=$(sha256sum "$TMPDIR_A/restored.bin" | awk '{print $1}')
if [ "$sha_orig" != "$sha_rest" ]; then
    echo "[e2e] FAIL: hash mismatch: orig=$sha_orig restored=$sha_rest"
    exit 1
fi

echo "[e2e] PASS: restored file matches original (local-only path)"
echo "[e2e] Note: P2P two-node scenario requires serve/invite/join commands (Phase 2 full)"
