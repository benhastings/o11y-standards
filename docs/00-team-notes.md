# 00 — Team Notes: Decisions, Placeholders, Open Questions

**Status:** Bootstrap · **Owner:** SRE

This repo is a starting point for the 80% solution: a telemetry standard,
the libraries that follow it, and a CI gate that checks it. This page lists
what's decided, what's still a placeholder, and what's open, so the team can
pick it up.

## Current goals

1. Go APIs on **otelkit**: one wide event per request/job, correct span
   naming, OTLP to the collector. otelkit **replaces o11yflow**; services
   move over and o11yflow is retired.
2. **Purge span events** and switch to log-based wide events
   ([02](02-logging-standard-wide-events.md)).
3. **Gate every service** with the Weaver live-check job, report mode first,
   then enforce ([03](03-weaver-gate.md)).
4. React UIs on **otel-web**, good enough to get traces and interaction
   events flowing ([05](05-frontend-guide.md)). Fidelity improves later.
5. Everything lands in **ClickHouse**; RED metrics and SLIs are SQL over
   wide events.

## Decided

| Topic | Decision |
|---|---|
| Org prefix | `kfo11y` for attributes, events, finding ids, Rego data, Go/TS constants, npm scope. Final. |
| Telemetry store | ClickHouse only, via the collector's `clickhouse` exporter. |
| Wide events | OTel log-based events (LogRecord `EventName`), not span events. |
| Weaver in CI | Baked into the **weaver-gate image** (built and versioned by this repo), run as a CI **service**, which is a sidecar in the job pod on the Kubernetes executor. No per-job downloads. |
| Pins | Weaver 0.27.0 · semconv 1.43.0 · OTel Go v1.47.0 / contrib v0.72.0 · Go 1.26 · OTel JS 2.12.0 / 0.223.0 |
| Gate rollout | Starts in report mode; each service flips to enforce once clean. |

## Placeholders to replace

| Placeholder | Where | Replace with |
|---|---|---|
| `gitlab.example.com` | `otelkit/go.mod` and every Go import, `wide` scope name, `weaver/registry/manifest.yaml` `schema_url`, docs/04 | Real GitLab host (Go module path) |
| `registry.gitlab.example.com/sre/o11y-standards/weaver-gate` | `ci/weaver.gitlab-ci.yml` (`O11Y_GATE_IMAGE`), `scripts/gate-local.sh` | The project's container registry path |
| `sre/o11y-standards` | Include snippets in docs/03 and the template header | Real project path |
| `@kfo11y/otel-web` registry | `otel-web/package.json` | Where the npm package is published (GitLab package registry?) |

Changing the Go module path is a search-and-replace across `otelkit/` and then
`go mod tidy`.

## Network assumptions

We believe runners can reach GitHub, but possibly only mirrored container
images. The design keeps internet access to one place:

- **Only `gate-image:build` in this repo** fetches anything external: the
  Weaver base image and the semconv release archive (checksum-verified). App
  pipelines pull only the gate image from our own registry.
- **Images to mirror** if Docker Hub/Quay aren't reachable: `otel/weaver`,
  `quay.io/buildah/stable`, `golang`, `node`, and for `dev/`,
  `clickhouse/clickhouse-server` and `otel/opentelemetry-collector-contrib`.
  Every one is a variable in `.gitlab-ci.yml` or a pinned tag in
  `dev/docker-compose.yaml`.
- **If GitHub is blocked for the build:** mirror the semconv archive and set
  `SEMCONV_URL`. The checksum still applies.
- **Go modules and npm** (`otelkit:test`, `otel-web:typecheck`, app builds)
  need `GOPROXY` / an npm registry mirror if the public ones aren't reachable.

## Open questions

| Question | Current assumption | Who decides |
|---|---|---|
| **Span-event cutover date** | Three sprints after adoption, then `span_event_level` → `violation` and wide events only. Date TBD. | SRE lead with tech leads |
| **Browser telemetry delivery and auth** | Tabled. otel-web posts OTLP/HTTP to a collector (`/otlp` behind the UI's ingress, or `dev/` locally). We may need a small ingest service in front of the collector for auth, rate limits and size caps. | SRE + platform |
| **Non-standard environment names** | `deployment.environment.name` allows `production`, `staging`, `test`, `development`, `ci`. Add any others your clusters use to `weaver/policy-data/kfo11y.yaml`. | Each team, via MR |
| **Telemetry retention** | Raw spans and wide events keep the exporter's short TTL; the metric rollups in `dev/rollups.sql` keep 13 months. Both numbers are placeholders. | SRE + platform |
| **Runner capabilities** | The Kubernetes executor resolves service aliases (`weaver`) to localhost, and rootless buildah (`vfs`, `chroot`) works on our runners. | Confirm on first pipeline |

## To confirm on the first real pipeline

Everything below was run locally against the real tools, but not yet on
GitLab runners:

1. `gate-image:build` with rootless buildah. If our runners don't allow it,
   swap in whatever the platform supports (a kaniko fork, a privileged
   docker-in-docker runner) with the same Dockerfile.
2. The job container can reach the sidecar at `http://weaver:4320/health`.
3. `o11y:weaver-live-check` in this repo passes in enforce mode against the
   example service.
4. Lint both files with `glab ci lint` or the pipeline editor.

## Follow-ups (backlog)

- **Registry imports.** Live-check needs `--include-unreferenced`, which
  Weaver 0.27 warns is deprecated. Replace it with explicit `imports` of the
  semconv attributes and groups we use, before Weaver drops the flag.
- **Codegen** `otelkit/attrs` and `otel-web/src/attrs.ts` from the registry
  with Weaver templates, instead of hand-maintaining them.
- **otel-web fidelity:** per-interaction attribution of API calls (instead of
  the global stack), native `async`/`await` context, web vitals once semconv
  moves past 1.43.
- **Front-end gate:** a collector in the CI job to bridge OTLP/HTTP to
  Weaver's gRPC listener, with Playwright driving the UI.
- **Continuous live-check in staging**, findings exported to ClickHouse.
- **Rollups on the platform ClickHouse.** Apply `dev/rollups.sql` (with the
  agreed TTLs) and backfill from the raw events still in retention.
- **Drop `server.address` / `server.port` from otelkit's HTTP metrics.**
  otelhttp adds them, but semconv makes them opt-in for metrics because they
  come from the Host header. A metric view in otelkit, or the collector, can
  remove them.
- **UI event gaps in the registry.** `kfo11y.ui.interaction.summary` doesn't
  reference `kfo11y.user.team.id`, so front-end tenant impact can't be
  queried, or `kfo11y.wide_event.truncated`, which otel-web already sets.
  Propose adding both.
- **Semconv 1.44** once both SDKs ship constants for it.
- **o11yflow retirement:** list the services on it, migrate each to otelkit,
  archive the repo.
