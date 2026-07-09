# Python Adaptive REST Client

Demonstrates NTN-aware patterns in Python:

- **Latency-based state detection** — tracks latency, classifies link as online/degraded/offline
- **Store-and-forward** — queues messages during coverage gaps
- **Graceful degradation** — reduces polling frequency when link is degraded
- **Auto-flush** — delivers queued messages when connectivity returns

## Run

```bash
# Under NTN conditions:
ntnbox run --profile testdata/profiles/leo_pass_90s.yaml -- python3 samples/python-adaptive/client.py

# With different profile:
ntnbox run --profile testdata/profiles/d2c_burst.yaml -- python3 samples/python-adaptive/client.py
```

## What you'll see

- **Online**: requests succeed, latency ~40ms (satellite overhead)
- **Degraded**: latency >500ms (satellite at horizon), polling slows down
- **Offline**: coverage gap, all requests fail → messages queued
- **Recovery**: connectivity returns → queue flushed automatically

## No dependencies

Uses only Python standard library (`http.client`, `ssl`, `json`, `time`).

## Docker note

The Docker image does not include Python. This sample runs on Linux
natively or requires Python 3 installed on the host. For Docker-based
demos, use the Go samples (`--sample go-messenger` or `--sample curl-demo`).
