# Tutorial: Building NTN-Aware Applications

This tutorial walks you through testing applications under simulated
satellite conditions using NTN-in-a-Box. You'll see how real apps
experience coverage gaps, latency spikes, and bandwidth constraints —
and learn the patterns that make apps resilient to these conditions.

## Prerequisites

- Docker Desktop (macOS) or Linux with root access
- Go 1.26+ (to build ntnbox)
- Node.js, Python 3, or Go for the sample apps

## Step 1: Run the simplest demo

The fastest way to see NTN conditions in action is the curl demo:

```bash
# Build and run via the demo script:
./scripts/demo.sh --sample curl-demo

# Or manually:
go build -o ntnbox ./cmd/ntnbox
sudo ./ntnbox run --profile testdata/profiles/leo_pass_90s.yaml -- ./samples/curl-demo.sh
```

You'll see output like:

```
  TIME        STATUS  LATENCY     RESULT
  ─────────────────────────────────────────────────
  10:00:01    200     152ms       ok          ← satellite rising (high delay)
  10:00:03    200     43ms        ok          ← overhead (low delay)
  10:00:05    200     41ms        ok
  ...
  10:01:30    ---     —           timeout     ← coverage lost
  10:01:32    ---     —           timeout
  ...
  10:10:00    200     148ms       ok          ← next pass begins
```

## Step 2: Understand the profiles

NTN-in-a-Box uses YAML profiles to define satellite pass characteristics:

```yaml
# testdata/profiles/leo_pass_90s.yaml
name: leo_pass_90s
schedule:
  mode: periodic # satellite passes repeatedly
  period_sec: 600 # one pass every 10 minutes
  window_sec: 90 # each pass lasts 90 seconds
  lookahead_sec: 30 # 30s advance warning before transitions

curves:
  delay_ms: # latency varies during the pass
    - { offset_sec: 0, value: 150 } # horizon: high delay
    - { offset_sec: 15, value: 40 } # overhead: low delay
    - { offset_sec: 75, value: 40 }
    - { offset_sec: 90, value: 100 } # setting: delay rises
  # jitter_ms, loss_pct, bandwidth_kbps follow similar curves
```

Available profiles:

- `leo_pass_90s` — single-satellite LEO pass (Iridium/Swarm-style visibility windows)
- `geo_steady` — continuous GEO link (always connected, variable quality)
- `d2c_burst` — short burst windows (Direct-to-Cell store-and-forward style)
- `sos_burst` — emergency/SOS short burst (15s window, tiny bandwidth, elevated loss)
- `sos_hostile` — harsher SOS variant for stress-testing offline queues

## Step 3: Test a Node.js app with retry logic

The Node.js sample demonstrates exponential backoff and offline queuing:

```bash
ntnbox run --profile testdata/profiles/leo_pass_90s.yaml \
  -- node samples/node-retry/index.js
```

Watch the output:

- Messages send successfully during coverage
- On coverage loss: retries with exponential backoff, then queues
- On coverage return: queue flushes automatically

**Key pattern:** The app doesn't need to know about satellites. It
just handles network failures gracefully — which is exactly what
NTN-aware apps must do.

## Step 4: Test a Python app with adaptive behavior

The Python sample adapts its behavior based on observed conditions:

```bash
ntnbox run --profile testdata/profiles/leo_pass_90s.yaml \
  -- python3 samples/python-adaptive/client.py
```

This demo shows:

- Link state detection (online → degraded → offline)
- Reduced polling frequency when degraded
- Store-and-forward during outages

**Key pattern:** Detect constraints and adapt. Don't hammer a degraded
link — reduce load and batch operations.

## Step 5: Test a Go messenger with queue flush

This demonstrates a messaging client with offline queuing. By default
it checks connectivity against `https://example.com` — no server
setup needed:

```bash
./scripts/demo.sh --sample go-messenger
```

Messages succeed during coverage and timeout during gaps. The client
queues locally and flushes when the next pass begins.

To demo the full client/server flow (Linux only — env vars are not
forwarded through the macOS Docker proxy):

```bash
# Terminal 1: start the server
go run samples/go-messenger/server/main.go
# Terminal 2: run client targeting the server (Linux native)
sudo SERVER_URL=http://10.200.0.1:9090/send \
  ./ntnbox run --profile testdata/profiles/leo_pass_90s.yaml -- ./messenger-client
```

**Key pattern:** Queue locally, flush on reconnect. This is the
store-and-forward pattern that real satellite messaging uses.

## Step 6: Use the TUI dashboard

Add `--tui` to see a live dashboard while your app runs:

```bash
ntnbox run --tui --profile testdata/profiles/leo_pass_90s.yaml \
  -- node samples/node-retry/index.js
```

The dashboard shows:

- Coverage status and countdown
- Link metrics (delay, jitter, loss, bandwidth) with sparklines
- Your app's output in a scrollable pane

## Step 7: Use the GUI visualization

Add `--addr :8080` to enable the web GUI:

```bash
ntnbox run --addr :8080 --profile testdata/profiles/leo_pass_90s.yaml \
  -- python3 samples/python-adaptive/client.py
```

Open `http://localhost:8080/ui` in your browser to see:

- Animated satellite moving along its orbit
- Coverage beam connecting to the ground device
- Real-time metrics and coverage timeline

## Step 8: Test your own app

Any application that makes network requests can be tested:

```bash
ntnbox run --profile testdata/profiles/leo_pass_90s.yaml -- ./your-app
```

No code changes needed — ntnbox shapes the network transparently
at the OS level. Your app sees real delays, real packet loss, and
real connectivity gaps.

## Step 9: Record and replay sessions

You can record a session to a JSONL file and replay it later — useful
for reproducing bugs or running the same conditions in CI:

```bash
# Record a session to a file:
./scripts/demo.sh --record session.jsonl

# Replay it later (no profile needed):
./scripts/demo.sh --replay session.jsonl

# Replay with TUI dashboard:
./scripts/demo.sh --tui --replay session.jsonl

# Replay at 10x speed (great for CI):
./ntnbox replay --file session.jsonl --speed 10 -- ./my-app
```

The recording captures every coverage transition and link-state update
as timestamped JSONL events. Replay reproduces them with exact timing.
When replay completes, both the TUI and GUI show a clear "replay
complete" status with a progress bar showing elapsed/total duration.
In TUI mode, you can press `r` to immediately replay again or `q` to
quit. The file is human-readable and can be stored alongside your test
suite for regression testing (add an exception to `.gitignore` or use a
dedicated directory like `testdata/sessions/`).

**Example session.jsonl:**
```jsonl
{"type":"coverage","kind":"window_opened","at":"2026-07-08T10:00:00Z","in_coverage":true,"elapsed_sec":0,"until_next_transition":90}
{"type":"linkstate","delay_ms":150,"jitter_ms":40,"loss_pct":10,"bandwidth_kbps":2000,"at":"2026-07-08T10:00:00.25Z"}
{"type":"linkstate","delay_ms":43,"jitter_ms":5,"loss_pct":0.2,"bandwidth_kbps":20000,"at":"2026-07-08T10:00:00.5Z"}
{"type":"coverage","kind":"window_closed","at":"2026-07-08T10:01:30Z","in_coverage":false,"elapsed_sec":0,"until_next_transition":510}
```

## Step 10: Query the API

While running with `--addr`, you can query satellite state
programmatically:

```bash
# Current condition (coverage, delay, jitter, loss, bandwidth):
curl http://localhost:8080/devices/sandbox-0/condition

# Pass lookahead (absolute open/close + duration):
curl http://localhost:8080/devices/sandbox-0/lookahead

# Satellite capabilities:
curl http://localhost:8080/devices/sandbox-0/capabilities

# Live event stream (Server-Sent Events):
curl -N http://localhost:8080/events
```

This lets your app adapt in real time based on satellite state.

**Note:** In replay mode, the GUI and SSE stream (`/events`) work
normally, but `/devices/{id}/condition` and `/capabilities` are not
available (no evaluator or profile loaded).

## Step 11: Test with an Android emulator

Goal: run an Android app against ntnbox in two layers:

1. **Traffic** (Linux / WSL2 / CI) — wrap the emulator so HTTP feels delay,
   loss, and coverage gaps.
2. **Signals** (every platform) — the app talks to the condition API /
   coverage SSE via `adb reverse`, so it can show countdown and adapt
   (retry, offline queue, flush on `window_opened`).

Cheat-sheet for your machine anytime:

```bash
./scripts/demo-android.sh              # default: sos_burst
./scripts/demo-android.sh leo_pass_90s
```

### Platform matrix

| Platform | Full emulator shaping (delay/loss/gaps) | API + GUI + companion |
|----------|-------------------------------------------|------------------------|
| Linux / WSL2 / CI | Yes — wrap `emulator` under `ntnbox run` | Yes |
| macOS | No on a host AVD — use Linux/WSL2/CI for shaping | Yes — Docker-backed `ntnbox run --addr … -- sleep …` |
| Native Windows | Prefer WSL2 wrap | Yes (API host via WSL2/Linux) |

Device id with `run --addr`: **`sandbox-0`**. Do **not** use `ntnbox serve`
for `/devices/sandbox-0/condition` — serve does not register that device or
an evaluator (you'd get 404).

### 1) Start ntnbox

**Linux / WSL2 — shape emulator traffic + expose API:**

```bash
go build -o ntnbox ./cmd/ntnbox/

# Replace @MyAVD with your AVD name (`emulator -list-avds`)
sudo ./ntnbox run --addr :8080 \
  --profile testdata/profiles/sos_burst.yaml \
  -- emulator @MyAVD
```

**macOS (or API-only anywhere) — signals without shaping the host AVD:**

```bash
go build -o ntnbox ./cmd/ntnbox/

# Docker-backed on Mac; keeps sandbox-0 + evaluator alive
./ntnbox run --addr :8080 \
  --profile testdata/profiles/sos_burst.yaml \
  -- sleep 3600
```

Open `http://localhost:8080/ui` for the live visualization. Confirm the API:

```bash
curl -s http://localhost:8080/devices/sandbox-0/condition
curl -s http://localhost:8080/devices/sandbox-0/lookahead
```

### 2) Build and install the sample

JDK 17+ and Android SDK required. Android Studio → open
`samples/android-connectivity/` → Run, **or** CLI:

```bash
cd samples/android-connectivity
# If ./gradlew is missing: gradle wrapper --gradle-version 8.9
./gradlew :app:installDebug
```

The sample depends on the companion via Gradle `includeBuild("../../android")`.

### 3) Bridge the emulator to the host API

```bash
adb reverse tcp:8080 tcp:8080
```

The app defaults to `http://127.0.0.1:8080` and device `sandbox-0`.

### 4) What you should see

In [`samples/android-connectivity/`](samples/android-connectivity/):

- HTTP every ~2s with retry + offline queue
- Companion line: “available in Xs” / “next transition in Xs”
- On coverage `window_opened`, the queue drains
- Under a Linux wrap, gaps also *fail* HTTP for real; on Mac API-only, shaping
  of the AVD path itself is skipped — countdown/SSE still work

### Use the companion in your own app

Library: [`android/ntnbox/`](android/ntnbox/) (details and Flow helpers in its
README). Minimal wiring after `adb reverse`:

```kotlin
val client = NtnBoxClient() // http://127.0.0.1:8080 , sandbox-0
client.addListener(mainExecutor, object : NtnBoxListener {
    override fun onCoverageChanged(inCoverage: Boolean, kind: CoverageKind) { /* … */ }
    override fun onCondition(condition: NtnCondition) { /* countdown / metrics */ }
    override fun onConnectionChanged(connected: Boolean) { /* SSE up/down */ }
})
client.start()
// …
client.stop()
```

Enable cleartext HTTP to localhost (`android:usesCleartextTraffic="true"` or a
network security config) — the sample already does.

## Patterns for NTN-aware apps

| Pattern                                  | When to use                                              |
| ---------------------------------------- | -------------------------------------------------------- |
| Exponential backoff                      | Always — don't hammer a failing link                     |
| Offline queue                            | Messaging, sync, uploads — anything that can be deferred |
| Adaptive timeout                         | Increase timeouts when latency is high                   |
| Reduce payload size on constrained links |
| Burst transfer                           | Transfer data in bursts during coverage, sleep between   |
| Graceful degradation                     | Reduce functionality when link quality drops             |

## Next steps

- Try different profiles (`geo_steady`, `d2c_burst`, `sos_burst`) to see how
  your app handles different satellite architectures
- Follow [Step 11](#step-11-test-with-an-android-emulator) for Android emulator testing
- Build a custom profile that matches your target constellation
- Use the `--addr` API to build satellite-aware features in your app
- Check the [API reference](README.md#api-reference) for all endpoints
