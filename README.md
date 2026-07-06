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
