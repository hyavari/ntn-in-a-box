"""
NTN-in-a-Box Sample: Adaptive REST Client (Python)

Demonstrates patterns for satellite-aware applications:
- Latency-based state detection (online/degraded/offline)
- Store-and-forward (queue during outage, sync on reconnect)
- Graceful degradation (reduce sync frequency under constraint)
- Connection state tracking

Run under ntnbox to see real NTN behavior:
    ntnbox run --profile testdata/profiles/leo_pass_90s.yaml -- python3 samples/python-adaptive/client.py
"""

import http.client
import ssl
import json
import time
import sys
from collections import deque
from urllib.parse import urlparse

# --- Configuration ---
TARGET_URL = "https://example.com"
POLL_INTERVAL = 2.0  # seconds
TIMEOUT = 5.0  # seconds
MAX_QUEUE = 50
DEGRADED_THRESHOLD_MS = 500  # latency above this = degraded

# --- State ---
class State:
    ONLINE = "online"
    DEGRADED = "degraded"
    OFFLINE = "offline"

state = State.ONLINE
queue = deque(maxlen=MAX_QUEUE)
msg_id = 0
stats = {"sent": 0, "failed": 0, "queued": 0, "flushed": 0}
latency_history = deque(maxlen=10)


# --- Helpers ---

def colored(color, text):
    colors = {"green": "\033[32m", "red": "\033[31m", "yellow": "\033[33m", "cyan": "\033[36m", "dim": "\033[2m", "reset": "\033[0m"}
    return f"{colors.get(color, '')}{text}{colors['reset']}"

def log(color, symbol, msg):
    ts = time.strftime("%H:%M:%S")
    print(f"  {colored('dim', ts)}  {colored(color, symbol)}  {msg}")

def http_get(url, timeout_sec):
    """Simple HTTP/HTTPS GET returning (status, latency_ms) or raising on failure."""
    parsed = urlparse(url)
    start = time.time()
    if parsed.scheme == "https":
        ctx = ssl.create_default_context()
        conn = http.client.HTTPSConnection(parsed.hostname, parsed.port or 443, timeout=timeout_sec, context=ctx)
    else:
        conn = http.client.HTTPConnection(parsed.hostname, parsed.port or 80, timeout=timeout_sec)
    try:
        conn.request("GET", parsed.path or "/")
        resp = conn.getresponse()
        resp.read()
        latency_ms = (time.time() - start) * 1000
        return resp.status, latency_ms
    finally:
        conn.close()


# --- Core Logic ---

def detect_state(latency_ms):
    """Update connection state based on observed latency."""
    global state
    latency_history.append(latency_ms)
    avg = sum(latency_history) / len(latency_history)
    
    if avg > DEGRADED_THRESHOLD_MS:
        if state != State.DEGRADED:
            log("yellow", "⚠", f"link degraded (avg latency: {avg:.0f}ms)")
        state = State.DEGRADED
    else:
        if state != State.ONLINE:
            log("green", "▲", f"link healthy (avg latency: {avg:.0f}ms)")
        state = State.ONLINE

def send_message(msg):
    """Attempt to send a message. Returns True on success."""
    global state
    try:
        status, latency_ms = http_get(TARGET_URL, TIMEOUT)
        if 200 <= status < 300:
            stats["sent"] += 1
            detect_state(latency_ms)
            log("green", "✓", f"msg#{msg['id']} sent ({latency_ms:.0f}ms) [{state}]")
            return True
        else:
            stats["failed"] += 1
            log("yellow", "✗", f"msg#{msg['id']} HTTP {status}")
            return False
    except Exception as e:
        stats["failed"] += 1
        if state != State.OFFLINE:
            log("red", "▼", f"connection lost: {e}")
            state = State.OFFLINE
        return False

def flush_queue():
    """Try to deliver all queued messages."""
    if not queue:
        return
    log("cyan", "⟳", f"flushing {len(queue)} queued messages...")
    delivered = 0
    while queue:
        msg = queue[0]
        if send_message(msg):
            queue.popleft()
            delivered += 1
            stats["flushed"] += 1
        else:
            log("red", "✗", f"flush stalled, {len(queue)} remaining")
            return
    log("green", "✓", f"flush complete ({delivered} delivered)")

def enqueue(msg):
    """Add a message to the offline queue."""
    queue.append(msg)
    stats["queued"] += 1
    log("red", "◌", f"queued msg#{msg['id']} (queue: {len(queue)})")


# --- Main Loop ---

def main():
    global msg_id
    print(f"\n  ntn-adaptive-client: syncing with {TARGET_URL} every {POLL_INTERVAL}s")
    print(f"  Demonstrates: latency-based state detection, store-and-forward, graceful degradation\n")

    while True:
        msg_id += 1
        msg = {"id": msg_id, "ts": time.time(), "payload": f"data-{msg_id}"}

        ok = send_message(msg)
        if not ok:
            enqueue(msg)
        elif queue:
            # Connectivity returned — flush.
            flush_queue()

        # Adaptive polling: slow down when degraded.
        interval = POLL_INTERVAL
        if state == State.DEGRADED:
            interval = POLL_INTERVAL * 2
            if msg_id % 5 == 0:
                log("dim", "│", f"degraded mode: polling at {interval}s interval")
        elif state == State.OFFLINE:
            interval = POLL_INTERVAL * 3

        # Stats every 10 messages.
        if msg_id % 10 == 0:
            log("dim", "│", f"stats: sent={stats['sent']} failed={stats['failed']} queued={stats['queued']} flushed={stats['flushed']} pending={len(queue)}")

        time.sleep(interval)


if __name__ == "__main__":
    try:
        main()
    except KeyboardInterrupt:
        print(f"\n  stopped. final stats: {json.dumps(stats)}")
        sys.exit(0)
