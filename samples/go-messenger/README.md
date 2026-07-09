# Go Satellite Messenger

A client demonstrating NTN-aware messaging patterns in Go. Works
standalone (GETs https://example.com) or with the included local
server (POSTs messages).

## Patterns demonstrated

- Offline message queue with automatic delivery
- Connection state detection (online/offline transitions)
- Per-message latency tracking
- Graceful queue flush on reconnect

## Run

```bash
# Standalone (no server needed — GETs https://example.com):
./scripts/demo.sh --sample go-messenger

# Or directly:
ntnbox run --profile testdata/profiles/leo_pass_90s.yaml \
  -- go run samples/go-messenger/client/main.go

# With local server (Linux only — env vars not forwarded through macOS Docker proxy):
# Terminal 1: start the server
go run samples/go-messenger/server/main.go
# Terminal 2: run client targeting the server
sudo SERVER_URL=http://10.200.0.1:9090/send \
  ./ntnbox run --profile testdata/profiles/leo_pass_90s.yaml -- ./messenger-client
```

## Note on timing

With `leo_pass_90s`, the coverage window is 90s and the gap is 510s.
If the queue fills during a gap, flush won't happen until the next pass
begins (~8.5 minutes later). This is realistic satellite behavior — not
a bug. Use `d2c_burst` for faster cycles during development.

## What you'll see

```
  14:10:01  ✓  msg#1 delivered (142ms)        ← satellite rising
  14:10:04  ✓  msg#2 delivered (43ms)         ← overhead
  14:10:07  ✓  msg#3 delivered (41ms)
  ...
  14:11:30  ▼  connection lost: timeout       ← coverage gap
  14:11:30  ◌  queued msg#30 (queue: 1)
  14:11:33  ◌  queued msg#31 (queue: 2)
  ...
  14:20:00  ▲  connection restored            ← next pass (~8.5min later)
  14:20:00  ⟳  flushing 5 queued messages...
  14:20:01  ✓  msg#30 delivered (148ms)
  14:20:01  ✓  flush complete
```
