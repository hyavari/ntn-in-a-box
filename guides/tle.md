# TLE support

Generate profiles from Two-Line Element orbital data, or drive a live
simulation from predicted passes. In TLE mode, the GUI shows a 3D
globe with the satellite orbiting in real-time, the observer pinned
on the surface, and a coverage beam connecting them during passes.
On macOS, `--tle` is bind-mounted the same way as `--profile`.

<img src="images/tle.png" alt="TLE Globe Visualization" width="800">

Quick start is also summarized in the [README](../README.md). For flags,
custom link models, and the demo script, see below.

```bash
# Offline: generate a YAML profile from ISS orbital data
./ntnbox tle generate \
  --file testdata/tle/iss.tle \
  --lat 37.7749 --lon -122.4194 \
  --output iss-pass.yaml

# Live: predict passes and simulate them in sequence
sudo ./ntnbox run \
  --tle testdata/tle/iss.tle \
  --lat 37.7749 --lon -122.4194 \
  --start-at next-pass --speed 10 \
  -- ./poller

# Works with --tui, --record, and --addr (GUI)
sudo ./ntnbox run --tui --addr :8080 \
  --tle testdata/tle/iss.tle \
  --lat 37.7749 --lon -122.4194 \
  --start-at next-pass \
  -- ./poller
```

## `ntnbox tle generate`

| Flag | Required | Default | Description |
|------|----------|---------|-------------|
| `--file` | Yes | — | Path to TLE file |
| `--lat` | Yes | — | Observer latitude (degrees, north +) |
| `--lon` | Yes | — | Observer longitude (degrees, east +) |
| `--alt` | No | 0 | Observer altitude (km) |
| `--output` | Yes | — | Output YAML file or directory |
| `--passes` | No | 1 | Number of passes to generate |
| `--link-model` | No | built-in | Custom link model YAML |
| `--elev-min` | No | 10 | Minimum elevation (degrees) |
| `--sat` | No | first | Select satellite by name or NORAD ID |
| `--start` | No | now | Prediction start time (RFC3339) |

```bash
# Multiple passes → writes pass_001.yaml, pass_002.yaml, … into a directory
./ntnbox tle generate \
  --file testdata/tle/iss.tle \
  --lat 37.7749 --lon -122.4194 \
  --passes 5 --output passes/
```

## `ntnbox run --tle`

| Flag | Required | Default | Description |
|------|----------|---------|-------------|
| `--tle` | Yes* | — | Path to TLE file (mutually exclusive with `--profile`) |
| `--lat` | Yes | — | Observer latitude |
| `--lon` | Yes | — | Observer longitude |
| `--alt` | No | 0 | Observer altitude (km) |
| `--link-model` | No | built-in | Custom link model YAML |
| `--elev-min` | No | 10 | Minimum elevation (degrees) |
| `--sat` | No | first | Satellite selector |
| `--start-at` | No | `next-pass` | `next-pass` (≈30s before next rise) or RFC3339 |
| `--speed` | No | 1.0 | Gap time acceleration factor |
| `--passes` | No | 10 | Passes to pre-compute |

*`--tle` and `--profile` are mutually exclusive; one is required.

## Custom link models

The default link model maps elevation angle to impairment values based
on published LEO broadband measurements. Override it with a YAML file:

```yaml
name: my_constellation
min_elev_deg: 5
delay_ms:
  - { elev_deg: 5, value: 200 }
  - { elev_deg: 45, value: 30 }
  - { elev_deg: 90, value: 20 }
jitter_ms:
  - { elev_deg: 5, value: 50 }
  - { elev_deg: 45, value: 8 }
  - { elev_deg: 90, value: 3 }
loss_pct:
  - { elev_deg: 5, value: 15 }
  - { elev_deg: 45, value: 0.5 }
  - { elev_deg: 90, value: 0.1 }
bandwidth_kbps:
  - { elev_deg: 5, value: 1000 }
  - { elev_deg: 45, value: 50000 }
  - { elev_deg: 90, value: 100000 }
```

## TLE demo script

```bash
./scripts/demo-tle.sh                          # ISS from San Francisco
./scripts/demo-tle.sh --generate-only          # just generate profiles
./scripts/demo-tle.sh --tui --speed 10         # TUI + gap acceleration
./scripts/demo-tle.sh --lat 51.5074 --lon -0.1278  # London observer
./scripts/demo-tle.sh --tle testdata/tle/starlink-single.tle  # Starlink
```

> **Note:** The TLE globe visualization loads Three.js and an Earth
> texture from jsDelivr CDN. If the network is unavailable, the GUI
> falls back to the flat sky-arc animation automatically.
