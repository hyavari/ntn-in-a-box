# Field-data report

Write a JSON summary when an `ntnbox run` session ends:

```bash
ntnbox run --report out.json --profile testdata/profiles/geo_blockage.yaml -- ./poller

# or via demos
./scripts/demo.sh --report out.json
./scripts/demo-blockage.sh --report out.json
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
