#!/usr/bin/env bash
# prefix-cache.sh — benchmark prefix caching against BNK AI Gateway VIP.
set -euo pipefail

VIP="${VIP:-10.0.17.130}"
ITERATIONS="${ITERATIONS:-10}"

echo "==> Running Prefix Caching Benchmark against http://${VIP}:80"
echo "==> Sending 1st Request (Cold Cache)"
START=$(date +%s%N)
curl -s -X POST "http://${VIP}:80/v1/chat/completions" \
  -H "Content-Type: application/json" \
  -d '{"model":"llama3","messages":[{"role":"user","content":"Explain Azure Local Edge architecture in detail with diagrams"}],"stream":false}' > /dev/null
END=$(date +%s%N)
COLD_MS=$(( (END - START) / 1000000 ))
echo "Cold Cache Latency: ${COLD_MS} ms"

echo "==> Sending 2nd Request (Warm / Prefix Hit)"
START=$(date +%s%N)
curl -s -X POST "http://${VIP}:80/v1/chat/completions" \
  -H "Content-Type: application/json" \
  -d '{"model":"llama3","messages":[{"role":"user","content":"Explain Azure Local Edge architecture in detail with diagrams and bullet points"}],"stream":false}' > /dev/null
END=$(date +%s%N)
WARM_MS=$(( (END - START) / 1000000 ))
echo "Warm Cache Latency: ${WARM_MS} ms"

if [ "$COLD_MS" -gt 0 ]; then
  SPEEDUP=$(( (COLD_MS - WARM_MS) * 100 / COLD_MS ))
  echo "==> Cache Speedup: ~${SPEEDUP}% TTFT reduction"
fi
