#!/usr/bin/env bash
# End-to-end backup/restore tests for CerclBackup.
# Usage: ./scripts/e2e_test.sh [path/to/cerclbackup-linux]
set -euo pipefail

BIN=${1:-./build/cerclbackup-linux}
PASS=0; FAIL=0

run_test() {
  local label=$1 size_bytes=$2 buddies=$3 kill_shard=$4
  local TDIR; TDIR=$(mktemp -d)
  local SRC=$TDIR/src.bin STORE=$TDIR/store OUT=$TDIR/out.bin
  local orig_home=$HOME; export HOME=$TDIR  # isolate keystore + manifest

  dd if=/dev/urandom of=$SRC bs=1 count=$size_bytes 2>/dev/null
  "$BIN" backup  --src $SRC --store $STORE --password pw --buddies $buddies 2>/dev/null

  if [ -n "$kill_shard" ]; then
    local FDIR; FDIR=$(ls "$STORE"/)
    rm -f "$STORE/$FDIR/${kill_shard}.shard"
  fi

  local FILE_ID; FILE_ID=$("$BIN" list --store $STORE --password pw 2>/dev/null | awk 'NR>2{print $1;exit}')
  "$BIN" restore --file-id "$FILE_ID" --store $STORE --out $OUT --password pw 2>/dev/null

  if cmp -s "$SRC" "$OUT" 2>/dev/null; then
    echo "PASS  $label"; PASS=$((PASS+1))
  else
    echo "FAIL  $label — src=$(wc -c<"$SRC") out=$(wc -c<"$OUT" 2>/dev/null||echo ERR)"
    FAIL=$((FAIL+1))
  fi

  export HOME=$orig_home
  rm -rf "$TDIR"
}

echo "=== CerclBackup e2e tests ==="
run_test "1 MB, 3 buddies, tous shards"            1000000   3 ""
run_test "1 MB, 5 buddies, tous shards"            1000000   5 ""
run_test "4194305 B (multi-chunk), 5 buddies"      4194305   5 ""
run_test "7 MB, 3 buddies, tous shards"            7340032   3 ""
run_test "7 MB, 5 buddies, tous shards"            7340032   5 ""
run_test "8 MB exact, 5 buddies"                   8388608   5 ""
run_test "9 MB, 8 buddies, tous shards"            9437184   8 ""
run_test "3 MB, 3 buddies, shard 0 manquant"       3000000   3 "0"
run_test "6 MB, 5 buddies, shard 1 manquant"       6000000   5 "1"
run_test "9 MB, 8 buddies, shard 2 manquant"       9000000   8 "2"

echo ""
echo "Results: ${PASS} passed, ${FAIL} failed"
[ $FAIL -eq 0 ]
