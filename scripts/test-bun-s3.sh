#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ACCESS_KEY='bun-integration-access'
SECRET_KEY='bun-integration-secret'
BUCKET='gati-media'

if [[ -n "${LOCAL_S3_TEST_ENDPOINT:-}" ]]; then
  ENDPOINT="$LOCAL_S3_TEST_ENDPOINT"
  echo "Using provided local-s3 endpoint"
  LOCAL_S3_TEST_ENDPOINT="$ENDPOINT" \
  LOCAL_S3_TEST_ACCESS_KEY="$ACCESS_KEY" \
  LOCAL_S3_TEST_SECRET_KEY="$SECRET_KEY" \
  LOCAL_S3_TEST_BUCKET="$BUCKET" \
    bun run "$ROOT/scripts/bun-s3-integration.ts"
  exit $?
fi

TMP_DIR="$(mktemp -d "${TMPDIR:-/private/var/folders/7b/3zt0gmk10ynfm5byf21zymnm0000gn/T/}local-s3-bun-test.XXXXXX")"
SERVER_PID=''
cleanup() {
  if [[ -n "$SERVER_PID" ]]; then
    kill "$SERVER_PID" 2>/dev/null || true
    wait "$SERVER_PID" 2>/dev/null || true
  fi
  rm -rf "$TMP_DIR"
}
trap cleanup EXIT INT TERM

# Use a supplied build when available; otherwise compile into the disposable directory.
if [[ -n "${LOCAL_S3_TEST_BINARY:-}" ]]; then
  BINARY="$LOCAL_S3_TEST_BINARY"
elif [[ -x "$ROOT/local-s3" ]]; then
  BINARY="$ROOT/local-s3"
else
  BINARY="$TMP_DIR/local-s3"
  go build -o "$BINARY" .
fi
PORT="$(python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1", 0)); print(s.getsockname()[1]); s.close()')"
ENDPOINT="http://127.0.0.1:$PORT"
S3_LISTEN_ADDR="127.0.0.1:$PORT" \
S3_DATA_DIR="$TMP_DIR/data" \
S3_BUCKETS="$BUCKET" \
S3_ACCOUNT_BUN="$ACCESS_KEY:$SECRET_KEY" \
S3_ACCOUNT_BUN_BUCKETS="$BUCKET" \
  "$BINARY" >"$TMP_DIR/server.log" 2>&1 &
SERVER_PID=$!

ready=0
for _ in $(seq 1 100); do
  if curl --silent --output /dev/null "$ENDPOINT/"; then
    ready=1
    break
  fi
  if ! kill -0 "$SERVER_PID" 2>/dev/null; then
    echo "local-s3 exited before becoming ready" >&2
    echo "Check server startup configuration; diagnostic log was withheld to avoid exposing credentials" >&2
    exit 1
  fi
  sleep 0.1
done
if [[ "$ready" != 1 ]]; then
  echo "Timed out waiting for local-s3 readiness" >&2
  echo "Check server startup configuration; diagnostic log was withheld to avoid exposing credentials" >&2
  exit 1
fi

LOCAL_S3_TEST_ENDPOINT="$ENDPOINT" \
LOCAL_S3_TEST_ACCESS_KEY="$ACCESS_KEY" \
LOCAL_S3_TEST_SECRET_KEY="$SECRET_KEY" \
LOCAL_S3_TEST_BUCKET="$BUCKET" \
  bun run "$ROOT/scripts/bun-s3-integration.ts"
