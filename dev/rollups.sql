-- Metric rollups over wide events (docs/01, "Metric aggregation").
--
-- Raw wide events in otel_logs are kept for a short window (the exporter's
-- TTL). These tables keep only the dimensions and measures that make sense to
-- aggregate, so RED metrics, SLIs and tenant impact outlive the raw events.
-- High-cardinality attributes (user.id, session.id, document ids, pod names,
-- trace ids) are left out on purpose: they stay in the raw events and age out.
--
-- Load after the collector has created otel_logs:
--   docker compose -f dev/docker-compose.yaml exec -T clickhouse \
--     clickhouse-client --user otel --password otel -d otel --multiquery < dev/rollups.sql
--
-- Materialized views only see rows inserted after they exist. To backfill,
-- run each view's SELECT as INSERT INTO <table> SELECT ... over the window
-- you want.
--
-- The TTLs are a starting point, not a decision: set them to the platform's
-- retention policy.

-- Requests: one row per minute per service, route and result.
CREATE TABLE IF NOT EXISTS kfo11y_request_rollup_1m
(
    t              DateTime,
    environment    LowCardinality(String),
    service        LowCardinality(String),
    namespace      LowCardinality(String),
    team           LowCardinality(String),
    version        LowCardinality(String),
    method         LowCardinality(String),
    route          LowCardinality(String),
    status         UInt16,
    outcome        LowCardinality(String),
    error_type     LowCardinality(String),
    search_index   LowCardinality(String),
    requests       SimpleAggregateFunction(sum, UInt64),
    duration_sum   SimpleAggregateFunction(sum, Float64),
    duration_q     AggregateFunction(quantilesTDigest(0.5, 0.9, 0.99), Float64),
    db_queries     SimpleAggregateFunction(sum, UInt64),
    db_seconds     SimpleAggregateFunction(sum, Float64),
    cache_lookups  SimpleAggregateFunction(sum, UInt64),
    cache_hits     SimpleAggregateFunction(sum, UInt64),
    cache_retries  SimpleAggregateFunction(sum, UInt64),
    searches       SimpleAggregateFunction(sum, UInt64),
    search_results SimpleAggregateFunction(sum, UInt64),
    zero_results   SimpleAggregateFunction(sum, UInt64),
    truncated      SimpleAggregateFunction(sum, UInt64)
)
ENGINE = AggregatingMergeTree
PARTITION BY toYYYYMM(t)
ORDER BY (environment, service, t, route, method, status, outcome, error_type, search_index, namespace, team, version)
TTL t + INTERVAL 13 MONTH;

CREATE MATERIALIZED VIEW IF NOT EXISTS kfo11y_request_rollup_1m_mv TO kfo11y_request_rollup_1m AS
SELECT
    toStartOfMinute(Timestamp)                                     AS t,
    ResourceAttributes['deployment.environment.name']              AS environment,
    ServiceName                                                    AS service,
    ResourceAttributes['service.namespace']                        AS namespace,
    ResourceAttributes['kfo11y.service.team']                      AS team,
    ResourceAttributes['service.version']                          AS version,
    LogAttributes['http.request.method']                           AS method,
    LogAttributes['http.route']                                    AS route,
    toUInt16OrZero(LogAttributes['http.response.status_code'])     AS status,
    LogAttributes['kfo11y.outcome']                                AS outcome,
    LogAttributes['error.type']                                    AS error_type,
    LogAttributes['kfo11y.search.index.name']                      AS search_index,
    count()                                                        AS requests,
    sum(toFloat64OrZero(LogAttributes['kfo11y.duration']))         AS duration_sum,
    quantilesTDigestState(0.5, 0.9, 0.99)(toFloat64OrZero(LogAttributes['kfo11y.duration'])) AS duration_q,
    sum(toUInt64OrZero(LogAttributes['kfo11y.db.query.count']))    AS db_queries,
    sum(toFloat64OrZero(LogAttributes['kfo11y.db.query.duration'])) AS db_seconds,
    countIf(mapContains(LogAttributes, 'kfo11y.cache.hit'))        AS cache_lookups,
    countIf(LogAttributes['kfo11y.cache.hit'] = 'true')            AS cache_hits,
    sum(toUInt64OrZero(LogAttributes['kfo11y.cache.retry.count'])) AS cache_retries,
    countIf(mapContains(LogAttributes, 'kfo11y.search.result.count')) AS searches,
    sum(toUInt64OrZero(LogAttributes['kfo11y.search.result.count'])) AS search_results,
    countIf(LogAttributes['kfo11y.search.result.count'] = '0')    AS zero_results,
    countIf(LogAttributes['kfo11y.wide_event.truncated'] = 'true') AS truncated
FROM otel_logs
WHERE EventName = 'kfo11y.request.summary'
GROUP BY t, environment, service, namespace, team, version, method, route, status, outcome, error_type, search_index;

-- Tenants: kept apart from the route rollup so tenant x route x status never
-- multiplies out. Answers "which tenants are hurting" after raw events expire.
CREATE TABLE IF NOT EXISTS kfo11y_tenant_rollup_5m
(
    t            DateTime,
    environment  LowCardinality(String),
    service      LowCardinality(String),
    tenant       LowCardinality(String),
    tier         LowCardinality(String),
    outcome      LowCardinality(String),
    requests     SimpleAggregateFunction(sum, UInt64),
    duration_sum SimpleAggregateFunction(sum, Float64),
    duration_q   AggregateFunction(quantilesTDigest(0.5, 0.9, 0.99), Float64)
)
ENGINE = AggregatingMergeTree
PARTITION BY toYYYYMM(t)
ORDER BY (environment, service, t, tenant, tier, outcome)
TTL t + INTERVAL 13 MONTH;

CREATE MATERIALIZED VIEW IF NOT EXISTS kfo11y_tenant_rollup_5m_mv TO kfo11y_tenant_rollup_5m AS
SELECT
    toStartOfFiveMinutes(Timestamp)                        AS t,
    ResourceAttributes['deployment.environment.name']      AS environment,
    ServiceName                                            AS service,
    LogAttributes['kfo11y.user.team.id']                   AS tenant,
    LogAttributes['kfo11y.user.tier']                      AS tier,
    LogAttributes['kfo11y.outcome']                        AS outcome,
    count()                                                AS requests,
    sum(toFloat64OrZero(LogAttributes['kfo11y.duration'])) AS duration_sum,
    quantilesTDigestState(0.5, 0.9, 0.99)(toFloat64OrZero(LogAttributes['kfo11y.duration'])) AS duration_q
FROM otel_logs
WHERE EventName = 'kfo11y.request.summary'
GROUP BY t, environment, service, tenant, tier, outcome;

-- Background jobs.
CREATE TABLE IF NOT EXISTS kfo11y_job_rollup_1m
(
    t            DateTime,
    environment  LowCardinality(String),
    service      LowCardinality(String),
    version      LowCardinality(String),
    job          LowCardinality(String),
    outcome      LowCardinality(String),
    error_type   LowCardinality(String),
    runs         SimpleAggregateFunction(sum, UInt64),
    duration_sum SimpleAggregateFunction(sum, Float64),
    duration_q   AggregateFunction(quantilesTDigest(0.5, 0.9, 0.99), Float64),
    db_queries   SimpleAggregateFunction(sum, UInt64),
    db_seconds   SimpleAggregateFunction(sum, Float64)
)
ENGINE = AggregatingMergeTree
PARTITION BY toYYYYMM(t)
ORDER BY (environment, service, t, job, outcome, error_type, version)
TTL t + INTERVAL 13 MONTH;

CREATE MATERIALIZED VIEW IF NOT EXISTS kfo11y_job_rollup_1m_mv TO kfo11y_job_rollup_1m AS
SELECT
    toStartOfMinute(Timestamp)                                      AS t,
    ResourceAttributes['deployment.environment.name']               AS environment,
    ServiceName                                                     AS service,
    ResourceAttributes['service.version']                           AS version,
    LogAttributes['kfo11y.job.name']                                AS job,
    LogAttributes['kfo11y.outcome']                                 AS outcome,
    LogAttributes['error.type']                                     AS error_type,
    count()                                                         AS runs,
    sum(toFloat64OrZero(LogAttributes['kfo11y.duration']))          AS duration_sum,
    quantilesTDigestState(0.5, 0.9, 0.99)(toFloat64OrZero(LogAttributes['kfo11y.duration'])) AS duration_q,
    sum(toUInt64OrZero(LogAttributes['kfo11y.db.query.count']))     AS db_queries,
    sum(toFloat64OrZero(LogAttributes['kfo11y.db.query.duration'])) AS db_seconds
FROM otel_logs
WHERE EventName = 'kfo11y.job.summary'
GROUP BY t, environment, service, version, job, outcome, error_type;

-- Front-end interactions.
CREATE TABLE IF NOT EXISTS kfo11y_ui_rollup_1m
(
    t               DateTime,
    environment     LowCardinality(String),
    service         LowCardinality(String),
    version         LowCardinality(String),
    route           LowCardinality(String),
    action          LowCardinality(String),
    outcome         LowCardinality(String),
    error_type      LowCardinality(String),
    interactions    SimpleAggregateFunction(sum, UInt64),
    duration_sum    SimpleAggregateFunction(sum, Float64),
    duration_q      AggregateFunction(quantilesTDigest(0.5, 0.9, 0.99), Float64),
    api_calls       SimpleAggregateFunction(sum, UInt64),
    api_call_errors SimpleAggregateFunction(sum, UInt64)
)
ENGINE = AggregatingMergeTree
PARTITION BY toYYYYMM(t)
ORDER BY (environment, service, t, route, action, outcome, error_type, version)
TTL t + INTERVAL 13 MONTH;

CREATE MATERIALIZED VIEW IF NOT EXISTS kfo11y_ui_rollup_1m_mv TO kfo11y_ui_rollup_1m AS
SELECT
    toStartOfMinute(Timestamp)                                          AS t,
    ResourceAttributes['deployment.environment.name']                   AS environment,
    ServiceName                                                         AS service,
    ResourceAttributes['service.version']                               AS version,
    LogAttributes['kfo11y.ui.route']                                    AS route,
    LogAttributes['kfo11y.ui.action']                                   AS action,
    LogAttributes['kfo11y.outcome']                                     AS outcome,
    LogAttributes['error.type']                                         AS error_type,
    count()                                                             AS interactions,
    sum(toFloat64OrZero(LogAttributes['kfo11y.duration']))              AS duration_sum,
    quantilesTDigestState(0.5, 0.9, 0.99)(toFloat64OrZero(LogAttributes['kfo11y.duration'])) AS duration_q,
    sum(toUInt64OrZero(LogAttributes['kfo11y.ui.api_call.count']))       AS api_calls,
    sum(toUInt64OrZero(LogAttributes['kfo11y.ui.api_call.error.count'])) AS api_call_errors
FROM otel_logs
WHERE EventName = 'kfo11y.ui.interaction.summary'
GROUP BY t, environment, service, version, route, action, outcome, error_type;
