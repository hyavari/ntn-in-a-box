# Step 1 — Dev Sandbox Module: Design

Date: 2026-07-07
Status: Approved
Ref: `docs/superpowers/specs/2026-07-03-ntn-in-a-box-design.md`

## Goal

Ship the first capability module: Dev Sandbox. After this step, a
developer can run any command under simulated NTN conditions with a
single CLI invocation:

```bash
ntnbox run --profile testdata/profiles/leo_pass_90s.yaml -- ./my-app
```

Their app experiences real, curve-shaped network degradation — delay,
jitter, loss, bandwidth throttling — driven by the Condition Engine in
real time, including total connectivity loss when coverage windows close.

## Deliverables

1. **`ntnbox run` CLI command** — wraps a process in a shaped network
   namespace, starts the kernel in-process, streams condition changes
   to stderr, tears down cleanly on Ctrl+C.

2. **Driver loop** — the missing kernel piece: a goroutine that ticks
   every 250ms, evaluates the Condition Engine, and publishes to the
   event bus. Turns the pull-based Evaluator into a push-based stream.

3. **Dev Sandbox module** (`internal/module/devsandbox/`) — the first
   real implementation of the 5-hook module contract.

4. **Netem shim** (`internal/module/devsandbox/netem/`) — translates
   `LinkState` into `tc qdisc change` commands via `os/exec`.

5. **Namespace wrapper** (`internal/module/devsandbox/netns/`) —
   creates Linux network namespace + veth pair, routes traffic through
   the shaped interface, launches the user's command inside it.

6. **macOS Docker proxy** — transparent re-invocation inside a
   container when `runtime.GOOS == "darwin"`.

7. **Reference poller** (`cmd/poller/`) — tiny HTTP client that polls
   an endpoint (default: kernel's `/echo`) and prints per-request
   latency/status, so a new user sees degradation within seconds.

8. **`/echo` endpoint** — added to apihost, returns `{"ts":"..."}` for
   the self-contained demo.

9. **Dockerfile** — Alpine + iproute2 + ntnbox binary, used by the
   macOS proxy and available for manual container usage.

## Architecture

### Components and their responsibilities

| Component | Responsibility |
|---|---|
| Driver loop | Ticks 250ms, calls `Evaluate(now)`, publishes coverage events on transitions, link-state continuously |
| Dev Sandbox module | Implements `pkg/module.Module`; receives events, drives netem shim |
| Netem shim | Translates `{delay, jitter, loss, bandwidth}` → `tc qdisc change` inside a netns |
| Namespace wrapper | Creates netns + veth pair, sets up routing, launches wrapped process |
| macOS Docker proxy | Detects darwin, re-invokes `ntnbox run` inside a Docker container |
| Reference poller | Prints latency/status per request to demonstrate degradation |

### Module contract integration

```text
Dev Sandbox Module implements pkg/module.Module:
  RegisterRoutes(host)     → GET /sandbox/status (current shaping values)
  OnCoverageEvent(event)   → coverage closed: set 100% loss
                             coverage opened: resume curve-driven shaping
  OnLinkState(state)       → tc qdisc change netem delay/jitter/loss/rate
  DeliverVia(adapter)      → no-op (Dev Sandbox doesn't deliver messages)
  Emit(emitter)            → push shaping-change events to observability
```

### Data flow

```text
time.Tick(250ms)
  │
  │  Evaluator.Evaluate(now)
  ▼
CoverageState + LinkState
  │
  │  Bus.PublishCoverageEvent() on transitions
  │  Bus.PublishLinkState() continuously (throttled by bus)
  ▼
Dev Sandbox Module
  │
  │  OnCoverageEvent(closed) → netem.SetFullLoss()
  │  OnCoverageEvent(opened) → netem.ApplyState(initial curve values)
  │  OnLinkState(state)      → netem.ApplyState(state)
  ▼
netem shim
  │
  │  tc qdisc change dev <veth-inner> root netem \
  │    delay <delay>ms <jitter>ms loss <loss>% rate <bw>kbit
  ▼
User's process (inside netns, traffic shaped transparently)
```

### CLI surface

```text
ntnbox run --profile <path> [--addr <host:port>] -- <cmd> [args...]
```

Behavior:
- Starts the kernel (profile + device registry + API host) in-process
  (same process, not a separate `ntnbox serve`)
- Creates a virtual UE device associated with the profile
- Starts the driver loop (250ms tick → evaluator → bus)
- Creates a network namespace + veth pair
- Applies initial `tc qdisc` based on the profile's first curve values
- Subscribes the Dev Sandbox module to the bus
- Launches `<cmd>` inside the namespace
- Streams condition updates to stderr (one line per transition/significant change)
- On Ctrl+C (SIGINT) or process exit: tears down namespace, stops kernel

The `--addr` flag optionally exposes the API host (default: off — the
API is not exposed unless the user asks for it, since `ntnbox run` is
primarily about the shaped command, not API queries) so the user can
query `/devices/{id}/condition` or `/sandbox/status` while the command
runs.

### Out-of-coverage behavior

When the coverage window closes (`CoverageEvent` kind `window_closed`):
- Set `tc` loss to 100% — packets silently drop
- This mimics real satellite behavior (signal disappears, no ICMP
  unreachable or RST — just silence)
- Apps must detect the outage via timeouts, which is exactly what
  NTN-aware apps need to handle

When coverage reopens (`CoverageEvent` kind `window_opened`):
- Resume curve-driven shaping at the first point's values (which are
  typically degraded — e.g. 150ms delay at window start for LEO)
- Subsequent `OnLinkState` calls ramp values along the curve

### Platform handling

| Platform | Behavior |
|---|---|
| Linux | Native: netns + veth + tc/netem. Requires `CAP_NET_ADMIN` (typically root or equivalent). |
| macOS | Auto-detects `runtime.GOOS == "darwin"`. Checks for `docker` on PATH. Builds/pulls the project's Docker image, bind-mounts the target binary into the container, re-invokes `ntnbox run` inside it with the same flags. Prints what it's doing to stderr. |
| Windows | Not supported. Clear error: "ntnbox run requires Linux network namespaces; not supported on Windows." |

**macOS constraint (v1):** The wrapped command must be either:
- A static binary (bind-mounted into the container), or
- A command already available inside the container image (curl, wget,
  standard tools)

Complex apps with local file dependencies should use the Dockerfile
directly or pass explicit `--mount` paths (potential future flag, not
in v1).

If Docker is not found on macOS: error message says "ntnbox run
requires Linux network namespaces. On macOS, install Docker Desktop
and retry."

### Netem shim detail

The shim shells out to `tc` and `ip` via `os/exec` (matching the
design doc's decision to use iproute2 rather than a Go netlink library
for v1). Commands issued:

**Setup (once, at namespace creation):**
```bash
ip netns add ntnbox-<device-id>
ip link add veth-outer type veth peer name veth-inner
ip link set veth-inner netns ntnbox-<device-id>
ip addr add 10.200.0.1/30 dev veth-outer
ip netns exec ntnbox-<device-id> ip addr add 10.200.0.2/30 dev veth-inner
ip link set veth-outer up
ip netns exec ntnbox-<device-id> ip link set veth-inner up
ip netns exec ntnbox-<device-id> ip route add default via 10.200.0.1
# Enable NAT so wrapped process can reach the internet
iptables -t nat -A POSTROUTING -s 10.200.0.0/30 -j MASQUERADE
# Initial qdisc
ip netns exec ntnbox-<device-id> tc qdisc add dev veth-inner root netem \
  delay <d>ms <j>ms loss <l>% rate <bw>kbit
```

**Update (every 250ms tick that passes the bus throttle):**
```bash
ip netns exec ntnbox-<device-id> tc qdisc change dev veth-inner root netem \
  delay <d>ms <j>ms loss <l>% rate <bw>kbit
```

**Out of coverage:**
```bash
ip netns exec ntnbox-<device-id> tc qdisc change dev veth-inner root netem \
  loss 100%
```

**Teardown (on exit):**
```bash
ip netns del ntnbox-<device-id>
# (deleting the netns also removes the veth pair)
iptables -t nat -D POSTROUTING -s 10.200.0.0/30 -j MASQUERADE
```

### Reference poller

```text
cmd/poller/main.go

Flags:
  --url       Target URL (default: http://localhost:8080/echo)
  --interval  Poll interval (default: 2s)

Output (one line per request):
  2026-07-07T10:25:51Z | 200 |  42ms | ok
  2026-07-07T10:25:53Z | 200 | 148ms | ok
  2026-07-07T10:25:55Z |   0 |    — | timeout (5s)
  2026-07-07T10:25:57Z |   0 |    — | timeout (5s)
  2026-07-07T10:25:59Z | 200 | 153ms | ok    ← coverage returned
```

### Dockerfile

```dockerfile
FROM alpine:3.20
RUN apk add --no-cache iproute2 iptables curl
COPY ntnbox /usr/local/bin/ntnbox
COPY poller /usr/local/bin/poller
ENTRYPOINT ["ntnbox"]
```

Built by `make docker` (multi-stage: Go build + Alpine runtime).

### /echo endpoint

Added to `apihost.Server`:
```text
GET /echo → {"ts":"2026-07-07T10:25:51Z"}
```

Minimal, deterministic, no auth — exists solely as the default poller
target.

## What this step proves

- The module contract works end-to-end with a real module
- The Condition Engine drives real traffic shaping in real time
- The event bus throttling prevents flooding tc with redundant updates
- A developer with zero telecom knowledge can test their app under
  satellite conditions in under a minute

## Explicitly out of scope

- Android `SatelliteManager` / `TRANSPORT_SATELLITE` mirroring (design
  doc says "later, once core sandbox is proven")
- HTTP/SOCKS proxy mode fallback (design doc acknowledges it as a
  possible later addition for environments without CAP_NET_ADMIN)
- Multiple concurrent devices/namespaces in one `ntnbox run` invocation
  (one device per run; multiple devices can use separate runs or the
  serve + API approach)
- Persistence of sandbox runs / saved-run comparison (Step 4 territory)
- Multi-profile hot-switching during a run (restart with a different
  profile instead)
