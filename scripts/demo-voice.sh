#!/usr/bin/env bash
# demo-voice.sh — Voice-grade metrics demo: L-band GEO (or geo_blockage) +
# synthetic voicecall sessions + field-data --report.
#
# Usage:
#   ./scripts/demo-voice.sh                     # lband_geo + voicecall + report
#   ./scripts/demo-voice.sh --blockage          # geo_blockage (drops mid-call)
#   ./scripts/demo-voice.sh --report out.json   # custom report path
#   ./scripts/demo-voice.sh --tui
#   ./scripts/demo-voice.sh --help
#
# Requires --addr so voicecall can POST call-events and poll condition.
# ntnbox run dual-binds loopback + 10.200.0.1 (veth gateway) and sets
# NTNBOX_API_BASE for the child (TUI and non-TUI).
# macOS: ntnbox auto-reinvokes inside Docker (voicecall is in the image).
# Linux: run with sudo for netns shaping.
#
# Ctrl+C to stop; report is written on session end.

set -euo pipefail

cd "$(dirname "$0")/.."

usage() {
  sed -n '2,16p' "$0" | sed 's/^# \{0,1\}//'
}

TUI_FLAG=""
USE_BLOCKAGE=""
REPORT_FILE="out.json"

while [[ "${1:-}" == --* ]]; do
  case "$1" in
    --help|-h) usage; exit 0 ;;
    --tui) TUI_FLAG="--tui"; shift ;;
    --blockage) USE_BLOCKAGE="1"; shift ;;
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

if [[ -n "$USE_BLOCKAGE" ]]; then
  PROFILE="geo_blockage"
else
  PROFILE="lband_geo"
fi

cleanup() {
  echo ""
  echo "cleaning up..."
  docker ps -q --filter "ancestor=ntnbox:latest" | while read -r cid; do docker stop "$cid" 2>/dev/null; done || true
  rm -f ntnbox voicecall
  if [[ "${PRUNE:-}" == "1" ]]; then
    echo "pruning docker image..."
    docker rmi ntnbox:latest 2>/dev/null || true
  fi
  echo "done."
}
trap cleanup EXIT

echo "==> building ntnbox + voicecall..."
go build -o ntnbox ./cmd/ntnbox/
go build -o voicecall ./cmd/voicecall/

echo "==> building docker image (macOS runs ntnbox inside Docker)..."
docker build -t ntnbox:latest . -q

# Short talk/gap so the demo produces several call events quickly.
TALK="${TALK:-8s}"
GAP="${GAP:-3s}"

RUN_ARGV=(run)
if [[ -n "$TUI_FLAG" ]]; then RUN_ARGV+=(--tui); fi
RUN_ARGV+=(--report "$REPORT_FILE")
# Loopback CLI is fine: ntnbox run dual-binds 10.200.0.1 for the sandbox;
# Darwin Docker still publishes localhost.
RUN_ARGV+=(--addr "127.0.0.1:8080" --profile "$PROFILE" --)
# Darwin Docker: bare name uses the image binary (host ./voicecall is Darwin).
# Linux netns: PATH has no cwd, so use ./voicecall from the project build.
if [[ "$(uname -s)" == "Darwin" ]]; then
  VOICECALL_BIN=voicecall
else
  VOICECALL_BIN=./voicecall
fi
RUN_ARGV+=("$VOICECALL_BIN" --talk "$TALK" --gap "$GAP")

cat <<INFO

==> scenario: voice-grade estimates + synthetic call sessions
    profile: $PROFILE
    report → $REPORT_FILE (written when you stop)
    watch for: started / completed / dropped rows from voicecall
    inspect:   jq .voice $REPORT_FILE
    GUI:       http://localhost:8080/ui

INFO

./ntnbox "${RUN_ARGV[@]}"
