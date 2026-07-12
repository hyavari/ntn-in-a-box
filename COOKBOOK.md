# NTN adaptation cookbook

Short patterns for apps using ntnbox condition, coverage SSE, and lookahead.
Full emulator walkthrough: [TUTORIAL.md Step 11](TUTORIAL.md#step-11-test-with-an-android-emulator).

## Setup (Mac API / companion)

```bash
# Prefer run --addr (shaping when Linux) or serve (API + sandbox-0, no netns):
./ntnbox serve --profile testdata/profiles/sos_burst.yaml
# or: ./ntnbox run --addr :8080 --profile … -- sleep 3600

adb reverse tcp:8080 tcp:8080
curl -s http://127.0.0.1:8080/devices/sandbox-0/condition
curl -s http://127.0.0.1:8080/devices/sandbox-0/lookahead
```

Device id: **`sandbox-0`**. Do not use `serve --no-device` unless you `POST /devices` yourself.

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

## Android companion

See [`android/ntnbox/README.md`](android/ntnbox/README.md): `NtnBoxClient` polls
condition + lookahead and listens to coverage (and link-state) SSE.
