#!/usr/bin/env bash
# assert-demo.sh — CI-friendly smoke: serve + UE→cloud must reach delivered.
#
# Exit 0 on success; non-zero on timeout / wrong status / curl failure.
# Does not use Docker. Fast profile (sos_burst).
#
# Usage:
#   ./scripts/assert-demo.sh
#   make assert-demo

set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

PORT="${ASSERT_PORT:-18091}"
API="http://127.0.0.1:${PORT}"
PROFILE="${ASSERT_PROFILE:-testdata/profiles/sos_burst.yaml}"
TIMEOUT_SEC="${ASSERT_TIMEOUT_SEC:-30}"
SERVE_PID=""

cleanup() {
  if [[ -n "${SERVE_PID}" ]] && kill -0 "${SERVE_PID}" 2>/dev/null; then
    kill "${SERVE_PID}" 2>/dev/null || true
    wait "${SERVE_PID}" 2>/dev/null || true
  fi
}
trap cleanup EXIT

if [[ ! -x ./ntnbox ]]; then
  go build -o ntnbox ./cmd/ntnbox/
fi

./ntnbox serve --profile "${PROFILE}" --addr "127.0.0.1:${PORT}" >/tmp/ntnbox-assert-serve.log 2>&1 &
SERVE_PID=$!

# Wait for health / condition.
ready=0
for _ in $(seq 1 50); do
  if curl -sf "${API}/devices/sandbox-0/condition" >/dev/null 2>&1; then
    ready=1
    break
  fi
  sleep 0.1
done
if [[ "${ready}" -ne 1 ]]; then
  echo "assert-demo: serve did not become ready (see /tmp/ntnbox-assert-serve.log)" >&2
  exit 1
fi

RESP=$(curl -sf -X POST "${API}/devices/sandbox-0/messages" \
  -H 'Content-Type: application/json' \
  -d '{"to":"cloud","body":"assert-demo"}')
MID=$(printf '%s' "${RESP}" | sed -n 's/.*"id":"\([^"]*\)".*/\1/p')
if [[ -z "${MID}" ]]; then
  echo "assert-demo: no message id in response: ${RESP}" >&2
  exit 1
fi

deadline=$((SECONDS + TIMEOUT_SEC))
status=""
while (( SECONDS < deadline )); do
  status=$(curl -sf "${API}/messages/${MID}" | sed -n 's/.*"status":"\([^"]*\)".*/\1/p' || true)
  if [[ "${status}" == "delivered" ]]; then
    echo "assert-demo: OK  ${MID} → delivered"
    exit 0
  fi
  if [[ "${status}" == "failed" ]]; then
    echo "assert-demo: message failed: ${MID}" >&2
    exit 1
  fi
  sleep 0.2
done

echo "assert-demo: timeout after ${TIMEOUT_SEC}s (last status=${status:-unknown}) mid=${MID}" >&2
exit 1
