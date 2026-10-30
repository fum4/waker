#!/usr/bin/env bash
# Run the Go test suite with the race detector and keep its full output.
#
# The `go test -json` stream is teed to a log outside the repo (so a gate's
# "clean tree" check is unaffected) at
#   ${XDG_STATE_HOME:-$HOME/.local/state}/waker-tests/<UTC timestamp>-<short sha>.json
# and a summary is printed: each package ok/FAIL and, on failure, the failing
# tests and the log path. Extra arguments go to `go test` (default: ./...).
# Needs jq.
set -euo pipefail

dir="${XDG_STATE_HOME:-$HOME/.local/state}/waker-tests"
mkdir -p "$dir"
log="$dir/$(date -u +%Y%m%dT%H%M%SZ)-$(git rev-parse --short HEAD 2>/dev/null || echo nogit).json"

[ "$#" -gt 0 ] || set -- ./...
status=0
go test -race -json "$@" >"$log" 2>&1 || status=$?

# Package results; non-JSON lines (build errors) are passed through as-is.
jq -rR 'fromjson? // ("  " + .) | if type == "string" then .
  elif .Test == null and (.Action == "pass" or .Action == "fail" or .Action == "skip")
  then "\(if .Action == "fail" then "FAIL" else .Action end)\t\(.Package)\t\(.Elapsed // "")s"
  else empty end' "$log"

if [ "$status" -ne 0 ]; then
  echo
  echo "Failing tests (and build errors):"
  jq -rR 'fromjson? | select(.Action == "fail" and .Test != null) | "  \(.Package) \(.Test)"' "$log"
  jq -jR 'fromjson? | select(.Action == "build-output") | "  " + .Output' "$log"
  echo
  echo "go test failed (exit $status); full log: $log"
  exit "$status"
fi
echo "full log: $log"
