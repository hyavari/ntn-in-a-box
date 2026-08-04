# Architecture

One platform, three capabilities, on a shared kernel:

```
Dev Sandbox          Messaging/SOS         Service API (CAMARA-aligned)
CLI · virtual UE       store-and-forward     REST endpoints
        \                    |                    /
         \                   |                   /
          --------- module contract (5 hooks) ---
                             |
              Platform Kernel (build once)
   Condition Engine · Device registry · IMS Adapter
   Event bus · Driver loop · API host
```

## Kernel components

| Package | Responsibility |
|---|---|
| `profile` | Parses and validates YAML pass-shape profiles (schedule + piecewise-linear impairment curves) |
| `condition` | Given a profile + epoch, computes coverage state and interpolated link impairments at any instant |
| `driver` | Ticks every 250ms, evaluates the Condition Engine, publishes coverage events and link state to the bus |
| `eventbus` | In-process pub/sub with throttled link-state (>5% delta or 250ms heartbeat), unthrottled coverage, and dual-path handover events |
| `device` | In-memory registry of virtual UEs and real-phone stubs, each associated with a profile |
| `imsadapter` | Pluggable message delivery backend (mock with failure injection; real IMS later) |
| `apihost` | HTTP server: health, profiles, devices, condition (+ bearer/paths), SSE (incl. handover), echo |

## Dev Sandbox module

| Component | Responsibility |
|---|---|
| `devsandbox` | Module implementing the 5-hook contract; receives events, drives the netem shim |
| `netem` | Translates link-state values into `tc qdisc change` commands inside a network namespace |
| `netns` | Creates/destroys Linux network namespaces with veth pairs and NAT routing |
| `path` | Dual-path bearer selection (`satellite` / `terrestrial`) when `terrestrial_fallback` is on |

### Dual-path (terrestrial fallback)

Opt-in via the profile. The namespace gets **two egress veths** (sat
`10.200.0.0/30`, terr `10.200.1.0/30`). On each satellite coverage edge the
module reconciles serially: shape sat (fail-closed full loss when leaving;
restore impairments before return) → `ip route replace default via …` →
commit bearer → publish `HandoverEvent`.

```
CoverageEvent (in_coverage, in_blockage)
        │
        ▼
  path.Desired → SetDefaultVia → path.TransitionTo
        │                              │
        ▼                              ▼
   sat/terr netem                 HandoverEvent → bus
                                        │
                          ┌─────────────┼─────────────┐
                          ▼             ▼             ▼
                       report         SSE           TUI/GUI
```

`in_coverage` stays satellite truth; `selected_bearer` is the live default
route. Details: [Profiles](profiles.md#terrestrial-fallback-dual-egress),
[API](api.md#condition-get-devicesidcondition).

## Data flow

```
profile.yaml → profile.LoadFile() → Profile (static)
                                        │
                      condition.NewEvaluator(profile, epoch)
                                        │
                                        ▼
                                    Evaluator ─── condition.Eval interface
                                        │
               driver.Loop ticks 250ms, calls Evaluate(now)
                                        │
                         ┌──────────────┴──────────────┐
                         ▼                              ▼
              CoverageEvent                      LinkState
           (transitions + lookahead)         (while in coverage)
                         │                              │
                         ▼                              ▼
                    eventbus.Bus ──────────────────────────
                         │
            ┌────────────┼────────────┐
            ▼            ▼            ▼
      Dev Sandbox    Messaging    Service API
      (netem/tc)     (shipped)     (future)
```

### TLE path (alternative to profile.yaml)

```
satellite.tle + observer location + link model
        │
        ▼
  tle.PredictPasses() → []Pass (SGP4 propagation)
        │
        ▼
  tle.GenerateProfile() → []profile.Profile (one per pass)
        │
        ▼
  tle.SequenceEvaluator ─── condition.Eval interface
        │                    (same interface as Evaluator)
        ▼
  driver.Loop (unchanged) → eventbus → modules
```

The `SequenceEvaluator` satisfies `condition.Eval` and implements
`condition.Advancer` for variable-rate time (1x during passes, Nx
during gaps). The driver calls `Advance(wallNow)` each tick; all
other consumers (SSE, TUI, recorder) call `Evaluate()` as a pure read.

## Module contract

Every capability module plugs into the kernel through 5 hooks:

1. `RegisterRoutes(host)` — add HTTP endpoints
2. `OnCoverageEvent(event)` — react to coverage transitions + lookahead
3. `OnLinkState(state)` — react to link impairment changes
4. `DeliverVia(adapter)` — optionally deliver messages via IMS backend
5. `Emit(emitter)` — push observability events
