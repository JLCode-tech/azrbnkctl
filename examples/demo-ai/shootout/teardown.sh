#!/usr/bin/env bash
# teardown.sh — cleans up the shootout resources.
set -euo pipefail

echo "==> Cleaning Shootout Resources"
kubectl delete namespace shootout --ignore-not-found=true
echo "==> Shootout cleaned up OK"
