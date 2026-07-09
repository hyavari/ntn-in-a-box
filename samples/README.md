# NTN-in-a-Box Sample Applications

Ready-to-run applications demonstrating how real apps behave under
satellite network conditions. Run any of them under `ntnbox run` to
see NTN effects in action.

## Samples

| Sample | Language | Pattern | macOS Docker |
|--------|----------|---------|--------------|
| [curl-demo.sh](curl-demo.sh) | Shell | Simple polling — see latency and timeouts | Yes (bind-mount) |
| [node-retry/](node-retry/) | Node.js | Exponential backoff + offline queue | No* |
| [python-adaptive/](python-adaptive/) | Python | Latency-based state detection + store-and-forward | No* |
| [go-messenger/](go-messenger/) | Go | Client/server messaging with queue flush | Yes (cross-compiled + bind-mount) |

*Node.js and Python samples require their runtimes installed. The Docker
image only contains `ntnbox` and `poller`. Shell and Go samples work on
macOS because the demo script cross-compiles Go binaries for Linux and
the Darwin proxy bind-mounts local files (prefixed with `./`) into the
container.

## Quick start

```bash
# Via demo script (builds Docker, easiest):
./scripts/demo.sh --sample curl-demo
./scripts/demo.sh --sample go-messenger

# Direct (Linux or custom command):
ntnbox run --profile testdata/profiles/leo_pass_90s.yaml -- ./samples/curl-demo.sh
ntnbox run --profile testdata/profiles/leo_pass_90s.yaml -- node samples/node-retry/index.js
ntnbox run --profile testdata/profiles/leo_pass_90s.yaml -- python3 samples/python-adaptive/client.py

# Go messenger (start server first, then client under ntnbox):
go run samples/go-messenger/server/main.go &
ntnbox run --profile testdata/profiles/leo_pass_90s.yaml -- go run samples/go-messenger/client/main.go
```

## What to observe

All samples show the same NTN patterns:

1. **Pass start** — high latency (satellite at horizon)
2. **Overhead** — low latency, good throughput
3. **Pass end** — latency rises again
4. **Coverage gap** — total timeout, requests fail
5. **Next pass** — connectivity returns, queued messages flush

## Adding your own

Any application that makes network requests can be tested:

```bash
ntnbox run --profile testdata/profiles/leo_pass_90s.yaml -- ./your-app
```

The application doesn't need any code changes — ntnbox shapes the
network transparently at the OS level.
