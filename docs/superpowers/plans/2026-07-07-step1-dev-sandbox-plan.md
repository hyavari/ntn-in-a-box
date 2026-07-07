# Step 1 — Dev Sandbox Module: Implementation Plan

Ref design: `docs/superpowers/specs/2026-07-07-step1-dev-sandbox-design.md`

**Resume point (last updated 2026-07-07):** Task 1 done. **Next up:
Task 2 — Netem shim.**

## Progress

- [x] Task 1 — Driver loop (`internal/kernel/driver`). `Loop` struct
      with Config (Evaluator, Bus, LookaheadSec, TickCh, Interval, Now).
      `Run(ctx)` ticks 250ms, evaluates, detects coverage transitions
      (including initial state announcement), fires lookahead events
      (window_closing/window_opening when UntilNextTransition <=
      LookaheadSec), publishes link state while in coverage. Accepts
      injectable tick channel + clock for testing. 4 tests pass with
      `-race`: coverage transitions, link state published, no link
      state out of coverage, continuous mode. README updated (driver
      loop row → Done, data flow diagram updated).
- [ ] Task 2 — Netem shim (`internal/module/devsandbox/netem`). **Next.**
- [ ] Task 3 — Namespace wrapper (`internal/module/devsandbox/netns`)
- [ ] Task 4 — Dev Sandbox module (`internal/module/devsandbox`)
- [ ] Task 5 — `/echo` endpoint in apihost
- [ ] Task 6 — `ntnbox run` CLI command (Linux path)
- [ ] Task 7 — macOS Docker proxy
- [ ] Task 8 — Dockerfile + `make docker`
- [ ] Task 9 — Reference poller (`cmd/poller`)
- [ ] Task 10 — Integration test: full loop on Linux
- [ ] Task 11 — README + docs update

To resume: read this file's Progress section, then the design spec,
then `git log --oneline` to confirm.

## Repo layout (new/modified)

```
internal/kernel/driver/           # driver loop (tick → evaluate → publish)
internal/module/devsandbox/       # Dev Sandbox module (5-hook contract)
  netem/                          # netem shim (tc qdisc commands)
  netns/                          # namespace wrapper (ip netns + veth)
cmd/ntnbox/                       # updated: adds `run` subcommand
cmd/poller/                       # reference HTTP poller
Dockerfile                        # Alpine + iproute2 + ntnbox + poller
```

## Tasks, in order

### Task 1 — Driver loop (`internal/kernel/driver`)

The missing kernel piece: a goroutine that ticks every 250ms, calls
`Evaluate(now)` on a device's Evaluator, detects coverage transitions,
and publishes to the event bus.

**Deliverables:**
- `driver.Loop` struct: holds an Evaluator, a Bus, and a tick interval
- `Loop.Run(ctx)` — blocks until ctx is cancelled; on each tick:
  - Calls `Evaluate(now)` to get current coverage + link state
  - Compares coverage to previous state; if changed, publishes
    `CoverageEvent` (opened/closed + lookahead notices)
  - If in coverage, calls `Bus.PublishLinkState()` (bus handles
    throttling)
- Lookahead: when out of coverage and time-until-next-transition <=
  profile's LookaheadSec, publish `KindWindowOpening`; when in
  coverage and time-until-close <= LookaheadSec, publish
  `KindWindowClosing`
- Unit tests: mock evaluator or real evaluator with a fast profile,
  verify correct event sequence over a simulated pass

**Design notes:**
- Uses `time.NewTicker(250ms)` for real usage, but accepts a
  `tickCh <-chan time.Time` for testability (inject fake ticks)
- Does NOT import or know about netem/netns — it just publishes to
  the bus; the module subscribes

### Task 2 — Netem shim (`internal/module/devsandbox/netem`)

Translates `LinkState` values into `tc qdisc` commands.

**Deliverables:**
- `netem.Controller` struct: holds namespace name, device name
- `Controller.Setup()` — adds initial qdisc (`tc qdisc add ...`)
- `Controller.Apply(state LinkState)` — `tc qdisc change ...` with
  delay/jitter/loss/rate from state
- `Controller.SetFullLoss()` — `tc qdisc change ... loss 100%`
- `Controller.Teardown()` — removes qdisc (or no-op if netns deletion
  handles it)
- An `Executor` interface for the actual shell-out (`os/exec` real
  implementation + a mock for testing)
- Unit tests: verify correct command strings are generated for
  various LinkState values, including edge cases (zero jitter, zero
  loss, very high bandwidth)

**Design notes:**
- Does NOT create or manage the namespace — it operates on an
  already-existing namespace/device pair
- Bandwidth uses `rate` parameter in kbit
- Delay uses `delay <mean>ms <jitter>ms` (netem interprets jitter as
  a uniform deviation around the mean)

### Task 3 — Namespace wrapper (`internal/module/devsandbox/netns`)

Creates and tears down the Linux network namespace + veth pair.

**Deliverables:**
- `netns.Namespace` struct: holds namespace name, veth names, subnet
- `Namespace.Create()` — executes the setup sequence:
  - `ip netns add ntnbox-<id>`
  - `ip link add <outer> type veth peer name <inner>`
  - `ip link set <inner> netns ntnbox-<id>`
  - Assign addresses (10.200.0.1/30 outer, 10.200.0.2/30 inner)
  - Bring both interfaces up
  - Set default route inside netns
  - Enable NAT (iptables MASQUERADE)
- `Namespace.Exec(ctx, cmd, args) (*exec.Cmd, error)` — returns an
  `exec.Cmd` that runs inside the namespace via `ip netns exec`
- `Namespace.Destroy()` — `ip netns del` + remove iptables rule
- Same `Executor` interface as netem for testability
- Unit tests: verify correct commands generated (no real netns in
  unit tests; integration test in Task 10 exercises real execution)

**Design notes:**
- Uses a fixed /30 subnet per namespace. For v1, only one namespace
  at a time is expected per `ntnbox run` invocation
- IP forwarding (`sysctl net.ipv4.ip_forward=1`) must be enabled;
  check on setup and warn/fail if not

### Task 4 — Dev Sandbox module (`internal/module/devsandbox`)

The first real module implementing the 5-hook contract.

**Deliverables:**
- `devsandbox.Module` struct: holds a `netem.Controller` reference
- Implements `pkg/module.Module`:
  - `RegisterRoutes(host)` → `GET /sandbox/status` returns current
    shaping values as JSON
  - `OnCoverageEvent(ev)` → on `window_closed`: call
    `controller.SetFullLoss()`; on `window_opened`: call
    `controller.Apply()` with last known state (or initial values)
  - `OnLinkState(state)` → call `controller.Apply(state)` + store
    as "last known state"
  - `DeliverVia(adapter)` → no-op
  - `Emit(emitter)` → store emitter; push events on each shaping
    change
- Thread safety: `OnCoverageEvent` and `OnLinkState` may fire
  concurrently (per Module contract docs)
- Unit tests: mock netem controller, verify Apply/SetFullLoss are
  called with correct values on various event sequences

### Task 5 — `/echo` endpoint in apihost

**Deliverables:**
- Add `GET /echo` to `apihost.Server`: returns
  `{"ts":"2026-07-07T10:25:51Z"}`
- One test in `server_test.go`
- Minimal change, exists for the reference poller's default target

### Task 6 — `ntnbox run` CLI command (Linux path)

Wires everything together for the Linux-native case.

**Deliverables:**
- New `runServe` → `runRun` function in `cmd/ntnbox/main.go` (or a
  separate file `cmd/ntnbox/run.go`)
- Parses: `--profile <path>`, `--addr <host:port>` (optional), `--`
  separator, then the command + args
- Sequence:
  1. Load profile
  2. Create device registry + register a virtual UE
  3. Create namespace (`netns.Namespace.Create()`)
  4. Create netem controller for the namespace
  5. Create Dev Sandbox module with the controller
  6. Create apihost.Server (optionally listen if --addr given)
  7. Create driver loop (evaluator + bus)
  8. Subscribe module to bus (`OnCoverageEvent`, `OnLinkState`)
  9. Start driver loop
  10. Launch user command inside namespace (`Namespace.Exec()`)
  11. Stream condition changes to stderr
  12. Wait for: user command exit OR SIGINT/SIGTERM
  13. Teardown: stop driver loop, destroy namespace, stop server
- Exit code: forward the wrapped command's exit code

### Task 7 — macOS Docker proxy

**Deliverables:**
- In `ntnbox run`, before doing any Linux-specific work:
  - If `runtime.GOOS == "darwin"`: enter Docker proxy path
  - Check `docker` is on PATH; if not, print clear error and exit
  - Resolve the target command to an absolute path
  - Build or pull the project's Docker image (check if image exists
    first to avoid rebuilding every time)
  - Re-invoke: `docker run --rm --privileged --cap-add NET_ADMIN
    -v <binary>:/app/<name> <image> run --profile /profiles/<name>
    -- /app/<name> [args]`
  - Bind-mount the profile file and the target binary
  - Forward signals (SIGINT → docker stop)
  - Stream stdout/stderr from the container
- Clear messages: "Detected macOS, running inside Docker...",
  "Docker not found: install Docker Desktop and retry"
- Unit test: mock docker exec, verify correct command construction

### Task 8 — Dockerfile + `make docker`

**Deliverables:**
- `Dockerfile` at project root:
  - Multi-stage: Go build stage → Alpine runtime
  - Runtime stage: Alpine + iproute2 + iptables + curl
  - Copies `ntnbox` and `poller` binaries
  - `ENTRYPOINT ["ntnbox"]`
- Add to `Makefile`:
  - `make docker` — builds the image tagged `ntnbox:latest`
- Includes sample profiles in `/profiles/` inside the image so the
  macOS proxy can use them without extra bind-mounts
- Test: `make docker` succeeds, `docker run ntnbox:latest serve
  --profile /profiles/leo_pass_90s.yaml` starts and responds to
  /health

### Task 9 — Reference poller (`cmd/poller`)

**Deliverables:**
- `cmd/poller/main.go`:
  - Flags: `--url` (default `http://localhost:8080/echo`),
    `--interval` (default `2s`), `--timeout` (default `5s`)
  - Loop: HTTP GET to url, print one line per request:
    `<timestamp> | <status> | <latency>ms | <result>`
  - On timeout: print `timeout (<duration>)`
  - On connection error: print the error
  - Ctrl+C stops cleanly
- No external dependencies (stdlib `net/http` + `flag`)
- Builds as a separate binary (`go build -o poller ./cmd/poller/`)

### Task 10 — Integration test: full loop on Linux

**Deliverables:**
- A test (skipped on non-Linux or when not root) that:
  1. Loads `leo_pass_90s` profile
  2. Creates a namespace + netem controller
  3. Starts the driver loop with a fast-forwarded tick (e.g. 10ms
     instead of 250ms, with a shortened profile for speed)
  4. Runs a simple command inside the namespace (e.g. `ping -c 1`)
  5. Verifies the command experienced non-zero latency
  6. Tears down cleanly
- This is the "does it actually work" proof, not a unit test
- Tagged with `//go:build linux` and skipped if `os.Getuid() != 0`

### Task 11 — README + docs update

**Deliverables:**
- README: update status banner, add Dev Sandbox section with usage
  examples, update roadmap (Step 1 → complete), add Docker/macOS
  instructions
- Step 1 plan: mark all tasks done
- Verify the kernel internals table reflects the driver loop as Done

## Open items to resolve during implementation

- **Exact stderr output format for condition streaming.** Direction:
  one line per meaningful change (coverage transitions always, link
  state on significant delta), prefixed with timestamp. Finalize
  when Task 6 is written.
- **Docker image naming/tagging convention.** Direction: `ntnbox:latest`
  locally, GitHub Container Registry for releases (later). Finalize
  in Task 8.
- **Multiple namespaces / veth subnet collision.** For v1, only one
  `ntnbox run` at a time is supported (fixed 10.200.0.0/30 subnet).
  If a user runs two concurrently, the second will fail at netns
  creation. Decide whether to add a counter/random suffix in Task 3
  or defer. Direction: use device ID in the netns name and a simple
  incrementing subnet offset, low-effort fix.
