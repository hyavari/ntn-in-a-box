#!/usr/bin/env bash
# demo-android.sh — Print the Android emulator testing path for ntnbox.
#
# Usage:
#   ./scripts/demo-android.sh              # default profile: sos_burst
#   ./scripts/demo-android.sh leo_pass_90s
#   ./scripts/demo-android.sh --help
#
# Does not require the Android SDK. Does not start the emulator.
# On Linux/WSL2, prints a wrap-emulator command for full NTN shaping.
# On macOS/Windows, prints API + adb reverse guidance (full emulator
# shaping requires Linux, WSL2, or CI — not Docker wrapping a host AVD).

set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

usage() {
  cat <<'EOF'
Usage: ./scripts/demo-android.sh [profile]

Prints commands to test an Android emulator/app under ntnbox.

  profile   Profile name under testdata/profiles/ (default: sos_burst)

Examples:
  ./scripts/demo-android.sh
  ./scripts/demo-android.sh leo_pass_90s

See TUTORIAL.md Step 11 for the full walkthrough.
EOF
}

if [[ "${1:-}" == "--help" || "${1:-}" == "-h" ]]; then
  usage
  exit 0
fi

PROFILE="${1:-sos_burst}"
PROFILE_PATH="testdata/profiles/${PROFILE}.yaml"

if [[ ! -f "$PROFILE_PATH" ]]; then
  echo "error: profile not found: $PROFILE_PATH" >&2
  echo "available:" >&2
  ls testdata/profiles/*.yaml | sed 's|.*/||; s|\.yaml||' | sed 's/^/  /' >&2
  exit 1
fi

OS="$(uname -s)"
ADDR=":8080"
API="http://localhost:8080"
DEVICE="sandbox-0"

echo "=== NTN-in-a-Box — Android emulator path ==="
echo
echo "Profile:   $PROFILE  ($PROFILE_PATH)"
echo "API:       $API"
echo "Device:    $DEVICE"
echo "GUI:       $API/ui"
echo "Condition: $API/devices/$DEVICE/condition"
echo "Sample:    samples/android-connectivity/"
echo "Companion: android/ntnbox/"
echo
echo "adb reverse (emulator → host API):"
echo "  adb reverse tcp:8080 tcp:8080"
echo

case "$OS" in
  Linux)
    echo "Platform: Linux — full shaping available (netns/netem)."
    echo
    echo "1) Build ntnbox (once):"
    echo "     go build -o ntnbox ./cmd/ntnbox/"
    echo
    echo "2) Wrap the emulator (replace @MyAVD):"
    echo "     sudo ./ntnbox run --addr $ADDR --profile $PROFILE_PATH -- emulator @MyAVD"
    echo
    echo "3) Build/install the sample (Android Studio recommended):"
    echo "     open samples/android-connectivity/ in Android Studio → Run"
    echo "     # or: cd samples/android-connectivity && gradle wrapper --gradle-version 8.9 && ./gradlew :app:installDebug"
    echo
    echo "4) Reverse the API port into the emulator:"
    echo "     adb reverse tcp:8080 tcp:8080"
    ;;
  Darwin)
    echo "Platform: macOS — no native netns."
    echo
    echo "API + companion (no emulator traffic shaping on the host AVD):"
    echo "  go build -o ntnbox ./cmd/ntnbox/"
    echo "  # Long-lived Docker-backed run registers sandbox-0 + evaluator:"
    echo "  ./ntnbox run --addr $ADDR --profile $PROFILE_PATH -- sleep 3600"
    echo "  adb reverse tcp:8080 tcp:8080"
    echo
    echo "Do NOT use 'ntnbox serve' for /devices/$DEVICE/condition — serve does not"
    echo "auto-register sandbox-0 or an evaluator (condition would 404)."
    echo
    echo "Full delay/loss/coverage on an emulator: use Linux, WSL2, or CI and wrap"
    echo "the emulator process (Docker on Mac does not shape a host AVD):"
    echo "  sudo ./ntnbox run --addr $ADDR --profile $PROFILE_PATH -- emulator @MyAVD"
    echo
    echo "Walkthrough: TUTORIAL.md → Step 11"
    ;;
  MINGW*|MSYS*|CYGWIN*|Windows_NT)
    echo "Platform: Windows — no native netns. Prefer WSL2 for full emulator shaping."
    echo
    echo "Native Windows: API via WSL2/Linux host + adb reverse (same as Mac API path)."
    echo "WSL2 wrap template:"
    echo "  sudo ./ntnbox run --addr $ADDR --profile $PROFILE_PATH -- emulator @MyAVD"
    echo
    echo "Walkthrough: TUTORIAL.md → Step 11"
    ;;
  *)
    echo "Platform: $OS — use Linux/WSL2/CI for full emulator shaping."
    echo "Wrap template:"
    echo "  sudo ./ntnbox run --addr $ADDR --profile $PROFILE_PATH -- emulator @MyAVD"
    ;;
esac

echo
echo "Done. No emulator was started."
