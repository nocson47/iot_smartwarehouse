#!/usr/bin/env bash
set -e

BASE=http://localhost:5000

echo "Waiting for server..."
for i in {1..20}; do
  if curl -s ${BASE}/api/readings >/dev/null 2>&1; then
    echo "Server reachable"; break
  fi
  sleep 1
done

echo "Publishing test message via /api/publish"
curl -s -X POST -H 'Content-Type: application/json' -d '{"topic":"sensors/test-device","payload":"{\"device\":\"test-device\",\"temp\":22.5,\"hum\":55.0}"}' ${BASE}/api/publish

sleep 2
echo "Checking readings"
OUT=$(curl -s ${BASE}/api/readings?limit=10)
echo "$OUT" | grep -q test-device && echo "OK: reading found" || (echo "FAIL: reading not found"; exit 2)
