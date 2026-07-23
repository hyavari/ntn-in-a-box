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
| GET | `/devices/{id}/condition` | Current coverage + link state |
| GET | `/devices/{id}/lookahead` | Next open/close times, window duration, elev (TLE); `?lead_sec=` advisory |
| GET | `/devices/{id}/capabilities` | Satellite capability discovery |
| POST | `/devices/{id}/messages` | Store-and-forward send (`to`: `cloud` or device id) |
| GET | `/devices/{id}/messages` | Delivered inbox (oldest-first); `cloud` is a synthetic recipient |
| GET | `/messages/{mid}` | Message lifecycle status |
| GET | `/sandbox/status` | Current shaping values (Dev Sandbox) |
| GET | `/events` | SSE: coverage, link-state, message, … |
| GET | `/ui/` | Web GUI (satellite pass visualization) |
