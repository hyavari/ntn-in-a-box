#!/usr/bin/env bash
# demo.sh — Build, run a demo, and clean up.
#
# Usage:
#   ./scripts/demo.sh                              # default: LEO pass + poller
#   ./scripts/demo.sh --tui                        # with TUI dashboard
#   ./scripts/demo.sh --sample curl-demo           # run curl demo sample
#   ./scripts/demo.sh --sample go-messenger        # run Go messenger client
#   ./scripts/demo.sh --record session.jsonl       # record bus events
#   ./scripts/demo.sh --report out.json            # field-data JSON at session end
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
# For messaging-only (no Docker): ./scripts/demo-messaging.sh

set -euo pipefail

usage() {
  sed -n '2,32p' "$0" | sed 's/^# \{0,1\}//'
}

# Parse flags.
TUI_FLAG=""
SAMPLE=""
RECORD_FILE=""
REPORT_FILE=""
REPLAY_FILE=""

while [[ "${1:-}" == --* ]]; do
  case "$1" in
    --help|-h)
      usage
      exit 0
      ;;
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
    --report)
      if [[ -z "${2:-}" || "${2:-}" == --* ]]; then
        echo "error: --report requires a filename" >&2
        exit 1
      fi
      REPORT_FILE="$2"
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
      echo "error: unknown flag: $1" >&2
      usage >&2
      exit 1
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
if [[ -n "$REPORT_FILE" && -n "$REPLAY_FILE" ]]; then
  echo "error: --report is only supported with run (not --replay)" >&2
  exit 1
fi

# macOS bash 3.2 + set -u rejects "${empty_array[@]}" — build argv explicitly.
build_run_argv() {
  # Sets global RUN_ARGV as a bash array for: ntnbox <mode> ... -- <cmd>
  local mode="$1"
  shift
  RUN_ARGV=("$mode")
  if [[ -n "$TUI_FLAG" ]]; then
    RUN_ARGV+=(--tui)
  fi
  if [[ -n "$RECORD_FILE" ]]; then
    RUN_ARGV+=(--record "$RECORD_FILE")
  fi
  if [[ -n "$REPORT_FILE" ]]; then
    RUN_ARGV+=(--report "$REPORT_FILE")
  fi
  if [[ -n "$REPLAY_FILE" ]]; then
    RUN_ARGV+=(--file "$REPLAY_FILE")
  fi
  RUN_ARGV+=(--addr "127.0.0.1:8080")
  if [[ "$mode" == "run" ]]; then
    RUN_ARGV+=(--profile "$PROFILE_PATH")
  fi
  RUN_ARGV+=(--)
  RUN_ARGV+=("$@")
}

# Replay mode short-circuits before profile check.
if [[ -n "$REPLAY_FILE" ]]; then
  if [[ ! -f "$REPLAY_FILE" ]]; then
    echo "error: recording file not found: $REPLAY_FILE" >&2
    exit 1
  fi
  if [[ $# -eq 0 ]]; then
    CMD=(poller --url https://example.com --interval 2s)
  else
    CMD=("$@")
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

  build_run_argv replay "${CMD[@]}"
  echo "==> replaying: ntnbox ${RUN_ARGV[*]}"
  echo "    GUI available at: http://localhost:8080/ui"
  echo ""
  ./ntnbox "${RUN_ARGV[@]}"
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

build_run_argv run "${CMD[@]}"
echo "==> running: ntnbox ${RUN_ARGV[*]}"
echo "    GUI available at: http://localhost:8080/ui"
if [[ -n "$RECORD_FILE" ]]; then
  echo "    Recording to: $RECORD_FILE"
fi
echo ""

./ntnbox "${RUN_ARGV[@]}"
