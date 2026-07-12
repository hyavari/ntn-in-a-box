#!/usr/bin/env bash
# demo-messaging.sh — Visualize store-and-forward (Network and/or UE).
#
# Uses `ntnbox serve` only — no Docker, no Linux netns. Works on macOS natively.
#
# Usage:
#   ./scripts/demo-messaging.sh                 # Network (cloud) smoke, exit
#   ./scripts/demo-messaging.sh --to ue         # UE→UE smoke (devices 2)
#   ./scripts/demo-messaging.sh --gui           # smoke + /ui (both destinations)
#   ./scripts/demo-messaging.sh --help

set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

ADDR_PORT=18082
API="http://127.0.0.1:${ADDR_PORT}"
GUI_URL="http://localhost:${ADDR_PORT}/ui"
PROFILE="sos_burst"
TO="network" # network | ue | both
WANT_GUI=0

usage() {
  cat <<'EOF'
Usage: ./scripts/demo-messaging.sh [profile] [--to network|ue|both] [--gui]

  Uses `ntnbox serve` (native, no Docker).

  profile     Profile under testdata/profiles/ (default: sos_burst)
  --to        Destination for the smoke send:
                network  UE → cloud (default; immediate deliver)
                ue       UE → sandbox-1 (waits for peer coverage)
                both     network then ue
  --ue2ue     Alias for --to ue
  --gui       Keep serve up; open Messages panel (Network | UE selector + Activity log).
              Always starts with --devices 2 so both destinations work.

Watch terminal lines like:
  ntnbox: message msg-…  sandbox-0 → cloud       queued|in_flight|delivered
  ntnbox: message msg-…  sandbox-0 → sandbox-1   queued … (later) delivered

Examples:
  ./scripts/demo-messaging.sh
  ./scripts/demo-messaging.sh --gui
  ./scripts/demo-messaging.sh --to ue
  ./scripts/demo-messaging.sh --gui --to both

See COOKBOOK.md §6.
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --help|-h) usage; exit 0 ;;
    --gui) WANT_GUI=1; shift ;;
    --ue2ue) TO=ue; shift ;;
    --to)
      shift
      TO="${1:-}"
      case "$TO" in
        network|ue|both) ;;
        *)
          echo "error: --to must be network, ue, or both" >&2
          exit 1
          ;;
      esac
      shift
      ;;
    --tui)
      echo "error: --tui was removed from this demo (it pulled in Docker on macOS)." >&2
      echo "  Messaging:  ./scripts/demo-messaging.sh --gui" >&2
      exit 1
      ;;
    -*)
      echo "error: unknown flag: $1" >&2
      usage >&2
      exit 1
      ;;
    *) PROFILE="$1"; shift ;;
  esac
done

PROFILE_PATH="testdata/profiles/${PROFILE}.yaml"
if [[ ! -f "$PROFILE_PATH" ]]; then
  echo "error: profile not found: $PROFILE_PATH" >&2
  ls testdata/profiles/*.yaml | sed 's|.*/||; s|\.yaml||' | sed 's/^/  /' >&2
  exit 1
fi

NEED_PEER=0
if [[ "$WANT_GUI" -eq 1 || "$TO" == "ue" || "$TO" == "both" ]]; then
  NEED_PEER=1
fi

echo "Building ntnbox..."
go build -o ntnbox ./cmd/ntnbox/

echo "=== NTN-in-a-Box — messaging demo ==="
echo "Profile: $PROFILE"
echo "API:     $API  (serve — no Docker)"
echo "Smoke:   --to $TO"
[[ "$WANT_GUI" -eq 1 ]] && echo "GUI:     $GUI_URL  (Network | UE selector)"
echo

NTN_PID=""
DEMO_LOG=""
cleanup() {
  if [[ -n "$NTN_PID" ]]; then
    kill "$NTN_PID" 2>/dev/null || true
    wait "$NTN_PID" 2>/dev/null || true
  fi
  if [[ -n "$DEMO_LOG" && -f "$DEMO_LOG" ]]; then
    rm -f "$DEMO_LOG"
  fi
}
trap cleanup EXIT

DEMO_LOG=$(mktemp "${TMPDIR:-/tmp}/ntnbox-messaging.XXXXXX")
chmod 600 "$DEMO_LOG"

SERVE_ARGS=(serve --profile "$PROFILE_PATH" --addr "127.0.0.1:${ADDR_PORT}")
if [[ "$NEED_PEER" -eq 1 ]]; then
  SERVE_ARGS+=(--devices 2 --phase-sec 240)
  echo "Devices: sandbox-0 + sandbox-1 (phase-sec 240)"
fi
# Capture ntnbox stderr only (lifecycle lines; no message bodies).
./ntnbox "${SERVE_ARGS[@]}" >/dev/null 2> >(tee "$DEMO_LOG" >&2) &
NTN_PID=$!

for _ in $(seq 1 150); do
  curl -sf "$API/health" >/dev/null 2>&1 && break
  sleep 0.1
done
if ! curl -sf "$API/health" >/dev/null 2>&1; then
  echo "error: ntnbox did not become ready" >&2
  [[ -f "$DEMO_LOG" ]] && cat "$DEMO_LOG" >&2
  exit 1
fi

echo "--- capabilities ---"
curl -s "$API/devices/sandbox-0/capabilities"
echo
echo

send_cloud() {
  echo "--- POST sandbox-0 → cloud (network) ---"
  local POST MID STATUS
  POST=$(curl -s -X POST "$API/devices/sandbox-0/messages" \
    -H 'Content-Type: application/json' \
    -d '{"to":"cloud","body":"SOS from demo-messaging"}')
  echo "$POST"
  MID=$(echo "$POST" | sed -n 's/.*"id":"\([^"]*\)".*/\1/p')
  if [[ -z "$MID" ]]; then
    echo "error: no message id (messaging not available on this process)" >&2
    exit 1
  fi
  echo
  echo "--- poll GET /messages/$MID ---"
  STATUS=""
  for _ in $(seq 1 30); do
    STATUS=$(curl -s "$API/messages/$MID")
    echo "$STATUS" | grep -q '"status":"delivered"' && break
    sleep 0.1
  done
  echo "$STATUS"
  if ! echo "$STATUS" | grep -q '"status":"delivered"'; then
    echo "error: message did not reach delivered" >&2
    exit 1
  fi
  echo
  echo "--- cloud inbox ---"
  curl -s "$API/devices/cloud/messages"
  echo
  echo
}

send_ue() {
  echo "--- POST sandbox-0 → sandbox-1 (UE) ---"
  local POST2 MID2 DELIVERED ST2
  POST2=$(curl -s -X POST "$API/devices/sandbox-0/messages" \
    -H 'Content-Type: application/json' \
    -d '{"to":"sandbox-1","body":"hello from phase-offset peer"}')
  echo "$POST2"
  MID2=$(echo "$POST2" | sed -n 's/.*"id":"\([^"]*\)".*/\1/p')
  if [[ -z "$MID2" ]]; then
    echo "error: no message id for UE→UE" >&2
    exit 1
  fi
  echo "Waiting up to ~5m for sandbox-1 window_opened → delivered..."
  echo "(watch ntnbox: message … sandbox-0 → sandbox-1  queued|…|delivered)"
  DELIVERED=0
  for _ in $(seq 1 3000); do
    ST2=$(curl -s "$API/messages/$MID2")
    if echo "$ST2" | grep -q '"status":"delivered"'; then
      echo "$ST2"
      DELIVERED=1
      break
    fi
    sleep 0.1
  done
  if [[ "$DELIVERED" -ne 1 ]]; then
    echo "status: $(curl -s "$API/messages/$MID2")"
    exit 1
  fi
  echo "--- sandbox-1 inbox ---"
  curl -s "$API/devices/sandbox-1/messages"
  echo
  echo
}

case "$TO" in
  network) send_cloud ;;
  ue) send_ue ;;
  both) send_cloud; send_ue ;;
esac

echo "OK — messaging API smoke passed."

if [[ "$WANT_GUI" -eq 1 ]]; then
  echo
  echo "GUI: pick Network or UE, Send, watch Activity + this terminal."
  echo "     $GUI_URL"
  echo "Ctrl+C to stop."
  if command -v open >/dev/null 2>&1; then
    open "$GUI_URL" || true
  elif command -v xdg-open >/dev/null 2>&1; then
    xdg-open "$GUI_URL" || true
  fi
  wait "$NTN_PID" || true
fi
