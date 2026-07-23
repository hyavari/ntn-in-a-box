# Getting started

1. Build or pull — see [README Quick start](../README.md#quick-start).
2. Run `./scripts/demo.sh --tui` (macOS/Docker) or
   `sudo ./ntnbox run --profile testdata/profiles/leo_pass_90s.yaml -- ./poller` (Linux).
3. Open `http://localhost:8080/ui` when using the demo script (or `--addr :8080`).
4. Try a sample below, or drop the GitHub Action into CI.

For YAML schedules and blockages, see [Profiles](profiles.md).
For orbital TLE, see [TLE](tle.md).

## Demo script

A convenience script that builds everything, runs a demo, and cleans up:

```bash
./scripts/demo.sh                              # default: LEO pass + poller
./scripts/demo.sh --tui                        # with live TUI dashboard
./scripts/demo.sh --sample curl-demo           # curl polling demo
./scripts/demo.sh --sample go-messenger        # Go messenger with queue/flush
./scripts/demo.sh --tui --sample go-messenger  # TUI + sample
./scripts/demo.sh --record session.jsonl       # record bus events to file
./scripts/demo.sh --replay session.jsonl       # replay a recorded session
./scripts/demo.sh --tui --replay session.jsonl # replay with TUI dashboard
./scripts/demo.sh geo_steady                   # different profile
./scripts/demo.sh d2c_burst curl https://example.com  # custom command
./scripts/demo.sh sos_burst                    # emergency/SOS short burst
PRUNE=1 ./scripts/demo.sh                     # also remove docker image on exit
```

The GUI is always available at `http://localhost:8080/ui` when using
the demo script.

Continuous GEO with surprise drops: `./scripts/demo-blockage.sh`
([Profiles](profiles.md)).

## TUI dashboard

<img src="images/tui.png" alt="TUI Dashboard" width="800">

Add `--tui` to get a live terminal dashboard instead of scrolling logs:

```bash
sudo ./ntnbox run --tui --profile testdata/profiles/leo_pass_90s.yaml -- ./poller
```

The dashboard shows:
- **Left panel:** coverage status (▲/▼), colored progress bar, link
  metrics with sparklines, profile info
- **Right panel:** scrollable output from the wrapped command, with
  coverage transition markers injected inline

Keyboard controls:
- `q` / `Ctrl+C` — quit
- `↑`/`↓`/`PgUp`/`PgDn` — scroll output
- `f` — toggle follow mode (auto-scroll)
- `Tab` — toggle expanded output view
- `r` — replay again (shown after replay completes)

The TUI auto-degrades to a stacked layout on terminals narrower than
100 columns. Without `--tui`, output behaves exactly as before
(scrolling logs, suitable for CI/piping).

Status labels: **BLOCKED** for blockage drops vs **OUT OF COVERAGE** for
scheduled gaps ([Profiles](profiles.md)).

## GUI visualization

<img src="images/gui.png" alt="GUI Visualization" width="800">

When running with `--addr`, a web-based GUI is available that shows a
live satellite pass animation:

```bash
sudo ./ntnbox run --addr :8080 --profile testdata/profiles/leo_pass_90s.yaml -- ./poller

# Open in browser:
# http://localhost:8080/ui
```

The GUI shows:
- **Left panel:** animated satellite moving along an orbit arc, coverage
  beam connecting to a ground device, sky darkening on coverage loss
- **Right panel:** live link metrics with sparklines, coverage timeline,
  window progress bar, and profile details (name, mode, schedule)

Features:
- Real-time updates via Server-Sent Events (no polling)
- Idle overlay when no session is active
- Responsive: stacks on narrow screens, hides animation on very narrow
- Works alongside `--tui` — open the GUI in a browser while TUI runs
  in the terminal

The GUI is embedded in the binary — no separate server or files needed.

## Sample applications

Ready-to-run examples in multiple languages showing how apps behave
under NTN conditions:

| Sample | Language | Pattern |
|--------|----------|---------|
| `samples/curl-demo.sh` | Shell | Simple polling — see latency and timeouts |
| `samples/node-retry/` | Node.js | Exponential backoff + offline queue |
| `samples/python-adaptive/` | Python | Latency-based state detection + store-and-forward |
| `samples/go-messenger/` | Go | Client/server messaging with queue flush |
| `samples/android-connectivity/` | Android | Retry + offline queue (emulator / Mobile DX) |

```bash
# Via demo script (builds Docker, easiest on macOS):
./scripts/demo.sh --sample curl-demo
./scripts/demo.sh --sample go-messenger

# Direct (Linux native):
ntnbox run --profile testdata/profiles/leo_pass_90s.yaml -- ./samples/curl-demo.sh
ntnbox run --profile testdata/profiles/leo_pass_90s.yaml -- node samples/node-retry/index.js
ntnbox run --profile testdata/profiles/leo_pass_90s.yaml -- python3 samples/python-adaptive/client.py

# Android Mobile DX helper (prints commands; see Tutorial Step 11):
./scripts/demo-android.sh sos_burst
```

Published images (linux/amd64 + linux/arm64) live at
[`ghcr.io/hyavari/ntn-in-a-box`](https://github.com/hyavari/ntn-in-a-box/pkgs/container/ntn-in-a-box)
(`:latest` and `:vX.Y.Z` on each release). The package is **public** — anonymous
`docker pull` works without `docker login`. The image includes ntnbox, poller,
curl, and **Node.js 24 + pnpm** so macOS developers can run JS/TS apps under
`ntnbox run` (host Darwin Node binaries cannot execute inside the Linux
container). The Darwin proxy bind-mounts referenced project paths and overlays
a Linux `node_modules` volume for JS projects. Python samples still need a
Linux host runtime today. Shell and Go samples work on macOS via the Docker
proxy (cross-compiled / bind-mounted automatically).

Release binaries (no Docker): `ntnbox-linux-amd64`, `ntnbox-linux-arm64`,
and `ntnbox-darwin-arm64` on the
[GitHub Releases](https://github.com/hyavari/ntn-in-a-box/releases) page.

No code changes needed in your app — ntnbox shapes the network
transparently at the OS level. See [TUTORIAL.md](../TUTORIAL.md) for a
step-by-step walkthrough.

### Mobile / Android

For emulator workflows, see [TUTORIAL.md — Step 11](../TUTORIAL.md#step-11-test-with-an-android-emulator),
`./scripts/demo-android.sh`, `samples/android-connectivity/`, and
[`android/ntnbox/`](../android/ntnbox/). Full emulator shaping needs Linux, WSL2,
or CI (macOS Docker does not shape a host AVD).

## GitHub Action

Run your CI tests under simulated NTN conditions with a single step:

```yaml
- uses: hyavari/ntn-in-a-box@v1
  with:
    profile: leo_pass_90s
    command: npm test
```

Requires `ubuntu-latest` (or any Linux runner with `sudo` and `ip`).
The action builds `ntnbox` from source (needs Go) unless you set
`ntnbox-version` to a release tag — then it downloads the matching
linux/amd64 or linux/arm64 binary (checksum verified). Your job's
toolchain (Node, Go, Python, etc.) is available to the command since it
runs directly on the host.

### Inputs

| Input | Required | Default | Description |
|-------|----------|---------|-------------|
| `profile` | Yes* | — | Profile name (`leo_pass_90s`, `geo_steady`, `d2c_burst`, `sos_burst`, `sos_hostile`, `geo_blockage`) or path to YAML |
| `command` | Yes | — | Command to run under NTN conditions |
| `replay` | No* | — | Path to a JSONL recording (overrides `profile`) |
| `speed` | No | `1` | Replay speed multiplier |
| `record` | No | — | Path to save a recording of this session |
| `ntnbox-version` | No | — | GitHub release tag to download (arch-specific asset); if unset, build from source |

*Either `profile` or `replay` is required.

### Examples

```yaml
jobs:
  ntn-test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v6
      - uses: actions/setup-node@v6
        with:
          node-version: '24'
      - run: npm ci

      # Run tests under a simulated LEO pass:
      - uses: hyavari/ntn-in-a-box@v1
        with:
          profile: leo_pass_90s
          command: npm test

      # Replay a recorded session at 10x speed:
      - uses: hyavari/ntn-in-a-box@v1
        with:
          replay: testdata/sessions/regression.jsonl
          speed: '10'
          command: npm test

      # Record a session for later replay:
      - uses: hyavari/ntn-in-a-box@v1
        with:
          profile: d2c_burst
          record: testdata/sessions/new.jsonl
          command: ./my-app --smoke-test
```

The action exit code matches the wrapped command — your CI sees test
pass/fail directly.
