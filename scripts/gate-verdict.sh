#!/usr/bin/env bash
# Enforce-mode verdict for a Weaver live-check report (weaver-report.json from
# scripts/gate-local.sh report). Fails if the report has violations, or if
# Weaver saw no telemetry at all, so a smoke test that never reached the gate
# can't pass by accident. Needs jq.
#
#   scripts/gate-verdict.sh weaver-report.json
set -euo pipefail
report="${1:-weaver-report.json}"

entities="$(jq '.statistics.total_entities // 0' "$report")"
violations="$(jq '.statistics.advice_level_counts.violation // 0' "$report")"

if [ "$entities" -eq 0 ]; then
  echo "Weaver received no telemetry. Check that the smoke test exported to the gate." >&2
  exit 1
fi
if [ "$violations" -gt 0 ]; then
  echo "Weaver reported $violations violation(s) across $entities entities." >&2
  jq -r '.statistics.advice_type_counts | to_entries[] | "  \(.key): \(.value)"' "$report" >&2
  echo "docs/03-weaver-gate.md lists each finding id and its fix." >&2
  exit 1
fi
echo "No violations ($entities entities checked)."
