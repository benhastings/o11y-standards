#!/usr/bin/env bash
# Run the Weaver gate on your machine, the same image CI uses.
#
#   scripts/gate-local.sh start     # Weaver listening on :4317 (OTLP/gRPC), admin :4320
#   ...run your smoke test or click through your service...
#   scripts/gate-local.sh report    # writes weaver-report.json, prints counts, stops Weaver
#
# GATE_IMAGE defaults to the released image; use weaver-gate:dev after
#   docker build -f ci/gate-image/Dockerfile -t weaver-gate:dev .
set -euo pipefail
image="${GATE_IMAGE:-registry.gitlab.example.com/sre/o11y-standards/weaver-gate:v0.1.0}"
name=kfo11y-gate
admin=http://127.0.0.1:4320

case "${1:-}" in
  start)
    docker rm -f "$name" >/dev/null 2>&1 || true
    docker run -d --name "$name" -p 4317:4317 -p 4320:4320 "$image" >/dev/null
    ready=""
    for _ in $(seq 1 30); do curl -fs -o /dev/null "$admin/health" && { ready=1; break; }; sleep 1; done
    if [ -z "$ready" ]; then
      echo "Weaver gate never became healthy at $admin" >&2
      docker logs "$name" >&2 || true
      exit 1
    fi
    cat <<MSG
Weaver gate is up. Point your service at it:
  export OTEL_EXPORTER_OTLP_ENDPOINT=http://127.0.0.1:4317 OTEL_EXPORTER_OTLP_PROTOCOL=grpc
  export DEPLOYMENT_ENVIRONMENT=development SERVICE_TEAM=<team> SERVICE_NAMESPACE=<area> SERVICE_VERSION=0.0.0-dev
Then: $0 report
MSG
    ;;
  report)
    curl -fsS -X POST "$admin/stop" >/dev/null
    curl -fsS "$admin/report" -o weaver-report.json
    curl -fsS -X POST "$admin/shutdown" >/dev/null || true
    docker rm -f "$name" >/dev/null 2>&1 || true
    sed -n '/"advice_level_counts"/,/}/p;/"advice_type_counts"/,/}/p' weaver-report.json
    echo "Full report: weaver-report.json"
    ;;
  *)
    echo "usage: $0 start|report" >&2; exit 2 ;;
esac
