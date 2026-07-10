#!/usr/bin/env bash
# demo-tle.sh — Demo the TLE support feature.
#
# Usage:
#   ./scripts/demo-tle.sh                          # generate ISS profile + run
#   ./scripts/demo-tle.sh --generate-only          # just generate profiles, don't run
#   ./scripts/demo-tle.sh --tui                    # run with TUI dashboard
#   ./scripts/demo-tle.sh --lat 51.5074 --lon -0.1278  # London observer
#   ./scripts/demo-tle.sh --tle testdata/tle/starlink-single.tle  # different satellite
#   ./scripts/demo-tle.sh --speed 10               # 10x gap acceleration
#
# This script:
# 1. Builds ntnbox
# 2. Generates profile(s) from TLE data (offline mode demo)
# 3. Runs ntnbox with --tle for live simulation (unless --generate-only)
#
# Ctrl+C to stop.

set -euo pipefail

# Defaults.
TLE_FILE="testdata/tle/iss.tle"
LAT="37.7749"
LON="-122.4194"
SPEED="1"
TUI_FLAG=""
GENERATE_ONLY=""
START_TIME="2024-04-09T12:00:00Z"  # Near TLE epoch for accuracy

while [[ "${1:-}" == --* ]]; do
  case "$1" in
    --tui)
      TUI_FLAG="--tui"
      shift
      ;;
    --generate-only)
      GENERATE_ONLY="1"
      shift
      ;;
    --tle)
      TLE_FILE="$2"
      shift 2
      ;;
    --lat)
      LAT="$2"
      shift 2
      ;;
    --lon)
      LON="$2"
      shift 2
      ;;
    --speed)
      SPEED="$2"
      shift 2
      ;;
    --start)
      START_TIME="$2"
      shift 2
      ;;
    *)
      echo "unknown flag: $1" >&2
      exit 1
      ;;
  esac
done

cleanup() {
  echo ""
  echo "cleaning up..."
  rm -f ntnbox poller
  rm -rf /tmp/ntnbox-tle-demo
  echo "done."
}
trap cleanup EXIT

echo "╔══════════════════════════════════════════════════════════╗"
echo "║           NTN-in-a-Box — TLE Support Demo              ║"
echo "╚══════════════════════════════════════════════════════════╝"
echo ""
echo "  TLE file:  $TLE_FILE"
echo "  Observer:  (${LAT}°, ${LON}°)"
echo "  Speed:     ${SPEED}x gap acceleration"
echo ""

# Build.
echo "==> building ntnbox + poller..."
go build -o ntnbox ./cmd/ntnbox/
go build -o poller ./cmd/poller/
echo ""

# ─── Part 1: Offline Profile Generation ───────────────────────────────────────

echo "━━━ Part 1: Offline Profile Generation (ntnbox tle generate) ━━━"
echo ""

mkdir -p /tmp/ntnbox-tle-demo

# Generate a single pass profile.
echo "==> generating single-pass profile..."
./ntnbox tle generate \
  --file "$TLE_FILE" \
  --lat "$LAT" --lon "$LON" \
  --start "$START_TIME" \
  --output /tmp/ntnbox-tle-demo/single-pass.yaml
echo ""

# Generate multiple passes.
echo "==> generating 3 pass profiles..."
./ntnbox tle generate \
  --file "$TLE_FILE" \
  --lat "$LAT" --lon "$LON" \
  --start "$START_TIME" \
  --passes 3 \
  --output /tmp/ntnbox-tle-demo/passes/
echo ""

# Show the generated profile.
echo "==> generated single-pass profile (first 30 lines):"
echo "────────────────────────────────────────────────────"
head -30 /tmp/ntnbox-tle-demo/single-pass.yaml
echo "..."
echo ""

# Show multi-pass files.
echo "==> generated multi-pass directory:"
ls -la /tmp/ntnbox-tle-demo/passes/
echo ""

if [[ -n "$GENERATE_ONLY" ]]; then
  echo "==> --generate-only: stopping here."
  echo "    profiles at: /tmp/ntnbox-tle-demo/"
  echo ""
  echo "    You can now run with a generated profile:"
  echo "    ./ntnbox run --profile /tmp/ntnbox-tle-demo/single-pass.yaml -- curl https://example.com"
  exit 0
fi

# ─── Part 2: Live TLE Simulation ─────────────────────────────────────────────

echo "━━━ Part 2: Live TLE Simulation (ntnbox run --tle) ━━━"
echo ""
echo "  This will predict passes and simulate satellite conditions in real-time."
echo "  The evaluator starts at the next predicted pass (--start-at next-pass)."
echo ""

# Build docker image for the live run (requires Linux namespaces).
echo "==> building docker image..."
docker build -t ntnbox:latest . -q

RUN_CMD=(
  ./ntnbox run
  --tle "$TLE_FILE"
  --lat "$LAT" --lon "$LON"
  --start-at next-pass
  --speed "$SPEED"
  --addr :8080
)

if [[ -n "$TUI_FLAG" ]]; then
  RUN_CMD+=("$TUI_FLAG")
fi

RUN_CMD+=(-- poller --url https://example.com --interval 2s)

echo "==> running: ${RUN_CMD[*]}"
echo "    GUI available at: http://localhost:8080/ui"
echo ""

"${RUN_CMD[@]}"
