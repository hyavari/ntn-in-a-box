# Step 0 — Kernel MVP: Implementation Plan

Ref design: `docs/superpowers/specs/2026-07-03-ntn-in-a-box-design.md`

**Resume point (last updated 2026-07-07):** All tasks (1-11) complete.
**Step 0 is done.** Next step: Step 1 — Dev Sandbox module.

## Progress (update this section as tasks complete — this is the
## source of truth for resuming across sessions, not just tool state)

- [x] Task 1 — License decided (Apache 2.0), repo created and pushed
      (`github.com/hyavari/ntn-in-a-box`, public). Commit `f547375`.
- [x] Task 2 — Repo scaffolding: `go.mod`, directory layout, `.gitignore`
      (docs/ excluded), `.golangci.yml`, `Makefile`. Commit `f547375`.
- [x] Task 3 — Profile schema + YAML loader
      (`internal/kernel/profile/{profile,validate,loader}.go` + tests).
      Sample profiles `leo_pass_90s`, `geo_steady`, `d2c_burst` in
      `testdata/profiles/`, curve values sourced/cited where possible
      (see `testdata/profiles/README.md`). Reviewed via semantic
      reviewer subagent, 5 issues found and fixed (NaN bypass in curve
      validation, schedule-validation early-return, unvalidated
      continuous-mode lookahead, undefined curve tail semantics,
      missing multi-error-aggregation test). Commit `9f486b2`, pushed.
- [x] Task 4 — Condition Engine core: curve evaluator + coverage
      scheduler (`internal/kernel/condition/{engine,scheduler,curve}.go`
      + tests). Reviewed via semantic reviewer subagent; fixed weak
      end-to-end test coverage (only sampled flat/exact curve points,
      not real interpolation) and added defensive copying of curve
      slices in NewEvaluator so external mutation can't leak in.
- [x] Task 5 — Event bus with throttled `LinkState` emission
      (`internal/kernel/eventbus/{events,throttle,bus}.go` + tests).
      Coverage events and Emit are never throttled; LinkState publishes
      on >5% relative delta in any field or a 250ms heartbeat. Reviewed
      via semantic reviewer subagent; fixed a NaN-poisoning gap (a
      contract-violating NaN publish would have permanently disabled
      delta-based throttling), a zero-transition asymmetry in the delta
      calc, added a concurrency test run under `-race`, and documented
      that a Bus is scoped to one link-state stream (not shareable
      across multiple devices without keying — relevant once Task 7's
      device registry lands). README updated with a "Kernel internals"
      section and flow diagram covering Condition Engine + Event bus.
- [x] Task 6 — Module contract interfaces (`pkg/module/{module,ims,noop}.go`
      + tests). `RouteRegistrar`/`IMSAdapter`/`Emitter` are pkg/module's
      own consumer-side interfaces (not imports of the not-yet-built
      `apihost`/`imsadapter` packages) so those packages can satisfy
      them structurally later without pkg/module depending on them now.
      `eventbus.Bus` already satisfies `Emitter` today. Confirmed with
      the project author: `Emit(emitter Emitter)` hands the module a
      capability once (module pushes events out later), matching
      `RegisterRoutes`/`DeliverVia`'s pattern — not "kernel calls Emit
      once per event," which was the other plausible reading of the
      design doc's `emit(event)` wording.
      Note for Task 9 (apihost): `RouteRegistrar` matches
      `http.ServeMux.Handle`, which panics on duplicate pattern
      registration — decide how overlapping module routes are handled
      (namespacing? conflict detection?) when apihost is actually built.
- [x] Task 7 — Device/identity registry (`internal/kernel/device`).
      In-memory registry: `Device` struct (ID, Type, ProfileName,
      CreatedAt), `Registry` with Register/Get/List/Remove, two device
      types (TypeVirtualUE, TypeRealPhone), sentinel errors
      (ErrDuplicateID, ErrNotFound), input validation. Lint caught a
      stutter (`device.DeviceType` → `device.Type`), fixed. Tests
      include concurrency (100 goroutines, `-race`). README updated
      with device registry in the kernel internals table + data flow.
- [x] Task 8 — IMS Adapter interface + mock backend
      (`internal/kernel/imsadapter`). `MockAdapter` satisfies
      `pkg/module.IMSAdapter`: configurable FailRate/QueueDelay/
      InFlightDelay, async receipt callbacks (queued → in-flight →
      delivered/failed), ctx cancellation stops receipts, injectable
      clock for deterministic testing, atomic monotonic IDs,
      concurrent-safe. 6 tests pass with `-race`. README updated.
- [x] Task 9 — API host with minimal HTTP routes
      (`internal/kernel/apihost`). `Server` wires profile store +
      device.Registry + per-device condition.Evaluator. Routes:
      GET /health, GET /profiles, GET /profiles/{name}, POST /devices,
      GET /devices, GET /devices/{id}, GET /devices/{id}/condition.
      Go 1.22+ ServeMux pattern routing, stdlib only. 12 integration
      tests via httptest. README updated with apihost in table + data
      flow.
- [x] Task 10 — CLI skeleton: `ntnbox serve --profile <name>`
      (`cmd/ntnbox`). Parses --profile (required) and --addr
      (default :8080), loads profile, wires apihost.Server +
      device.Registry, graceful shutdown on SIGINT/SIGTERM.
- [x] Task 11 — End-to-end manual check of Step 0 kernel MVP.
      Built binary, ran `ntnbox serve --profile leo_pass_90s.yaml`,
      curled all endpoints: health OK, profiles list/get, device
      registration, condition query returns in_coverage=true with
      interpolated curve values matching the profile (delay ~150ms
      near window start, bandwidth ~2000kbps). Error cases return
      proper 404 JSON. Graceful shutdown works.

**Step 0 is complete.** All 11 tasks done. Next: Step 1 (Dev Sandbox
module).

To resume this work in a new session: read this file's Progress
section first, then `docs/superpowers/specs/2026-07-03-ntn-in-a-box-design.md`
for the full design context, then `git log --oneline` to confirm what's
actually committed matches what's checked off above.

Goal: a kernel that can load a pass-shape profile, simulate a satellite
pass over time (coverage windows + continuous link state), expose it via
a minimal API, and provide a mock IMS Adapter — with no modules built yet.
Definition of done: kernel builds, is unit-tested, and running it with a
sample profile produces a correct, observable sequence of coverage/link
events end-to-end.

Explicitly out of scope for Step 0 (belongs to later steps per the design
doc): `tc`/netem application to real traffic (Step 1, Dev Sandbox-specific),
any capability module, real IMS backend, auth/multi-tenancy.

## Repo layout

```
cmd/ntnbox/              # CLI entrypoint (thin, wires kernel together)
internal/kernel/
  profile/                # profile schema + YAML loader + validation
  condition/               # curve evaluation + coverage scheduler
  eventbus/                # pub/sub: coverage events, link state, generic emit
  device/                  # device/identity registry (virtual UE, real phone stub)
  imsadapter/               # IMSAdapter interface + mock backend
  apihost/                  # HTTP server, routes
pkg/module/                # module contract interfaces (5 hooks) — consumed
                             # by future modules, not implemented here
testdata/profiles/         # sample profiles (leo_pass_90s.yaml, geo_steady.yaml)
```

## Tasks, in order

Note: numbering here is offset by one from the Progress section above
(Progress splits "license/repo" and "scaffolding" into separate Task 1/
Task 2; this section merges them into a single item 1). When resuming,
match by *description*, not by number, to avoid confusion — e.g.
Progress's "Task 7 — Device/identity registry" is item 6 below.

1. **Repo scaffolding.** `go.mod`, directory layout above, `.gitignore`
   (Go defaults + editor files), `Makefile` or documented `go build`/`go
   test` commands in README.

2. **Profile schema + loader** (`internal/kernel/profile`).
   - Finalize the YAML schema sketched in the design doc's open questions
     (window open/close/lookahead + per-metric curves for
     delay/jitter/loss/bandwidth). Decide piecewise-linear vs. sampled
     points here — piecewise-linear (a list of `{t_offset, value}`
     points, linearly interpolated) is the simplest that still supports
     ramp-up/steady/ramp-down and should be the default unless it proves
     insufficient.
   - Loader + validation (reject malformed curves, missing fields).
   - Two sample profiles: `leo_pass_90s`, `geo_steady`.
   - Unit tests: valid/invalid profile loading, curve point ordering.

3. **Condition Engine core** (`internal/kernel/condition`).
   - Curve evaluator: given a profile and an elapsed time within a
     window, return interpolated delay/jitter/loss/bandwidth.
   - Coverage scheduler: given a profile's repeat/window pattern and a
     start time, compute window open/close times and expose lookahead
     (time until next open/close).
   - Unit tests: curve values at known offsets (start/mid/end of
     ramp/steady segments), scheduler correctness for sequential windows.

4. **Event bus** (`internal/kernel/eventbus`).
   - In-process pub/sub for: `CoverageEvent` (open/close + lookahead
     announcements) and `LinkState` (continuous curve values).
   - Implement the throttling decision from the design doc: emit
     `LinkState` on meaningful delta (configurable threshold per metric)
     or at most every N ms, whichever comes first — pick concrete
     defaults (e.g., 250ms / 5% delta) and make both configurable.
   - Unit tests: throttling behavior (burst of tiny changes suppressed,
     a big jump always emitted, periodic emission when values are static).

5. **Module contract types** (`pkg/module`).
   - Go interfaces for the 5 hooks (`RegisterRoutes`, `OnCoverageEvent`,
     `OnLinkState`, `DeliverVia`, `Emit`), matching the design doc's
     contract exactly.
   - A trivial no-op reference implementation used only in tests, to
     confirm the kernel can register/drive a module without errors.
     (Not a real module — Dev Sandbox is Step 1.)

6. **Device/identity registry** (`internal/kernel/device`).
   - In-memory registry: register a virtual UE or a real-phone stub
     device, look up by ID, associate with a profile.
   - No persistence, no auth/multi-tenancy yet (explicitly deferred).

7. **IMS Adapter interface + mock backend** (`internal/kernel/imsadapter`).
   - `IMSAdapter` interface: submit message, receive delivery/read
     receipt callback.
   - Mock implementation per design decision 6: queued → in-flight →
     delivered/failed transitions, simulated receipts with timestamps,
     configurable failure injection (fail rate, timeout).
   - Unit tests: happy path delivery, injected failure + retry-visible
     state, receipt timestamps.

8. **API host** (`internal/kernel/apihost`).
   - Minimal HTTP server (stdlib `net/http` is enough at this scale —
     no need for a router framework yet).
   - Routes: health check, list/get profiles, register a device, get
     current condition state (coverage + link state) for a device.
   - Integration test: start server, drive a profile forward via a fake
     clock, assert API responses match expected coverage/link state.

9. **CLI skeleton** (`cmd/ntnbox`).
   - `ntnbox serve --profile <name>`: starts the kernel + API host with
     a given profile loaded. This is plumbing only — `ntnbox run` (the
     netns-wrapping command from design decision 8) is Step 1, not here.

10. **End-to-end check.** Run `ntnbox serve --profile leo_pass_90s`,
    poll the API, and manually confirm coverage opens/closes and link
    state changes match the profile's curve — this is the "kernel is
    useful on its own" proof before starting Step 1.

## Open items to resolve during this step (not before)

- Exact curve interpolation method (default: piecewise-linear, confirm
  once curve evaluator is written).
- Default `LinkState` throttling numbers (default: 250ms / 5% delta,
  tune once observable).
