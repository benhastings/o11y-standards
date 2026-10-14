#!/usr/bin/env bash
# Refreshes the vendored upstream OTel advice policy after a Weaver upgrade.
# Passing --advice-policies replaces Weaver's built-in policy instead of adding
# to it, so the gate carries a copy next to the kfo11y rules.
#
#   WEAVER_VERSION=0.28.0 scripts/update-upstream-policy.sh
set -euo pipefail
: "${WEAVER_VERSION:?set WEAVER_VERSION, e.g. 0.27.0}"
dest="$(dirname "$0")/../weaver/policies/live_check_advice/zz_upstream_otel.rego"
url="https://raw.githubusercontent.com/open-telemetry/weaver/v${WEAVER_VERSION}/defaults/policies/live_check_advice/otel.rego"
tmp="$(mktemp)"
curl -fsSL "$url" -o "$tmp"
{
  printf '# Vendored from open-telemetry/weaver v%s,\n' "$WEAVER_VERSION"
  printf '# defaults/policies/live_check_advice/otel.rego (Apache-2.0). Do not edit.\n#\n'
  printf "# Passing --advice-policies replaces Weaver's built-in policy instead of adding\n"
  printf '# to it, so a copy lives next to ours. Refresh it with\n'
  printf '# scripts/update-upstream-policy.sh when WEAVER_VERSION changes.\n\n'
  cat "$tmp"
} > "$dest"
rm -f "$tmp"
echo "updated $dest"
