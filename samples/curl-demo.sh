#!/bin/sh
# curl-demo.sh — The simplest NTN-in-a-Box demo.
#
# Polls an HTTP endpoint in a loop and prints latency + status.
# Run under ntnbox to see real satellite-like degradation:
#
#   ntnbox run --profile testdata/profiles/leo_pass_90s.yaml -- ./samples/curl-demo.sh
#
# You'll see latency spike at pass start/end, requests timeout during
# coverage gaps, and normal responses during overhead passes.

URL="${1:-https://example.com}"
INTERVAL="${2:-2}"

echo "ntn-curl-demo: polling $URL every ${INTERVAL}s"
echo "              (Ctrl+C to stop)"
echo ""
printf "  %-10s  %-6s  %-10s  %s\n" "TIME" "STATUS" "LATENCY" "RESULT"
echo "  ─────────────────────────────────────────────────"

while true; do
    ts=$(date +%H:%M:%S)
    
    # Capture both HTTP status and time_total.
    result=$(curl -s -o /dev/null -w "%{http_code} %{time_total}" \
        --connect-timeout 5 --max-time 10 "$URL" 2>&1) || true
    
    status=$(echo "$result" | awk '{print $1}')
    latency=$(echo "$result" | awk '{printf "%.0fms", $2 * 1000}')
    
    if [ "$status" = "000" ] || [ -z "$status" ]; then
        printf "  %-10s  \033[31m%-6s\033[0m  %-10s  \033[31m%s\033[0m\n" \
            "$ts" "---" "—" "timeout/unreachable"
    elif echo "$status" | grep -q "^2"; then
        printf "  %-10s  \033[32m%-6s\033[0m  %-10s  \033[32m%s\033[0m\n" \
            "$ts" "$status" "$latency" "ok"
    else
        printf "  %-10s  \033[33m%-6s\033[0m  %-10s  \033[33m%s\033[0m\n" \
            "$ts" "$status" "$latency" "error"
    fi
    
    sleep "$INTERVAL"
done
