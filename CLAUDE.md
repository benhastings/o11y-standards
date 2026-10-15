# CLAUDE.md — o11y-standards

Read this first. It carries the context behind the repo and what has been
verified so far. docs/00-team-notes.md is the team-facing version of the
decisions, placeholders and open questions.

## What this repo is

The SRE org's telemetry standard and its enforcement, for the authn/z
platform services. A bootstrap for the 80% solution: get teams onto it, then
improve fidelity. See README.md.

Environment:
- **Applications:** Go APIs and React UIs on Kubernetes + Istio, with
  MariaDB/Postgres. The dev teams use **Valkey** as their application cache;
  the `kfo11y.cache.*` attributes describe that cache.
- **Telemetry store: ClickHouse only.** The platform has pivoted away from
  Tempo and VictoriaMetrics. Traces, metrics, and logs (including wide events)
  all export to ClickHouse via the collector's clickhouse exporter; RED metrics
  and SLIs come from SQL over spans and wide events. Don't reintroduce Tempo or
  VictoriaMetrics in docs, config, or examples.
- **No cache in the o11y platform itself.** None is planned. If one ever
  becomes necessary, the preference is **DragonflyDB**; don't add one without
  asking.
- **otelkit replaces o11yflow.** Breaking with o11yflow is fine.
- Keep docs and examples generic: no references to the deployment sector or
  customer, and no real team or service names. Examples use `policy-api`,
  `policy-team`, `us-east-1`.
- **Source lives on GitHub** (`benhastings/o11y-standards`, private). Only the
  Weaver gate CI (`.gitlab-ci.yml`, `ci/weaver.gitlab-ci.yml`) is
  GitLab-specific; it's kept (don't delete it) but doesn't run on GitHub and
  is untested until we pick a way to exercise it. GitHub Actions runs the
  same checks. The gate image (`ci/gate-image/`),
  libraries, docs and `dev/` stack are host-independent.

Repo layout:
- `weaver/` — registry (v1 definitions; `manifest.yaml` with
  `file_format: definition_manifest/2.0`; depends on semconv v1.43.0), Rego
  live-check advice policies (ours + vendored upstream `zz_upstream_otel.rego`),
  policy data, `weaver.toml` (.weaver.toml format)
- `ci/gate-image/` — Dockerfile + `bin/gate-serve`, `bin/gate-self-test`
- `ci/weaver.gitlab-ci.yml` — include-able job; gate image as a CI service
- `.github/workflows/ci.yml` — this repo's CI on GitHub: pin check + zizmor,
  gate image build + self-test + example in enforce mode (via
  `scripts/gate-local.sh` and `scripts/gate-verdict.sh`), otelkit tests,
  otel-web typecheck, `v*` tag pushes the tested image to
  `ghcr.io/benhastings/o11y-standards/weaver-gate`. Actions are SHA-pinned
  (`scripts/check-pins.sh`); Dependabot bumps actions only.
- `.github/workflows/docs.yml` + `zensical.toml` — docs/ as a Zensical site
  (`--strict`, so broken links and anchors fail), deployed to GitHub Pages
  from `main`. Toolchain pinned in `requirements-docs.txt` (Zensical is
  0.0.x, so bump deliberately). `docs/index.md` is the site's landing page.
- `.gitlab-ci.yml` — the GitLab equivalent, kept for later (build image, self-test, template
  vs. example in enforce mode, otelkit tests, otel-web typecheck, release)
- `otelkit/` — Go library + `example/` service + `o11ysmoke` smoke test
- `otel-web/` — TypeScript package for React front ends
- `dev/` — collector + ClickHouse docker compose; `rollups.sql` (metric
  rollups over wide events, see docs/01 "Metric aggregation")
- `docs/` — 00 team notes, 01 metadata standard, 02 wide events,
  03 gate, 04 Go guide, 05 front-end guide

## Decisions already made — ask before changing

- Org prefix is **`kfo11y`** (attributes `kfo11y.*`, events
  `kfo11y.*.summary`, finding ids `kfo11y_*`, Rego data `data.kfo11y`,
  `policy-data/kfo11y.yaml`, Go constants `attrs.Kfo11y*`, TS
  `ATTR_KFO11Y_*`, npm scope `@kfo11y`). Final; don't rename.
- Everything lives on GitHub for now: Go module
  `github.com/benhastings/o11y-standards/otelkit` (tags `otelkit/vX.Y.Z`),
  gate image `ghcr.io/benhastings/o11y-standards/weaver-gate` (tags
  `vX.Y.Z`), GitLab template via `include: remote:` on raw.githubusercontent.
  otel-web is unpublished (GitHub Packages can't host the `@kfo11y` scope).
- Registry pins semconv **v1.43.0** (newest both Go and JS SDKs ship), not 1.44.
- Registry definitions stay in Weaver **v1** format (v2 unstable; weaver#1814).
- Wide events are OTel **log-based events** (LogRecord EventName), not span
  events. The Span Event API is deprecated upstream (OTEP 4430). Doc 02 makes
  that case; keep its framing neutral toward the earlier span-events decision.
- Span-event purge target: three sprints after adoption (date TBD).
- Durations in seconds (`kfo11y.duration`). Gate starts in report mode.
- Weaver is never downloaded per job: it ships in the weaver-gate image and
  runs as a CI service (sidecar in the k8s job pod).
- Browser telemetry auth is tabled; assume otel-web posts to a collector.
- Pins: Weaver **0.27.0** (image digest in Dockerfile/.gitlab-ci.yml), OTel Go
  **v1.47.0**, contrib **v0.72.0**, Go **1.26**, OTel JS 2.12.0 / 0.223.0
  (exact versions in package.json).

## Weaver 0.27.0 facts (verified against source and a real report)

- `manifest.yaml` without `file_format` is assumed v2 and warned about;
  `registry_manifest.yaml` is "legacy". `--future` makes both errors.
  semconv 1.43's own manifest lacks `file_format`; the image patches it.
- live-check only knows semconv definitions the registry imports or refs.
  `weaver/registry/imports.yaml` imports them explicitly (attribute groups
  `registry.*`, plus the entities, metrics and spans we emit); this replaced
  the deprecated `--include-unreferenced` flag, which is no longer used.
  Without the `registry.*` import, deprecated names report as missing, not
  `deprecated`. A plain `*` import fails: some semconv groups (gen_ai, mcp,
  zos) are excluded from dependency resolution. Verified: report identical
  to the flag's (entities, levels, every advice message) on the example.
- The policy step of `registry check` downloads dependencies and ignores
  `[resolve.schema_url_overrides]`; the image rewrites the manifest's
  `registry_path` to `/opt/semconv/model` instead.
- `--advice-policies` replaces the built-in `otel.rego`; the default jq
  preprocessor still runs.
- `--output http`: `POST /stop`, then `GET /report`, then `POST /shutdown` on
  the admin port. Both listeners bind `--otlp-grpc-address` (default
  127.0.0.1; the image uses 0.0.0.0).
- Rego input: `input.sample.span.{name, kind (lowercase), attributes[].name/value,
  span_events}` with **no parent span id**; `input.sample.resource.attributes`;
  `input.sample.log.{event_name, attributes}`; `input.sample.span_event.name`;
  `input.sample.attribute.name`. Findings may use `{id, level, context, message}`.

## Verified

- Registry check `--future` clean; policy self-test passes offline in the image.
- Example service passes the gate end to end (go-test and process modes) via
  the template's own script; break tests fire `deprecated`,
  `kfo11y_sensitive_attribute`, `kfo11y_missing_resource_attribute`,
  `kfo11y_span_event_deprecated`.
- otelkit `go vet` + `go test -race` green; otel-web `tsc --noEmit` green.
- ClickHouse round trip via `dev/`; queries in docs/02 and docs/03 run.
- `dev/rollups.sql` loads (and reloads) cleanly; rollup totals match the raw
  events for request, tenant, job and UI events; backfill via INSERT SELECT
  works; the docs/01 rollup queries run.

## Still unverified

- Anything on real GitLab runners: rootless buildah, service alias
  resolution on the Kubernetes executor, `glab ci lint`.
- otel-web in a real browser.

## Conventions

- Keep attribute names in `otelkit/attrs` and `otel-web/src/attrs.ts` in sync
  with `weaver/registry`; the registry is the source of truth. docs/01's
  wide-event table and metric classes must match it too;
  `scripts/check-docs-registry.py` enforces that in the docs workflow.
- Mermaid for diagrams in markdown.
- Don't add attributes outside the registry; propose registry changes instead.
- Commit after each green step.
