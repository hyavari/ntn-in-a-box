# API reference

## `ntnbox serve`

Query the kernel API without netns shaping (any platform):

```bash
# Auto-registers sandbox-0 + condition/lookahead/events
# Default listen: 127.0.0.1:8080 (use --addr 0.0.0.0:8080 for LAN)
./ntnbox serve --profile testdata/profiles/leo_pass_90s.yaml

curl http://localhost:8080/devices/sandbox-0/condition
curl http://localhost:8080/devices/sandbox-0/lookahead
curl http://localhost:8080/devices/sandbox-0/capabilities

# Legacy: API only — register devices yourself
./ntnbox serve --no-device --profile testdata/profiles/leo_pass_90s.yaml
curl -X POST http://localhost:8080/devices \
  -H "Content-Type: application/json" \
  -d '{"id":"ue-1","type":"virtual_ue","profile_name":"leo_pass_90s"}'
```

Adaptation patterns (queue flush, burst gates, lead_sec, store-and-forward):
[COOKBOOK.md](../COOKBOOK.md).

## Endpoints

| Method | Path | Description |
|---|---|---|
| GET | `/health` | Liveness check |
| GET | `/echo` | Returns `{"ts":"..."}` (poller target) |
| GET | `/profiles` | List loaded profiles |
| GET | `/profiles/{name}` | Get a profile's full definition |
| POST | `/devices` | Register a device (`{id, type, profile_name}`) |
| GET | `/devices` | List registered devices |
| GET | `/devices/{id}` | Get a device |
| GET | `/devices/{id}/condition` | Current coverage + link state; with terrestrial fallback also `selected_bearer` + `paths` |
| GET | `/devices/{id}/lookahead` | Next open/close times, window duration, elev (TLE); `?lead_sec=` advisory |
| GET | `/devices/{id}/capabilities` | Satellite capability discovery (`voice` for lband_geo / geo_steady / geo_blockage) |
| POST | `/devices/{id}/call-events` | Voice call telemetry ingest (`{id, status}`: started\|completed\|dropped) — not a full Voice API |
| POST | `/devices/{id}/messages` | Store-and-forward send (`to`: `cloud` or device id) |
| GET | `/devices/{id}/messages` | Delivered inbox (oldest-first); `cloud` is a synthetic recipient |
| GET | `/messages/{mid}` | Message lifecycle status |
| GET | `/sandbox/status` | Current shaping values (Dev Sandbox) |
| GET | `/events` | SSE: coverage, link-state, handover, message, … |
| GET | `/ui/` | Web GUI (satellite pass visualization) |

## Condition (`GET /devices/{id}/condition`)

`in_coverage` is always **satellite** truth (schedule + blockages). Top-level
`delay_ms` / `jitter_ms` / `loss_pct` / `bandwidth_kbps` are satellite
impairments while in coverage (omitted when sat is down).

With `terrestrial_fallback: true` on the profile (Dev Sandbox `run` only),
responses also include the active egress and both path snapshots:

```json
{
  "in_coverage": false,
  "in_blockage": true,
  "elapsed_sec": 12.5,
  "until_next_transition_sec": 18.0,
  "cycle_pos_sec": 12.5,
  "selected_bearer": "terrestrial",
  "paths": {
    "satellite": { "up": false },
    "terrestrial": {
      "up": true,
      "delay_ms": 30,
      "jitter_ms": 5,
      "loss_pct": 0.1,
      "bandwidth_kbps": 10000
    }
  }
}
```

| Field | Meaning |
|-------|---------|
| `in_coverage` | Satellite window open and not blocked |
| `selected_bearer` | `"satellite"` or `"terrestrial"` — which default route is installed |
| `paths.satellite.up` | Same as `in_coverage` |
| `paths.terrestrial.*` | Fixed terrestrial impairments from the profile (always `up` when fallback is on) |

Apps that treat “sat down” as hard constrained should read `selected_bearer`
(or use ntnkit `ntnboxLinkState({ terrestrialFallback: true })`).

## SSE (`GET /events`)

Stream of `text/event-stream` events. Common types: `coverage`, `linkstate`,
`message`, `handover`, `session_info`, `satellite_position` (TLE), `lifecycle`.

### `event: handover`

Emitted when the selected egress changes (dual-path only). Never dropped under
backpressure (unlike high-rate `linkstate`).

```text
event: handover
data: {"from":"satellite","to":"terrestrial","reason":"satellite_blocked","at":"2026-08-04T18:00:00Z","device_id":"sandbox-0"}
```

| Field | Values |
|-------|--------|
| `from` / `to` | `satellite` \| `terrestrial` |
| `reason` | `satellite_coverage_lost`, `satellite_coverage_gained`, `satellite_blocked`, `satellite_unblocked` |
| `device_id` | Device that switched (usually `sandbox-0`) |

Profile wiring and demo: [Profiles — Terrestrial fallback](profiles.md#terrestrial-fallback-dual-egress).
