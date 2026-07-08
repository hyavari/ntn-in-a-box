#!/usr/bin/env bash
# demo.sh — Build, run a demo, and clean up.
#
# Usage:
#   ./scripts/demo.sh                          # default: LEO pass + poller
#   ./scripts/demo.sh --tui                    # with TUI dashboard
#   ./scripts/demo.sh geo_steady               # use a different profile
#   ./scripts/demo.sh --tui leo_pass_90s       # TUI + specific profile
#   ./scripts/demo.sh leo_pass_90s curl http://example.com   # custom command
#
# Options:
#   PRUNE=1 ./scripts/demo.sh                  # also remove docker image on exit
#
# Ctrl+C to stop. Binaries are cleaned up on exit.

set -euo pipefail

# Check for --tui flag.
TUI_FLAG=""
if [[ "${1:-}" == "--tui" ]]; then
  TUI_FLAG="--tui"
  shift
fi

PROFILE="${1:-leo_pass_90s}"
shift 2>/dev/null || true

PROFILE_PATH="testdata/profiles/${PROFILE}.yaml"
if [[ ! -f "$PROFILE_PATH" ]]; then
  echo "error: profile not found: $PROFILE_PATH" >&2
  echo "available profiles:" >&2
  ls testdata/profiles/*.yaml | sed 's|.*/||; s|\.yaml||' | sed 's/^/  /' >&2
  exit 1
fi

# Default command: poller against example.com
CMD=("${@:-poller --url http://example.com --interval 2s}")
if [[ $# -eq 0 ]]; then
  CMD=(poller --url http://example.com --interval 2s)
fi

cleanup() {
  echo ""
  echo "cleaning up..."
  # Stop any running ntnbox containers.
  docker ps -q --filter "ancestor=ntnbox:latest" | xargs -r docker stop 2>/dev/null || true
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

echo "==> building docker image..."
docker build -t ntnbox:latest . -q

echo "==> running: ntnbox run ${TUI_FLAG} --addr :8080 --profile $PROFILE_PATH -- ${CMD[*]}"
echo ""
echo "    GUI available at: http://localhost:8080/ui"
echo ""

./ntnbox run ${TUI_FLAG} --addr :8080 --profile "$PROFILE_PATH" -- "${CMD[@]}"
