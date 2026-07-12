#!/usr/bin/env bash
# demo-android.sh — Print the Mobile DX path for Android emulator testing.
#
# Usage:
#   ./scripts/demo-android.sh              # default profile: sos_burst
#   ./scripts/demo-android.sh leo_pass_90s
#   ./scripts/demo-android.sh --help
#
# Does not require the Android SDK. Does not start the emulator.
# On Linux/WSL2, prints a wrap-emulator command for full NTN shaping.
# On macOS/Windows, prints API + adb reverse guidance (full shaping via
# WSL2, Docker, or CI).

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

See TUTORIAL.md (Mobile DX / Android emulator step) for the full walkthrough.
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

echo "=== NTN-in-a-Box Mobile DX ==="
echo
echo "Profile:  $PROFILE  ($PROFILE_PATH)"
echo "API:      $API"
echo "Device:   $DEVICE"
echo "GUI:      $API/ui"
echo "Condition: $API/devices/$DEVICE/condition"
echo "Sample:   samples/android-connectivity/"
echo
echo "adb reverse (emulator can reach host API):"
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
    echo "3) In another terminal, build/install the sample:"
    echo "     cd samples/android-connectivity && ./gradlew :app:installDebug"
    echo
    echo "4) Optional — poll condition from the device:"
    echo "     adb reverse tcp:8080 tcp:8080"
    echo "     # then GET http://127.0.0.1:8080/devices/$DEVICE/condition inside the app/emulator"
    ;;
  Darwin)
    echo "Platform: macOS — no native netns. Full shaping: Linux CI, Docker, or a Linux/WSL2 host."
    echo
    echo "On this Mac you can still:"
    echo "  - Run ntnbox with --addr for API/GUI (Docker proxy for run, or ntnbox serve)"
    echo "  - adb reverse tcp:8080 tcp:8080 and poll /devices/$DEVICE/condition"
    echo
    echo "For real delay/loss/coverage gaps, wrap the emulator on Linux/WSL2:"
    echo "  sudo ./ntnbox run --addr $ADDR --profile $PROFILE_PATH -- emulator @MyAVD"
    echo
    echo "Walkthrough: TUTORIAL.md → Step 11 (Android emulator)"
    ;;
  MINGW*|MSYS*|CYGWIN*|Windows_NT)
    echo "Platform: Windows — no native netns. Prefer WSL2 for full shaping (same as Linux)."
    echo
    echo "Native Windows: API + adb reverse only."
    echo "WSL2 wrap template:"
    echo "  sudo ./ntnbox run --addr $ADDR --profile $PROFILE_PATH -- emulator @MyAVD"
    echo
    echo "Walkthrough: TUTORIAL.md → Step 11 (Android emulator)"
    ;;
  *)
    echo "Platform: $OS — treat like macOS/Windows: use Linux/WSL2/CI for full shaping."
    echo "Wrap template:"
    echo "  sudo ./ntnbox run --addr $ADDR --profile $PROFILE_PATH -- emulator @MyAVD"
    ;;
esac

echo
echo "Done. No emulator was started."
