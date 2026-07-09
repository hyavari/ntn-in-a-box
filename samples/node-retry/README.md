# Node.js Resilient HTTP Client

Demonstrates NTN-aware patterns in Node.js:

- **Exponential backoff** on request failures
- **Offline queue** — buffers messages during coverage gaps
- **Auto-flush** — delivers queued messages when connectivity returns
- **Adaptive timeout** — increases timeout after failures, decreases after success

## Run

```bash
# Under NTN conditions:
ntnbox run --profile testdata/profiles/leo_pass_90s.yaml -- node samples/node-retry/index.js

# With custom target:
TARGET_URL=http://your-api.com ntnbox run --profile ... -- node samples/node-retry/index.js
```

## What you'll see

- During coverage: requests succeed with varying latency (40-150ms)
- At pass edges: latency spikes, some retries needed
- During coverage gap: all requests fail → messages queued
- When coverage returns: queue flushes automatically

## No dependencies

Uses only Node.js built-in `http`/`https` modules. No `npm install` needed.

## Docker note

The Docker image does not include Node.js. This sample runs on Linux
natively or requires Node.js installed on the host. For Docker-based
demos, use the Go samples (`--sample go-messenger` or `--sample curl-demo`).
