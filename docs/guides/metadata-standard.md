# Telemetry Metadata Standard

!!! note "Standalone guide"
    General guidance for any service team, independent of this repository's
    tooling. It uses `myorg.` as a placeholder for your org prefix; this
    repository's own standard uses `kfo11y.` (see [01](../01-telemetry-standard.md)).

## Why metadata matters

Every span, metric, log and event your service emits should answer three questions without anyone opening the code: **who** sent it, **what** happened, and **where** it ran. Metadata is what makes that possible. Consistent names let one query, dashboard or alert work across every service; inconsistent names make each service a special case.

This standard tells you which attributes to set, what to call your own, and what to keep out of telemetry. It builds on the [OpenTelemetry semantic conventions](https://opentelemetry.io/docs/specs/semconv/) and adds only what they don't cover.

Metadata lives in three places, and each has its own job:

|  | Resource | Wide event | Span |
| --- | --- | --- | --- |
| Describes | The process: who and where | One unit of work: a request, job or UI interaction | One operation's timing and place in a trace |
| Emitted as | Attached to every signal the process sends | An OpenTelemetry log record with an event name | A span in a trace |
| Sent | With every signal | Once, when the work finishes | Once per operation |
| Kept | With its signal | Every event | Every span, with the trace |
| Correlation | — | Carries the trace and span ID of the active span | Is the trace |
| Feeds metrics and SLIs | Bounded attributes become dimensions | Yes: it's the record of what happened | No |

Don't copy resource attributes onto spans or events. They're already attached to everything the process sends.

## Principles

1. **Use OpenTelemetry names first.** If the semantic conventions define an attribute, use that name, type and unit. Pin a semconv version so everyone means the same thing.
2. **Put everything else under one org prefix.** Your own attributes live under a single namespace (shown here as `myorg.`) so they can never collide with a future OpenTelemetry name.
3. **Names are low-cardinality; values can be high-cardinality.** Span names, event names and routes are templates (`GET /v1/documents/{id}`). User IDs and tenant IDs go in attribute values, never in names.
4. **Metadata comes from automation, not hand-typed code.** Identity comes from CI and the deployment; Kubernetes and cloud metadata come from the collector. Your code adds what only the request knows: who called, what they asked for, what was decided, what it cost. Add plenty of it.
5. **Anything a metric, SLI or audit depends on goes on the wide event.** Wide events are log records, one per unit of work, and are the record of what happened. Spans carry timing and trace shape.
6. **Durations are in seconds**, as a double. Match the OpenTelemetry unit, not milliseconds.

## Required resource attributes

Set these once per process. The SDK attaches them to every span, metric and log record.

| Attribute | Required | Where it comes from | Example |
| --- | --- | --- | --- |
| `service.name` | Yes | `OTEL_SERVICE_NAME` | `search-api` |
| `service.namespace` | Yes | Deploy manifest | `search` |
| `service.version` | Yes | Injected by CI | `1.4.32` |
| `service.instance.id` | Yes | Pod UID via the downward API; a UUID otherwise | `7d3c…` |
| `deployment.environment.name` | Yes | Set explicitly per environment | `production` |
| `myorg.service.team` | Yes | Deploy manifest or pod label | `search-team` |
| `vcs.ref.head.revision` | Yes | Build info or `GIT_COMMIT_SHA` from CI | `8f7a2b3c` |
| `service.criticality` | Recommended | Deploy manifest | `critical` |
| `k8s.cluster.name`, `k8s.namespace.name`, `k8s.deployment.name`, `k8s.pod.name`, `k8s.node.name` | Yes | Collector (`k8sattributes`) | — |
| `cloud.provider`, `cloud.region`, `cloud.availability_zone` | Yes | Collector (`resourcedetection`) | `aws`, `us-east-1` |

Rules that trip teams up:

- **`service.namespace` is the product area**, not the Kubernetes namespace, and never carries the environment. `search-dev` mixes two things; split it into `service.namespace=search` and `deployment.environment.name=development`.
- **`deployment.environment.name`** uses the well-known values `production`, `staging`, `test` and `development` wherever they fit. Set it explicitly; don't derive it from the Kubernetes namespace.
- **Ownership is not a `service.*` attribute.** That namespace belongs to OpenTelemetry, so the team goes under your org prefix.
- **Don't set Kubernetes or cloud attributes in code.** The collector adds them consistently for everyone.

A typical deployment:

```yaml
env:
  - name: OTEL_SERVICE_NAME
    value: "search-api"
  - name: OTEL_RESOURCE_ATTRIBUTES
    value: "service.namespace=search,myorg.service.team=search-team,deployment.environment.name=production"
  - name: SERVICE_VERSION
    value: "1.4.32"   # injected by CI
  - name: POD_UID
    valueFrom:
      fieldRef:
        fieldPath: metadata.uid
```

## Context your code adds

Resource attributes say who and where. The attributes your code adds say **what happened**, and that's where most answers in an investigation come from. Set them generously on every unit of work, on its wide event.

| Category | Attributes | Example | Role in metrics |
| --- | --- | --- | --- |
| HTTP and execution | `http.request.method`, `http.route`, `http.response.status_code`, `myorg.duration` (s) | `POST`, `/v1/indexes/{index}/search`, `200`, `0.1425` | Method, route and status are dimensions; duration is a measure |
| Customer context | `user.id`, `myorg.user.team.id` (tenant), `myorg.user.tier`, `session.id` | `usr_98123`, `enterprise_corp_a`, `enterprise` | Tenant and tier are dimensions; user and session are drill-down |
| Work metrics | `myorg.cache.hit`, `myorg.cache.retry.count` (DB calls: use the semconv `db.client.operation.duration` metric) | `false`, `0` | Measures |
| Search path and features | `myorg.search.index.name`, `myorg.search.served_from` (`cache` / `index` / `db`), `myorg.feature_flags.enabled` | `products`, `index`, `["new-ranker"]` | Index and served-from tier are dimensions; flags are drill-down |
| Diagnostics | `myorg.outcome`, `error.type` | `upstream_error`, `GatewayTimeout` | Dimensions |

With the resource attributes above, that's more than 20 fields on a typical request, and every one is a question you can answer without shipping code.

**Not sure whether to add something? Add it.** High cardinality isn't a reason to leave a value out. It only means the value is for drill-down, not a metric dimension (see Cardinality). Define new attributes in the registry under your org prefix so every service names them the same way.

## Naming your own attributes

Add an attribute only after checking the [OpenTelemetry attribute registry](https://opentelemetry.io/docs/specs/semconv/registry/attributes/). If it's there, use it as written.

When it isn't:

- **Prefix:** `myorg.` followed by the domain, e.g. `myorg.search.index.name`, `myorg.cache.hit`.
- **Case:** lowercase `snake_case` within a segment, dots between segments: `myorg.db.query.count`.
- **Never reuse an OpenTelemetry namespace** (`db.`, `http.`, `service.`, `user.`) for your own names. A future semconv release may claim the name with a different meaning.
- **Units in the definition, not the name.** `myorg.db.query.duration` in seconds, not `db_duration_ms`.
- **Counts end in `.count`**, booleans read as a fact (`myorg.cache.hit`), enums have a fixed, documented value list.
- **Errors are classes, not messages.** `error.type=timeout`, never the exception text.

Renames teams commonly need:

| Earlier name | Now | Why |
| --- | --- | --- |
| `main: true` | Event name `myorg.request.summary` | A first-class log record field, not an attribute |
| `trace_id`, `span_id` attributes | The log record's trace context | Native correlation fields |
| `service.team` | `myorg.service.team` | `service.*` belongs to OpenTelemetry |
| `service.build.git_hash` | `vcs.ref.head.revision` | Semconv VCS attribute |
| `deployment.environment` | `deployment.environment.name` | Renamed in semconv |
| `cloud.az` | `cloud.availability_zone` | Semconv name |
| `http.method`, `http.status_code` | `http.request.method`, `http.response.status_code` | Old names deprecated |
| `duration_ms` | `myorg.duration` (s) | Org prefix; seconds |
| `user.team.id` | `myorg.user.team.id` | Org concepts go under the org prefix |
| `db.query_count`, `db.duration_ms` | `db.client.operation.duration` (s, histogram metric) | Semconv already covers it: the histogram's count = executions, its sum = total time |
| `cache.hit` | `myorg.cache.hit` | Org prefix |
| `outcome` | `myorg.outcome` | Org prefix; fixed value list |

To propose a new attribute, give its name, type, unit, a one-line brief, example values, and whether it's a metric dimension, a measure or drill-down only. Review is for naming and cardinality.

## Sensitive data

Telemetry is copied, retained and read widely. Treat every attribute as if a broad audience will see it.

**Never emit:**

- Passwords, tokens, API keys, session cookies, or `Authorization` headers
- Credentials inside URLs (`url.full` with `user:pass@`, or tokens in query strings)
- Request or response bodies
- Email addresses, names, phone numbers, or employee numbers

**Allowed with care:**

- `user.id` only as an opaque internal identifier. If the only identifier you have is personal data, hash it in the collector before export.
- `client.address` and `user_agent.original` on spans for debugging, never as metric dimensions.

Redact at the source when you can, and add a collector `transform` or `redaction` processor as a safety net. A leaked secret in telemetry is an incident: rotate it, then purge it from the store.

## Cardinality

An attribute can be a metric dimension only if its set of values is bounded: it doesn't grow with traffic, users or deploys. High-cardinality values are welcome on spans and events, where they make drill-down possible.

| Role | Examples | Use it to |
| --- | --- | --- |
| Dimension | `deployment.environment.name`, `service.name`, `myorg.service.team`, `service.version`, `http.request.method`, `http.route`, `http.response.status_code`, `error.type`, outcome enums | Group, filter and alert |
| Measure | Durations, counts, sizes, cache hits | Sum, count, average, take percentiles; never group by |
| Drill-down only | `user.id`, `session.id`, trace and span IDs, `service.instance.id`, `k8s.pod.name`, `url.path`, `client.address` | Find one request; never a metric label |

Three rules keep cardinality safe:

- **Templates, not values, in names.** A span named `GET /v1/documents/8812` creates one series per document. `GET /v1/documents/{id}` creates one.
- **Never multiply dimensions blindly.** Tenant × route × status can explode. Give tenant-level metrics their own aggregation without route.
- **Watch client-controlled values.** `server.address` from a Host header, or a raw user agent, lets a caller create unlimited series.

## Checklist

- [ ] All required resource attributes are set, from config rather than code
- [ ] `service.namespace` and `deployment.environment.name` are separate and correct
- [ ] Every attribute either matches a semconv name or sits under `myorg.`
- [ ] No deprecated semconv names remain
- [ ] Span names and routes are templates
- [ ] Durations are in seconds
- [ ] No secrets, credentials or personal data in any attribute
- [ ] High-cardinality values are never used as metric dimensions
