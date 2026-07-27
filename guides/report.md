# Field-data report

Write a JSON summary when an `ntnbox run` session ends:

```bash
ntnbox run --report out.json --profile testdata/profiles/geo_blockage.yaml -- ./poller

# or via demos
./scripts/demo.sh --report out.json
./scripts/demo-blockage.sh --report out.json
./scripts/demo-voice.sh --report out.json
```

On stop (command exit or Ctrl+C), ntnbox writes the file and prints a one-line
stderr summary.

## Fields

| Field | Meaning |
|-------|---------|
| `started_at` / `ended_at` | Session bounds (RFC3339) |
| `duration_sec` | Wall-clock length of the run |
| `profile` | Profile name (or `tle:…`) |
| `coverage.in_pct` / `in_sec` | Time the link was up (see below) |
| `coverage.blocked_pct` / `blocked_sec` | Unscheduled blockage (tunnel/terrain) |
| `coverage.out_pct` / `out_sec` | Scheduled gap (periodic window closed) |
| `coverage.opens` / `closes` | Scheduled window open/close counts (not blockage enter/exit) |
| `messaging.present` | `false` if no store-and-forward traffic |
| `messaging.unique` | Distinct message IDs seen |
| `messaging.delivered` / `failed` / `open` | Latest status per ID |
| `messaging.delivery_rate` | `delivered / unique` when `present` |
| `voice.capable` | `true` for voice-oriented profiles (`lband_geo`, `geo_steady`, `geo_blockage`) |
| `voice.estimates.*` | Link-derived mouth-to-ear / MOS-ish / stress (when capable) |
| `voice.calls.present` | `false` unless call-event telemetry was ingested |
| `voice.calls.attempted` / `completed` / `dropped` / `open` | Latest status per call id (`open` = in-flight / `started`) |
| `voice.calls.completion_rate` / `drop_on_close_rate` | Rates over attempted |

Call session stats are included whenever call-events were ingested, even if
`voice.capable` is false (no link-derived estimates in that case).

### Coverage seconds vs percent

- `in_sec` — wall-clock seconds during the run when the link was up
  (in coverage, not blocked).
- `in_pct` — that share of the whole run:
  `in_pct = 100 * in_sec / duration_sec`

`blocked_*` and `out_*` use the same pattern for blockage and scheduled gaps.
The three `*_pct` values are of `duration_sec` and should sum ≈ 100%.

Blockage is detected by sampling coverage state (including mid-window drops),
not only from `window_closed` events.

Poller/curl-only runs leave messaging as `{ "present": false }`. Messaging
stats appear when something uses the store-and-forward API during the run.

### Voice estimates (illustrative)

When `voice.capable` is true, the report samples in-coverage delay/jitter/loss
about once per second and derives engineering estimates (not ITU E-model
calibrated). If no in-coverage samples were collected, `estimates` is omitted
(so short or fully-blocked runs do not report `mos_avg: 0`).

- `mouth_to_ear_ms ≈ delay_ms / 2 + 40`
- jitter-buffer stress and PLC pressure from jitter/loss
- coarse MOS in `[1.0, 4.5]`

Call session stats appear when a client posts
`POST /devices/{id}/call-events` (the `voicecall` sample does this). That
ingest is telemetry only — not a full Voice REST API.

Averages (`mos_avg`, stress, PLC) use every in-coverage sample. Percentiles
(`mouth_to_ear_ms_p50` / `p95`) use at most the last ~3600 samples (~1 hour
at 1 Hz) to bound memory.

## Example (`demo-voice.sh`)

After a short `lband_geo` + `voicecall` run, `jq .` looks like:

```json
{
  "started_at": "2026-07-27T16:09:37Z",
  "ended_at": "2026-07-27T16:10:51Z",
  "duration_sec": 74.5,
  "profile": "lband_geo",
  "coverage": {
    "in_pct": 100,
    "blocked_pct": 0,
    "out_pct": 0,
    "in_sec": 74.5,
    "blocked_sec": 0,
    "out_sec": 0,
    "opens": 1,
    "closes": 0
  },
  "messaging": { "present": false },
  "voice": {
    "capable": true,
    "estimates": {
      "mouth_to_ear_ms_p50": 340,
      "mouth_to_ear_ms_p95": 340,
      "mos_avg": 3.47,
      "jitter_buffer_stress_avg": 0.25,
      "plc_pressure_avg": 0.1,
      "in_coverage_sample_count": 74
    },
    "calls": {
      "present": true,
      "attempted": 7,
      "completed": 6,
      "dropped": 1,
      "open": 0,
      "completion_rate": 0.857,
      "drop_on_close_rate": 0.143
    }
  }
}
```

Inspect just the voice block: `jq .voice out.json`.

