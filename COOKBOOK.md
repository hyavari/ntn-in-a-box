# NTN adaptation cookbook

Short patterns for apps using ntnbox condition, coverage SSE, lookahead, and
store-and-forward messaging.
Full emulator walkthrough: [TUTORIAL.md Step 11](TUTORIAL.md#step-11-test-with-an-android-emulator).

## Setup (Mac API / companion)

```bash
# Prefer run --addr (shaping when Linux) or serve (API + sandbox-0, no netns):
./ntnbox serve --profile testdata/profiles/sos_burst.yaml
# default bind is 127.0.0.1:8080; use --addr 0.0.0.0:8080 for LAN
# or: ./ntnbox run --addr 127.0.0.1:8080 --profile … -- sleep 3600
# bare --addr :8080 is rewritten to 127.0.0.1; use 0.0.0.0:8080 for LAN

adb reverse tcp:8080 tcp:8080
curl -s http://127.0.0.1:8080/devices/sandbox-0/condition
curl -s http://127.0.0.1:8080/devices/sandbox-0/lookahead
```

Device id: **`sandbox-0`**. Do not use `serve --no-device` unless you `POST /devices` yourself.

Multi-device (phase offset — synthetic staggered windows):
`./ntnbox serve --profile … --devices 2 --phase-sec 240`

Multi-device (TLE dual-observer — real geography, e.g. SF + NYC):
```bash
./ntnbox serve --tle testdata/tle/iss.tle \
  --observer sandbox-0=37.7749,-122.4194 \
  --observer sandbox-1=40.7128,-74.0060
```
Do **not** combine `--observer` with `--devices` / `--phase-sec`.
GUI globe shows one pin (and beam) per observer. Same flags work on
`ntnbox run --tle … --addr …`.

Record/replay preserves `device_id` on coverage/link JSONL so multi-device
messaging flush stays correct on replay.

TUI (`run --tui` with messaging / `--addr`): Messages panel lists
id / from→to / status (no body); `J`/`K` scrolls.

## Patterns

### 1. Offline queue → flush on coverage

On SSE `window_opened` (or companion `CoverageKind.WINDOW_OPENED`), drain a local
queue. The Android sample does this.

### 2. Countdown UI from condition

Poll `/devices/{id}/condition` (or `onCondition`): if `in_coverage`, show
“next transition in Xs”; else “available in Xs” using `until_next_transition_sec`.

### 3. Burst only when the window is long enough

Use `/lookahead` (or `onLookahead`):

- If `next_window_duration_sec` < your min (e.g. 10s), skip starting a large upload.
- Prefer starting work when `until_next_transition_sec` (while in coverage) is still large,
  or when out of coverage and `next_window_duration_sec` is enough.

### 4. Burst when link improves

Subscribe to SSE `linkstate` (companion `onLinkState` if enabled): when
`delay_ms` / `loss_pct` drop and `bandwidth_kbps` rises, allow a sync burst.
Combine with (3) so you do not start a burst into a 2-second window.

### 5. Lead-time advisory

`GET /lookahead?lead_sec=60` only changes `effective_lookahead_sec` in the JSON.
It does **not** change when the server fires `window_opening` / `window_closing`
(those use the profile’s `lookahead_sec`).

### 6. Store-and-forward (UE→cloud)

One-shot demo script:

```bash
./scripts/demo-messaging.sh                 # Network (cloud) smoke
./scripts/demo-messaging.sh --to ue         # UE→UE smoke
./scripts/demo-messaging.sh --to both
./scripts/demo-messaging.sh --gui           # /ui: Network | UE + Activity log
```

GUI: choose **Network (cloud)** or **UE (sandbox-1)**. Activity log mirrors
`ntnbox: message …` lines on stderr. `--gui` starts with two devices.

Or manually:

```bash
./ntnbox serve --profile testdata/profiles/sos_burst.yaml

MID=$(curl -s -X POST http://127.0.0.1:8080/devices/sandbox-0/messages \
  -H 'Content-Type: application/json' \
  -d '{"to":"cloud","body":"SOS need help"}' | sed -n 's/.*"id":"\([^"]*\)".*/\1/p')
# → id is msg-<hex> (unguessable)

curl -s "http://127.0.0.1:8080/messages/$MID"
curl -s http://127.0.0.1:8080/devices/cloud/messages
```

No auth: keep the API on loopback (`serve` default `127.0.0.1:8080`).
Use `--addr 0.0.0.0:8080` only on trusted networks.

Submit anytime; `cloud` is always “in coverage” so delivery is immediate through
the mock IMS. UE→UE uses the same API with `to: "sandbox-1"` (needs
`--devices 2 --phase-sec …`); the kernel holds the message until the recipient
gets `window_opened`.

Companion: `sendMessage`, `fetchInbox`, `onMessage` / `messageFlow`.

### 7. Assert smoke (CI / local)

```bash
./scripts/assert-demo.sh
# or: make assert-demo
```

Starts `serve`, POSTs UE→cloud, polls until `delivered`, exits non-zero on
failure. Fast profile only (not TLE dual-observer).

## Android companion

See [`android/ntnbox/README.md`](android/ntnbox/README.md): `NtnBoxClient` polls
condition + lookahead and listens to coverage, link-state, and message SSE.
