#!/usr/bin/env bash
# assert-demo.sh — thin wrapper around `ntnbox assert`.
#
# Exit 0 on success; non-zero on timeout / wrong status / failure.
#
# Usage:
#   ./scripts/assert-demo.sh
#   make assert-demo
#   ./ntnbox assert [--profile PATH] [--addr HOST:PORT] …

set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

if [[ ! -x ./ntnbox ]]; then
  go build -o ntnbox ./cmd/ntnbox/
fi

exec ./ntnbox assert "$@"
