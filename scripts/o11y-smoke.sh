#!/usr/bin/env bash
# Generic smoke driver for services that aren't Go, or that can't run the
# in-process smoke test. Reads a routes file and calls each route once.
#
# Routes file format, one per line (blank lines and # comments ignored):
#   METHOD  PATH  EXPECTED_STATUS  [HEADER ...]
# Example:
#   GET  /v1/documents/doc-1  200  X-User-Id:usr_1
#   GET  /v1/documents/doc-1  401
set -uo pipefail

APP_URL="${O11Y_APP_URL:-http://127.0.0.1:8080}"
ROUTES="${O11Y_SMOKE_ROUTES:-o11y-smoke.routes}"
READY_PATH="${O11Y_READY_PATH:-/healthz}"

for _ in $(seq 1 60); do
  curl -fs -o /dev/null "${APP_URL}${READY_PATH}" && break
  sleep 1
done

fail=0
while read -r method path want rest; do
  [[ -z "${method:-}" || "$method" == \#* ]] && continue
  headers=()
  for h in $rest; do headers+=(-H "${h/:/: }"); done
  got="$(curl -s -o /dev/null -w '%{http_code}' -X "$method" "${headers[@]}" "${APP_URL}${path}")"
  if [[ "$got" == "$want" ]]; then
    echo "ok    $method $path -> $got"
  else
    echo "FAIL  $method $path -> $got (want $want)"
    fail=1
  fi
done < "$ROUTES"
exit "$fail"
