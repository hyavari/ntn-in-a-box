# Profiles

Pass-shape profiles define how a satellite link looks at the service layer:
**periodic** LEO-style windows, **continuous** GEO-style links, and optional
unscheduled **blockages** (tunnels, terrain, tree cover) layered on the
schedule.

## Schedule modes

| Mode | Behavior |
|------|----------|
| `periodic` | Coverage window of `window_sec` repeats every `period_sec` |
| `continuous` | Always in coverage unless a blockage is active |

Blockages are *surprise* drops: they do not get lookahead from orbital
mechanics (`lookahead_sec: 0` in the sample). Apps must detect them
reactively via timeouts — the automotive case where a GEO link is always
in view yet still drops as the vehicle moves.

Sample: [`testdata/profiles/geo_blockage.yaml`](../testdata/profiles/geo_blockage.yaml).
Sources and caveats for curve values: [`testdata/profiles/README.md`](../testdata/profiles/README.md).

## Pass-shape schema

```yaml
name: leo_pass_90s
schedule:
  mode: periodic          # periodic (LEO) or continuous (GEO)
  period_sec: 600         # full cycle length
  window_sec: 90          # coverage window duration
  lookahead_sec: 30       # advance notice before transitions
curves:
  delay_ms:
    - { offset_sec: 0, value: 150 }    # horizon (high delay)
    - { offset_sec: 15, value: 40 }    # overhead (low delay)
    - { offset_sec: 75, value: 40 }
    - { offset_sec: 90, value: 100 }   # setting
  # jitter_ms, loss_pct, bandwidth_kbps follow the same shape
```

## Blockages (unscheduled outages)

Any profile may add a `blockages` list — repeating, unscheduled link drops
layered on the schedule. Unlike a periodic window close, a blockage has
**no lookahead**: a moving vehicle cannot predict a tunnel from orbital
mechanics.

```yaml
name: geo_blockage
schedule:
  mode: continuous
  period_sec: 300
  lookahead_sec: 0        # surprise drops, no advance notice
blockages:
  - { offset_sec: 60, duration_sec: 8 }    # short tunnel
  - { offset_sec: 180, duration_sec: 20 }  # tree-lined ridge
# curves: … (as above)
```

Blockages repeat every `period_sec`, are active on
`[offset_sec, offset_sec + duration_sec)`, must fit within the cycle, and
must be strictly ascending and non-overlapping. Primarily for continuous
profiles, but permitted on any mode.

Try it (macOS or Linux):

```bash
./scripts/demo-blockage.sh          # fast demo (drops within ~10s) + GUI
./scripts/demo-blockage.sh --tui    # live TUI dashboard
./scripts/demo-blockage.sh --real   # the realistic 300s geo_blockage profile
```

## Out-of-coverage behavior

When coverage is lost — a scheduled window close **or** an unscheduled
blockage — the Dev Sandbox sets 100% packet loss: packets silently drop,
mimicking real satellite behavior (the signal disappears without sending
ICMP unreachable or RST). Apps must detect the outage via timeouts.
The TUI/GUI label **BLOCKED** for blockage drops vs **OUT OF COVERAGE**
for scheduled gaps.

## Sample profiles

`leo_pass_90s`, `geo_steady`, `d2c_burst`, `sos_burst`, `sos_hostile`,
`geo_blockage`.
