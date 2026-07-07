# NTN-in-a-Box

A self-hostable, open-source platform that makes any network path behave
like a Non-Terrestrial Network (NTN) — and exposes the satellite
capabilities (coverage windows, store-and-forward messaging, reachability)
that operators currently keep closed. Built so both real phones and
pure-software apps can develop and test against realistic NTN conditions
without any telecom hardware.

> Status: early design/development. Nothing runnable yet — see
> [Roadmap](#roadmap) below.

## Why

Android already ships `SatelliteManager` and a `TRANSPORT_SATELLITE`
network type, and operators are rolling out direct-to-cell messaging and
emergency services over satellite. But apps can't be built or tested
against this: Starlink Direct-to-Cell and operator sandboxes are gated
behind commercial roaming agreements, and existing open tools
(Sionna, OpenNTN, OpenAirInterface) target the PHY/RAN layers, not the
service/API/sandbox layer developers actually need.

NTN-in-a-Box fills that gap: a condition engine that shapes real network
traffic like a satellite pass (delay, jitter, loss, bandwidth, coverage
windows), plus a pluggable module system for building capabilities like
messaging/emergency and a CAMARA-aligned service API on top of it.

## How it's shaped

One platform, three capabilities, on a shared kernel:

```
Dev Sandbox          Messaging/SOS         Service API (CAMARA-aligned)
SDK/CLI · virtual UE   store-and-forward     REST endpoints
        \                    |                    /
         \                   |                   /
          --------- module contract (5 hooks) ---
                             |
              Platform Kernel (build once)
   Condition Engine · Device registry · IMS Adapter
   Event bus + Observability · API host
```

- **Condition Engine** models satellite pass shapes (ramp-up → steady →
  ramp-down for delay/jitter/loss/bandwidth), not just flat on/off
  coverage — this is what makes it feel like NTN instead of "slow Wi-Fi."
- **Modules** (Dev Sandbox, Messaging/Emergency, Service API) plug into
  the kernel through the same 5-hook contract and don't reach into each
  other.
- **IMS Adapter** is pluggable: a mock/loopback backend for anyone with no
  telecom core, and a real IMS backend for real-phone delivery.

## Kernel internals (Step 0, in progress)

### What each piece is responsible for

| Package | Responsibility | Status |
|---|---|---|
| `profile` | Parses and validates a YAML pass-shape profile into a `Profile` struct. Pure data — no time, no behavior. | Done |
| `condition` | Given a `Profile` + a fixed starting instant ("epoch"), answers "at this instant, what's the coverage/link state?" via `Evaluate(now)`. Stateless and pull-based — you ask it, it answers; it doesn't know or care if anyone's listening. | Done |
| `eventbus` | Receives candidate state updates (`Publish...`), decides whether each one is worth telling subscribers about (throttling), and fans it out to whoever subscribed. Push-based. | Done |
| `device` | In-memory registry of virtual UEs and real-phone stubs. Each device has an ID, a type, and a profile name. The wiring layer (apihost/CLI) creates a per-device Evaluator + Bus keyed by device ID. | Done |
| `imsadapter` | Mock IMS backend: simulates message delivery lifecycle (queued → in-flight → delivered/failed) with configurable failure injection and timing. Satisfies `pkg/module.IMSAdapter`. No real protocol — just state transitions and receipt callbacks. | Done |
| *(driver loop)* | Nothing currently calls `condition.Evaluate()` on a loop and feeds the results into `eventbus.Bus`. This is the missing link between the two — a real gap, not a deferred design choice. | **Not built** |
| `apihost` | Minimal HTTP server (stdlib `net/http`, Go 1.22+ ServeMux routing). Routes: `GET /health`, `GET /profiles`, `GET /profiles/{name}`, `POST /devices`, `GET /devices`, `GET /devices/{id}`, `GET /devices/{id}/condition`. Wires profile store + device registry + per-device Evaluator together into a queryable surface. | Done |

### Data flow

```
testdata/profiles/*.yaml
        │
        │  profile.LoadFile()  — parse + validate
        ▼
  profile.Profile                 (static: describes the schedule + curves)
        │
        │  condition.NewEvaluator(profile, epoch)
        ▼
  condition.Evaluator
        │
        │  Evaluate(now)  — called on demand, for any instant "now"
        ▼
  CoverageState + LinkState       (dynamic: the answer for that instant)
        ┆
        ┆  <- nothing built yet calls this on a loop and feeds it below
        ┆     (see "driver loop" in the table above)
        ▼
  eventbus.Bus
        │  PublishCoverageEvent()  — every call delivered immediately
        │  PublishLinkState()      — throttled: >5% delta in any field,
        │                            or a 250ms heartbeat if unchanged
        ▼
  Subscriber handlers              (future: Dev Sandbox, Messaging/
                                     Emergency, Service API modules
                                     register here once they exist)


  apihost.Server                   (HTTP surface — wires it all together)
        │
        │  POST /devices           → device.Registry.Register()
        │                            + condition.NewEvaluator(profile, now)
        │  GET /devices/{id}/condition
        │                          → evaluator.Evaluate(time.Now())
        │  GET /profiles           → list loaded profiles
        │  GET /health             → liveness check
        ▼
  device.Registry                  (parallel concern — not in the
        │                            event path, but apihost uses it
        │  Register / Get / List     to decide which profile + evaluator
        ▼                            + bus to create per device)
  Device { ID, Type, ProfileName }
```

## Roadmap

| Step | Delivers | Depends on real IMS? |
|---|---|---|
| 0 | Kernel MVP — Condition Engine, device registry, event bus, API host, mock IMS backend | No |
| 1 | Dev Sandbox module — `ntnbox run --profile <name> -- <cmd>`, SDK, virtual UE | No |
| 2 | Messaging/Emergency module — store-and-forward, still on mock backend | No |
| 3 | Real IMS backend swap — real phone delivery | Yes |
| 4 | Service API module — CAMARA-aligned endpoints, dashboard | Yes |

Developers are the first audience: Steps 0–2 are fully usable standalone,
with zero telecom dependency.

## Tech stack

Go — chosen for single-binary distribution, `tc`/netem and concurrency fit,
and because it's the ecosystem this project's audience (infra/network
tooling developers) already expects.

## Development

Requires Go 1.26+.

```
make build   # go build ./...
make test    # go test ./...
make fmt     # gofmt + goimports, applied in place
make vet     # go vet ./...
make lint    # golangci-lint run ./...  (see .golangci.yml)
make check   # fmt + vet + lint + test + build — run before committing
```

`golangci-lint` and `goimports` aren't part of the standard Go toolchain;
install them once with:

```
go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest
go install golang.org/x/tools/cmd/goimports@latest
```

## Contributing

Not yet open for contributions — the kernel MVP (Step 0) is still being
designed/built. Watch this repo for updates.

## License

[Apache License 2.0](LICENSE)
