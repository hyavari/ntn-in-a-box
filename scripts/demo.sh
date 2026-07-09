#!/usr/bin/env bash
# demo.sh — Build, run a demo, and clean up.
#
# Usage:
#   ./scripts/demo.sh                              # default: LEO pass + poller
#   ./scripts/demo.sh --tui                        # with TUI dashboard
#   ./scripts/demo.sh --sample curl-demo           # run curl demo sample
#   ./scripts/demo.sh --sample go-messenger        # run Go messenger client
#   ./scripts/demo.sh --record session.jsonl       # record bus events
#   ./scripts/demo.sh --replay session.jsonl       # replay a recorded session
#   ./scripts/demo.sh geo_steady                   # use a different profile
#   ./scripts/demo.sh --tui --sample go-messenger  # TUI + sample
#   ./scripts/demo.sh leo_pass_90s curl https://example.com  # custom command
#
# Samples available via --sample:
#   curl-demo       — curl in a loop (POSIX sh + curl, works in Docker)
#   go-messenger    — Go messenger client (cross-compiled, bind-mounted)
#   node-retry      — Node.js retry client (requires node, Linux only)
#   python-adaptive — Python adaptive client (requires python3, Linux only)
#
# Note: On macOS, samples run inside Docker via bind-mount. The Docker
#       image contains only ntnbox + poller + curl. Go samples are
#       cross-compiled for Linux by this script. Node.js/Python require
#       native Linux execution.
#
# Options:
#   PRUNE=1 ./scripts/demo.sh                  # also remove docker image on exit
#
# Ctrl+C to stop. Binaries are cleaned up on exit.

set -euo pipefail

# Parse flags.
TUI_FLAG=""
SAMPLE=""
RECORD_FILE=""
REPLAY_FILE=""

while [[ "${1:-}" == --* ]]; do
  case "$1" in
    --tui)
      TUI_FLAG="--tui"
      shift
      ;;
    --sample)
      if [[ -z "${2:-}" || "${2:-}" == --* ]]; then
        echo "error: --sample requires a value" >&2
        echo "available samples: curl-demo, go-messenger, node-retry, python-adaptive" >&2
        exit 1
      fi
      SAMPLE="$2"
      shift 2
      ;;
    --record)
      if [[ -z "${2:-}" || "${2:-}" == --* ]]; then
        echo "error: --record requires a filename" >&2
        exit 1
      fi
      RECORD_FILE="$2"
      shift 2
      ;;
    --replay)
      if [[ -z "${2:-}" || "${2:-}" == --* ]]; then
        echo "error: --replay requires a filename" >&2
        exit 1
      fi
      REPLAY_FILE="$2"
      shift 2
      ;;
    *)
      break
      ;;
  esac
done

PROFILE="${1:-leo_pass_90s}"
shift 2>/dev/null || true

# Cannot use --record and --replay together.
if [[ -n "$RECORD_FILE" && -n "$REPLAY_FILE" ]]; then
  echo "error: --record and --replay cannot be used together" >&2
  exit 1
fi

# Replay mode short-circuits before profile check.
if [[ -n "$REPLAY_FILE" ]]; then
  if [[ ! -f "$REPLAY_FILE" ]]; then
    echo "error: recording file not found: $REPLAY_FILE" >&2
    exit 1
  fi
  CMD=("${@:-poller --url https://example.com --interval 2s}")
  if [[ $# -eq 0 ]]; then
    CMD=(poller --url https://example.com --interval 2s)
  fi

  cleanup() {
    echo ""
    echo "cleaning up..."
    docker ps -q --filter "ancestor=ntnbox:latest" | while read -r cid; do docker stop "$cid" 2>/dev/null; done || true
    rm -f ntnbox poller messenger-client
    if [[ "${PRUNE:-}" == "1" ]]; then docker rmi ntnbox:latest 2>/dev/null || true; fi
    echo "done."
  }
  trap cleanup EXIT

  echo "==> building ntnbox + poller..."
  go build -o ntnbox ./cmd/ntnbox/
  go build -o poller ./cmd/poller/

  echo "==> building docker image..."
  docker build -t ntnbox:latest . -q

  echo "==> replaying: ntnbox replay ${TUI_FLAG} --file $REPLAY_FILE -- ${CMD[*]}"
  echo "    GUI available at: http://localhost:8080/ui"
  echo ""
  ./ntnbox replay ${TUI_FLAG} --file "$REPLAY_FILE" --addr :8080 -- "${CMD[@]}"
  exit $?
fi

PROFILE_PATH="testdata/profiles/${PROFILE}.yaml"
if [[ ! -f "$PROFILE_PATH" ]]; then
  echo "error: profile not found: $PROFILE_PATH" >&2
  echo "available profiles:" >&2
  ls testdata/profiles/*.yaml | sed 's|.*/||; s|\.yaml||' | sed 's/^/  /' >&2
  exit 1
fi

# Determine the command to run.
if [[ -n "$SAMPLE" ]]; then
  case "$SAMPLE" in
    curl-demo)
      CMD=(./samples/curl-demo.sh)
      ;;
    go-messenger)
      CMD=(./messenger-client)
      ;;
    node-retry)
      CMD=(node samples/node-retry/index.js)
      ;;
    python-adaptive)
      CMD=(python3 samples/python-adaptive/client.py)
      ;;
    *)
      echo "error: unknown sample: $SAMPLE" >&2
      echo "available samples: curl-demo, go-messenger, node-retry, python-adaptive" >&2
      exit 1
      ;;
  esac
elif [[ $# -gt 0 ]]; then
  CMD=("$@")
else
  CMD=(poller --url https://example.com --interval 2s)
fi

cleanup() {
  echo ""
  echo "cleaning up..."
  # Stop any running ntnbox containers.
  docker ps -q --filter "ancestor=ntnbox:latest" | while read -r cid; do docker stop "$cid" 2>/dev/null; done || true
  rm -f ntnbox poller messenger-client
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

# Build Go samples if requested (cross-compile for Linux since it runs in Docker).
if [[ "$SAMPLE" == "go-messenger" ]]; then
  echo "==> building go-messenger client (linux)..."
  DOCKER_ARCH=$(docker info --format '{{.Architecture}}' 2>/dev/null || echo "amd64")
  case "$DOCKER_ARCH" in
    aarch64|arm64) GOARCH=arm64 ;;
    *)             GOARCH=amd64 ;;
  esac
  GOOS=linux GOARCH=$GOARCH CGO_ENABLED=0 go build -o messenger-client ./samples/go-messenger/client/
fi

echo "==> building docker image..."
docker build -t ntnbox:latest . -q

RECORD_ARGS=()
if [[ -n "$RECORD_FILE" ]]; then
  RECORD_ARGS=(--record "$RECORD_FILE")
fi

echo "==> running: ntnbox run ${TUI_FLAG} ${RECORD_ARGS[*]} --addr :8080 --profile $PROFILE_PATH -- ${CMD[*]}"
echo "    GUI available at: http://localhost:8080/ui"
if [[ -n "$RECORD_FILE" ]]; then
  echo "    Recording to: $RECORD_FILE"
fi
echo ""

./ntnbox run ${TUI_FLAG} "${RECORD_ARGS[@]}" --addr :8080 --profile "$PROFILE_PATH" -- "${CMD[@]}"
