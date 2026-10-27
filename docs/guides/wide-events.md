# Wide Events: A Guide for Service Teams

!!! note "Standalone guide"
    General guidance for any service team, independent of this repository's
    tooling. It uses `myorg.` as a placeholder for your org prefix; this
    repository's own standard uses `kfo11y.` (see [01](../01-telemetry-standard.md)).

## Why wide events

Wide events let us answer the incident question nobody planned for with a query instead of a code change, and they give us RED metrics and SLIs from a record we already emit.

**Wide on purpose.** A wide event gets more useful with every field on it. In an incident, the question is rarely the one you planned for. Is it one tenant, one version, one pod, one route, a cold cache? Each attribute already on the event is a question you can answer with a filter instead of a redeploy.

So the default is to include context, not to leave it out. An extra attribute costs a few bytes. A missing one costs a code change, a deploy and a wait for the problem to happen again. A typical request event carries 20 or more fields, and that's the floor, not the ceiling.

Every event is kept even when traces are sampled, so wide events are also the cheapest source of RED metrics and SLIs. You can sample traces hard and still keep 100% of the per-request record.

## What is a wide event?

A wide event is **one structured record per unit of work**, emitted when the work finishes, carrying everything you'd want to know about it: who asked, what they asked for, what was decided, what it cost, and how it ended. One request produces one row, not fifteen log lines.

A wide event is an OpenTelemetry **log-based event**: a log record with an event name, emitted inside the active trace so it carries the trace and span IDs. That is OpenTelemetry's model for events, and it replaces scattered free-text log lines with one queryable record.

| Question | Free-text logs | Wide event (log-based event) |
| --- | --- | --- |
| Queryable without parsing? | No | Yes: one row with typed fields |
| Consistent field names? | Per developer | Defined in a shared schema |
| Answers "who, what, how long, what failed" in one place? | No: spread over many lines | Yes |
| Correlated with the trace? | Only if someone adds the IDs | Yes: trace and span ID from the active context |
| Kept when traces are sampled? | Yes | Yes: log records aren't sampled with traces |
| Feeds metrics and SLIs? | Only after parsing | Directly, with a `GROUP BY` |

## When to emit

Emit exactly one wide event per unit of work, on every exit path, including errors and panics.

| Unit of work | Event name | Emitted when |
| --- | --- | --- |
| Inbound HTTP or RPC request | `myorg.request.summary` | The response is sent |
| Background job or message handler | `myorg.job.summary` | The job or message finishes |
| User interaction in a front end | `myorg.ui.interaction.summary` | The interaction settles (its API calls are done) |

Build it by **accumulating context as you go**. Middleware starts the event when the work begins and emits it at the end. In between, each layer adds what only it knows: authentication adds the caller, data access adds query counts and time, the search layer adds which tier served it, error handling adds the error class. Handlers enrich; they never emit directly.

Something that happens outside a unit of work, such as a cache reload or a certificate rotation, is its own **named event** with its own schema, not a sentence in a log.

## What goes in

Every wide event carries the context below, grouped by the question it answers. Identity and environment come from the resource, so your code never sets them, but they arrive on every event and you query them like any other field. The middleware adds the request basics. Your code adds the rest as the work runs.

OpenTelemetry semantic-convention names are used wherever they exist. Everything else sits under your org prefix (`myorg.` here). Durations are in seconds.

### Event marker and trace context

| Field | Example | Required | Why it's there |
| --- | --- | --- | --- |
| Event name (log record field) | `myorg.request.summary` | Always | Marks the record as the canonical wide event. One filter finds every request |
| Trace ID (log record field) | `4bf92f3577b34da6a3ce929d0e0e4736` | Always, from context | Jump from the event to the distributed trace |
| Span ID (log record field) | `00f067aa0ba902b7` | Always, from context | The server span the event was emitted under |
| Severity (log record field) | `INFO`, `ERROR` | Always | Follows the outcome, so log tooling sorts it correctly |

### Service identity (resource)

| Attribute | Example | Required | Why it's there |
| --- | --- | --- | --- |
| `service.name` | `search-api` | Always | Which service did the work |
| `service.namespace` | `search` | Always | The product area it belongs to |
| `myorg.service.team` | `search-team` | Always | Who to page. Routes alerts and incidents |
| `service.version` | `2.4.1` | Always | Which release. Compares deploys |
| `vcs.ref.head.revision` | `8f7a2b3c` | Always | The exact commit. Answers "what changed?" |

### Environment (resource)

| Attribute | Example | Required | Why it's there |
| --- | --- | --- | --- |
| `deployment.environment.name` | `production`, `staging` | Always | Keeps test traffic out of production numbers |
| `cloud.region` | `us-east-1` | Always, from the collector | Is it one region? |
| `cloud.availability_zone` | `us-east-1a` | Always, from the collector | Is it one zone? |
| `k8s.cluster.name` | `prod-use1` | Always, from the collector | Is it one cluster? |
| `k8s.pod.name` | `search-api-7f8g9-2x5` | Always, from the collector | Is it one bad pod? |
| `service.instance.id` | `7d3c…` | Always | The process, across restarts and reschedules |

### HTTP and execution

| Attribute | Example | Required | Why it's there |
| --- | --- | --- | --- |
| `http.request.method` | `POST` | Request events | What was asked |
| `http.route` | `/v1/indexes/{index}/search` | If a route matched | Rates and latency per endpoint. A template, never the raw path |
| `http.response.status_code` | `200`, `503` | If a response was sent | What the caller saw |
| `myorg.duration` | `0.1425` (s) | Always | End-to-end latency of the unit of work |
| `myorg.job.name` | `search-index-rebuild` | Job events | Which job ran |
| `myorg.ui.action`, `myorg.ui.route` | `run_search`, `/search` | UI events | What the user did, and where |

### Customer context

| Attribute | Example | Required | Why it's there |
| --- | --- | --- | --- |
| `user.id` | `usr_98123` | Once authenticated | Find one user's requests. An opaque ID, never personal data |
| `myorg.user.team.id` (tenant) | `enterprise_corp_a` | Once authenticated | Isolate one tenant's impact during an incident |
| `myorg.user.tier` | `enterprise` | Recommended | Weigh impact by service tier |
| `session.id` | `s_51b2…` | UI events | Follow one browser session |

### Work metrics

| Attribute | Example | Required | Why it's there |
| --- | --- | --- | --- |
| `db.client.operation.duration` | `histogram (s)` | Recommended | Semconv metric from the DB client instrumentation, not an event attribute. Its count is DB executions (spots N+1 queries), its sum is total DB time |
| `myorg.cache.hit` | `false` | Recommended | Cold cache or eviction storm? |
| `myorg.cache.retry.count` | `0` | Recommended | A cache under strain shows up here first |
| `myorg.ui.api_call.count`, `myorg.ui.api_call.error.count` | `3`, `0` | UI events | How many API calls one interaction made, and how many failed |

### Decisions and features

| Attribute | Example | Required | Why it's there |
| --- | --- | --- | --- |
| `myorg.search.served_from` | `cache`, `index`, `db` | Search requests | Which tier answered, not just what it returned |
| `myorg.search.index.name` | `products` | Recommended | Which index answered the query |
| `myorg.feature_flags.enabled` | `["new-ranker"]` | Recommended | Ties a regression to a flag, not just a deploy |

### Diagnostics

| Attribute | Example | Required | Why it's there |
| --- | --- | --- | --- |
| `myorg.outcome` | `success`, `upstream_error` | Always | How the work ended, in business terms (values below) |
| `error.type` | `GatewayTimeout` | On failure | The failure class. Groups errors without parsing messages |

**Add your own.** This list is where you start, not where you stop. Add anything that answers a question about this unit of work: the code path taken, items processed, the upstream that was slow, payload sizes, retry counts. Define each new attribute once in the registry under your org prefix, then set it everywhere it applies.

**Coming from the earlier attribute list?** The concepts are the same. Some names changed to match OpenTelemetry or move under the org prefix:

| Earlier name | Now |
| --- | --- |
| `main: true` | Event name `myorg.request.summary` |
| `trace_id`, `span_id` attributes | The log record's own trace context |
| `service.team` | `myorg.service.team` |
| `service.build.git_hash` | `vcs.ref.head.revision` |
| `deployment.environment` | `deployment.environment.name` |
| `cloud.az` | `cloud.availability_zone` |
| `http.method`, `http.status_code` | `http.request.method`, `http.response.status_code` |
| `duration_ms` | `myorg.duration` (seconds) |
| `user.team.id` | `myorg.user.team.id` |
| `db.query_count`, `db.duration_ms` | `db.client.operation.duration` (seconds, histogram metric) |
| `cache.hit` | `myorg.cache.hit` |
| `outcome` | `myorg.outcome` |

**Outcome values.** One field tells you how the work ended, so dashboards don't have to infer it from status codes:

| `myorg.outcome` | Meaning | Severity |
| --- | --- | --- |
| `success` | Completed as intended | INFO |
| `rejected` | Completed, and the answer was a deliberate "no", such as a query past the result limit. Not an error | INFO |
| `client_error` | The caller's request couldn't be served | WARN |
| `upstream_error` | A dependency failed | ERROR |
| `server_error` | This service failed | ERROR |
| `ui_error` | Front end only: browser code failed without an API error | ERROR |

Never add `trace_id` or `span_id` as attributes: the log record carries them natively. The body is a one-line summary for people reading the log, such as "POST /v1/indexes/products/search 422 rejected". Every value a query, dashboard or alert needs goes in its own attribute. Never extract values from the body with a regex or string split: its wording can change at any time, and anything that depends on it breaks without warning.

## How to emit

Emit a wide event through the OpenTelemetry **Logs API**: a log record with an event name, emitted inside the active span's context so the trace and span IDs come with it. Since 17 March 2026, OpenTelemetry's guidance is that events are log records with names ([announcement](https://opentelemetry.io/blog/2026/deprecating-span-events/)).

Go, with the OpenTelemetry Logs API:

```go
logger := global.GetLoggerProvider().Logger("search-api")

var rec log.Record
rec.SetEventName("myorg.request.summary")
rec.SetTimestamp(time.Now())
rec.SetSeverity(log.SeverityInfo)
rec.SetBody(log.StringValue("POST /v1/indexes/products/search 422 rejected"))
rec.AddAttributes(
    log.String("http.request.method", "POST"),
    log.String("http.route", "/v1/indexes/{index}/search"),
    log.Int("http.response.status_code", 422),
    log.Float64("myorg.duration", elapsed.Seconds()),
    log.String("myorg.outcome", "rejected"),
)
logger.Emit(ctx, rec) // ctx carries the span: trace and span IDs come along
```

TypeScript, with `@opentelemetry/api-logs`:

```ts
logger.emit({
  eventName: "myorg.ui.interaction.summary",
  severityNumber: SeverityNumber.INFO,
  body: "run_search success",
  attributes: {
    "myorg.ui.action": "run_search",
    "myorg.ui.route": "/search",
    "myorg.duration": 0.42,
    "myorg.outcome": "success",
  },
});
```

In practice, wrap this in one shared middleware or helper per language, so service code only calls something like `event.Set(ctx, key, value)` and never builds the record itself. The helper owns emitting on every exit path, mapping outcome to severity, and enforcing size limits.

## Example

A search that was correctly rejected for asking past the result limit, as a collector receives it (simplified OTLP). Every category above is present, and that's what a normal request should look like:

```json
{
  "resource": {
    "service.name": "search-api",
    "service.namespace": "search",
    "myorg.service.team": "search-team",
    "service.version": "2.4.1",
    "vcs.ref.head.revision": "8f7a2b3c",
    "service.instance.id": "7d3c1e9a-52f0-4b8e-9a61-0c2f4e8d1b77",
    "deployment.environment.name": "production",
    "cloud.region": "us-east-1",
    "cloud.availability_zone": "us-east-1a",
    "k8s.cluster.name": "prod-use1",
    "k8s.pod.name": "search-api-7f8g9-2x5"
  },
  "event_name": "myorg.request.summary",
  "trace_id": "4bf92f3577b34da6a3ce929d0e0e4736",
  "span_id": "00f067aa0ba902b7",
  "severity_text": "INFO",
  "body": "POST /v1/indexes/products/search 422 rejected",
  "attributes": {
    "http.request.method": "POST",
    "http.route": "/v1/indexes/{index}/search",
    "http.response.status_code": 422,
    "myorg.duration": 0.0124,
    "user.id": "usr_98123",
    "myorg.user.team.id": "enterprise_corp_a",
    "myorg.user.tier": "enterprise",
    "myorg.cache.hit": false,
    "myorg.cache.retry.count": 0,
    "myorg.search.served_from": "index",
    "myorg.search.index.name": "products",
    "myorg.feature_flags.enabled": ["new-ranker"],
    "myorg.outcome": "rejected"
  }
}
```

The outcome is `rejected`, not `client_error`: the service answered correctly, so this request counts as good in an availability SLI. A handler that returns a deliberate "no" sets the outcome explicitly; a bare 4xx defaults to `client_error`.

## Querying and deriving metrics

With one row per request, RED metrics and SLIs are a `GROUP BY`. The examples below are plain SQL over a table of events with an `attributes` map; adapt the column names to your backend.

Rate, errors and latency per route:

```sql
SELECT
    service_name,
    attributes['http.route'] AS route,
    count(*) AS requests,
    sum(CASE WHEN attributes['myorg.outcome'] IN ('server_error', 'upstream_error') THEN 1 ELSE 0 END) AS errors,
    percentile_cont(0.99) WITHIN GROUP (ORDER BY CAST(attributes['myorg.duration'] AS DOUBLE)) AS p99_s
FROM events
WHERE event_name = 'myorg.request.summary'
  AND ts > now() - INTERVAL '1 hour'
GROUP BY service_name, route
ORDER BY requests DESC;
```

An availability SLI counts `success`, `rejected` and `client_error` as good, because the service answered correctly. A latency SLI counts requests under a threshold, such as 0.3 s.

For long-range charts, keep **rollups**: aggregate each minute of events by a few bounded dimensions (environment, service, version, route, status, outcome) into counts, sums and percentile sketches. Raw events then only need a short retention window. Keep tenant-level rollups separate from route-level ones so the dimensions don't multiply.

To jump from a trace to its record, join on trace and span ID: the event carries both from the span it was emitted under.

## Limits, pitfalls and checklist

| Limit | Value | Why |
| --- | --- | --- |
| Target event size | 8 KiB | Cheap to ship and store |
| Hard limit | 16 KiB | Drop non-essential attributes above it |
| String value | 1 KiB | Truncate and flag it (e.g. `myorg.wide_event.truncated=true`) |
| Array value | 32 elements | Truncate |
| Payloads, stack traces, tokens | Never | Store a reference such as an object ID instead |

Common mistakes:

- **Emitting from handlers.** Only the middleware emits. Handlers that emit their own produce two events per request.
- **Raw paths in `http.route`.** Use the matched template.
- **Error messages in `error.type`.** Use a class such as `timeout` or `db_unavailable`. Messages and stack traces don't belong on the event.
- **Per-query detail on the event.** Keep roll-ups (count, total time) on the event and per-call detail on client spans.
- **Treating a deliberate "no" as an error.** Set `rejected` so correct answers don't burn the error budget.

- [ ] One event per unit of work, emitted by shared middleware on every exit path
- [ ] Emitted through the Logs API, with an event name
- [ ] Every category of context present: identity, environment, request, customer, work metrics, decisions and diagnostics
- [ ] Trace context comes from the active span, not from attributes
- [ ] No free-text operational logs
- [ ] Size limits enforced; no payloads, secrets or personal data
