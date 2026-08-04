#!/usr/bin/env bash
# demo-handover.sh — Cellular↔satellite dual-path demo.
#
# Runs geo_blockage_handover: while satellite is blocked, the sandbox
# switches the default route to a light terrestrial egress so poller
# traffic keeps succeeding. Watch selected_bearer / handover SSE / report.
#
# Usage:
#   ./scripts/demo-handover.sh
#   ./scripts/demo-handover.sh --tui           # status shows · SAT / via TERR
#   ./scripts/demo-handover.sh --report out.json
#   ./scripts/demo-handover.sh --help
# GUI: open the printed /ui URL — coverage line shows bearer + flash on switch.
#
# macOS: ntnbox auto-reinvokes inside Docker.
# Linux: run with sudo for netns shaping.
#
# Ctrl+C to stop; --report is written on session end.

set -euo pipefail

cd "$(dirname "$0")/.."

usage() {
  sed -n '2,16p' "$0" | sed 's/^# \{0,1\}//'
}

TUI_FLAG=""
REPORT_FILE=""

while [[ "${1:-}" == --* ]]; do
  case "$1" in
    --help|-h) usage; exit 0 ;;
    --tui) TUI_FLAG="--tui"; shift ;;
    --report)
      if [[ -z "${2:-}" || "${2:-}" == --* ]]; then
        echo "error: --report requires a filename" >&2
        exit 1
      fi
      REPORT_FILE="$2"
      shift 2
      ;;
    *) echo "error: unknown flag: $1" >&2; usage >&2; exit 1 ;;
  esac
done

PROFILE="geo_blockage_handover"

cleanup() {
  echo ""
  echo "cleaning up..."
  docker ps -q --filter "ancestor=ntnbox:latest" | while read -r cid; do docker stop "$cid" 2>/dev/null; done || true
  rm -f ntnbox poller
  if [[ "${PRUNE:-}" == "1" ]]; then
    echo "pruning docker image..."
    docker rmi ntnbox:latest 2>/dev/null || true
  fi
  echo "done."
}
trap cleanup EXIT

echo "==> building ntnbox + poller..."
go build -o ntnbox ./cmd/ntnbox/
go build -o poller ./cmd/poller/

echo "==> building docker image (macOS runs ntnbox inside Docker)..."
docker build -t ntnbox:latest . -q

RUN_ARGV=(run)
if [[ -n "$TUI_FLAG" ]]; then RUN_ARGV+=(--tui); fi
if [[ -n "$REPORT_FILE" ]]; then RUN_ARGV+=(--report "$REPORT_FILE"); fi
RUN_ARGV+=(--addr "127.0.0.1:8080" --profile "$PROFILE" --)
if [[ "$(uname -s)" == "Darwin" ]]; then
  POLLER_BIN=poller
else
  POLLER_BIN=./poller
fi
RUN_ARGV+=("$POLLER_BIN" --url http://example.com --interval 2s)

cat <<INFO

==> scenario: dual-path handover (satellite ↔ terrestrial)
    profile: $PROFILE
    expect: poller keeps getting 200s during satellite blockage
    API:    curl -s localhost:8080/devices/sandbox-0/condition | jq .selected_bearer
    SSE:    event: handover
INFO
if [[ -n "$REPORT_FILE" ]]; then
  echo "    report → $REPORT_FILE  (jq .handover $REPORT_FILE)"
fi
echo ""

./ntnbox "${RUN_ARGV[@]}"
