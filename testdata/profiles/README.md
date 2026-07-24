Sample pass-shape profiles for tests and manual runs. Schema is defined
alongside the loader in `internal/kernel/profile`.

The same YAML files are **embedded** in the `ntnbox` binary
(`internal/kernel/profile/builtins/`). Short names work without a checkout:

```bash
ntnbox run --profile nbiot_ntn -- ./poller
ntnbox serve --profile lband_geo
```

On macOS, `ntnbox run` materializes builtins to a temp file and bind-mounts
them into Docker, so short names work even if `ntnbox:latest` is older than
this binary.

A filesystem path (anything containing `/`, or a bare filename that exists
on disk) still loads that file. Keep `testdata/profiles/` and
`builtins/` byte-identical (enforced by tests).

## Profiles

- `leo_pass_90s.yaml` — single-satellite LEO pass (rise, overhead, set,
  then a real out-of-coverage gap), e.g. Iridium/Swarm-style
  store-and-forward messaging satellites.
- `geo_steady.yaml` — always-in-coverage GEO link, flat high-latency
  profile (HTS-scale bandwidth).
- `d2c_burst.yaml` — Direct-to-Cell: narrowband, opportunistic bursts to
  an unmodified phone (Starlink Direct to Cell, AST SpaceMobile).
- `sos_burst.yaml` — emergency / SOS short burst (15s window, long gap,
  tiny bandwidth, elevated loss). Good default for queue-across-gap demos.
- `sos_hostile.yaml` — harsher SOS variant (10s window, higher loss).
  Stress-tests offline queues and deadline-aware send.
- `geo_blockage.yaml` — always-in-coverage GEO link with intermittent,
  *unscheduled* blockage (tunnel / terrain / tree cover). Models the
  automotive case: coverage never drops for orbital reasons, but a moving
  vehicle still loses the link — and with no lookahead, so apps must
  recover reactively. Stress-tests reconnect/backoff against surprise
  drops rather than scheduled passes.

### Named bearer presets (3GPP-NTN link classes)

- `nbiot_ntn.yaml` — NB-IoT NTN-class: continuous, few-kbps, high delay,
  messaging-first.
- `lband_geo.yaml` — L-band GEO-class: continuous ~600 ms RTT, modest
  bandwidth (not HTS `geo_steady`).
- `leo_d2c.yaml` — LEO direct-to-cell bursts; sibling of `d2c_burst` with
  a recognizable D2C label.

## Blockages

Any profile may include a `blockages` list: repeating, unscheduled
outages layered on top of the schedule (see `Blockage` in
`internal/kernel/profile`). Each blockage is an `{offset_sec,
duration_sec}` interval within one `period_sec` cycle; it repeats every
cycle and is active on the half-open interval
`[offset_sec, offset_sec + duration_sec)`. Blockages must fit within the
cycle (no wrap) and be strictly ascending and non-overlapping.

Unlike a periodic window close, a blockage carries **no lookahead** — set
`lookahead_sec: 0` — because a real vehicle cannot predict a tunnel from
orbital mechanics. They are primarily intended for continuous profiles
but are permitted on any mode (on a periodic profile they only bite while
a window would otherwise be open). Blockage timings are illustrative
engineering values, not measurements of any specific route.

## Sourcing

Curve *values* (delay/jitter/loss/bandwidth) are grounded in published
measurements where noted below; everything else is an explicitly
flagged engineering estimate, not a citation. Window/period *timing* in
all three profiles is illustrative (fast dev-loop values), not a
measurement of any specific real constellation's revisit schedule —
that depends on satellite count/altitude/inclination, which isn't
specified anywhere in the project design.

- **GEO round-trip delay (600ms)**: pure propagation is ~240-260ms
  round-trip at 35,786km altitude; real-world consumer GEO broadband
  measures 550-800ms round-trip once ground processing/routing is
  included. Ookla 2025 data: HughesNet 683ms median, Viasat 684ms
  ([Ookla, "Latency is the Achille's Heel for HughesNet, Viasat"](https://www.ookla.com/articles/hughesnet-viasat-performance-2025)).
- **LEO steady-state delay (40ms)**: within the commonly-measured
  real-world range (~20-60ms) for LEO broadband in good conditions.
- **LEO pass-edge delay (100-150ms)**: based on a measured
  handover-induced latency spike of ~140ms at the start and ~75ms at
  the end of each cycle in a real LEO deployment
  ([arXiv:2601.08439, "Statistical Characterization and Prediction of
  E2E Latency over LEO Satellite Networks"](https://arxiv.org/html/2601.08439v1)) —
  used as the basis for the ramp shape rather than an arbitrary spike.
- **D2C bandwidth (64 kbps)**: loosely informed by the one measurement
  found, ~3 Mbps *per beam* (shared across users in that beam), not a
  per-device figure
  ([arXiv:2506.00283, "A First Look into Starlink's Direct
  Satellite-to-Device Radio Access Network through Crowdsourced
  Measurements"](https://arxiv.org/html/2506.00283v2)).
- Everything else (jitter, loss_pct, GEO/LEO bandwidth, all D2C values
  except bandwidth) is an engineering estimate, explicitly commented as
  such in each YAML file. If you have better sourcing, replace them —
  they were not derived from a specific spec, dataset, or channel model
  (e.g. 3GPP TR 38.811, which OpenNTN implements and this project
  deliberately does not — see the design doc's non-goals).
