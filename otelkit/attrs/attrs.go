// Package attrs holds attribute keys, event names, and enum values from the
// org telemetry registry (weaver/registry) and the OpenTelemetry semantic
// conventions it depends on (v1.43.0).
//
// Hand-maintained until registry code generation lands (README roadmap,
// phase 3). The Weaver gate is the safety net: a typo here shows up as a
// missing_attribute violation in CI.
package attrs

import "go.opentelemetry.io/otel/attribute"

// OpenTelemetry semantic conventions v1.43.0.
const (
	ServiceName               = attribute.Key("service.name")
	ServiceNamespace          = attribute.Key("service.namespace")
	ServiceVersion            = attribute.Key("service.version")
	ServiceInstanceID         = attribute.Key("service.instance.id")
	DeploymentEnvironmentName = attribute.Key("deployment.environment.name")
	VCSRefHeadRevision        = attribute.Key("vcs.ref.head.revision")

	HTTPRequestMethod      = attribute.Key("http.request.method")
	HTTPRoute              = attribute.Key("http.route")
	HTTPResponseStatusCode = attribute.Key("http.response.status_code")
	ErrorType              = attribute.Key("error.type")
	ServerAddress          = attribute.Key("server.address")
	ServerPort             = attribute.Key("server.port")
	UserID                 = attribute.Key("user.id")
)

// Org registry attributes.
const (
	Kfo11yServiceTeam = attribute.Key("kfo11y.service.team")

	Kfo11yUserTeamID = attribute.Key("kfo11y.user.team.id")
	Kfo11yUserTier   = attribute.Key("kfo11y.user.tier")

	Kfo11yDuration = attribute.Key("kfo11y.duration")
	Kfo11yOutcome  = attribute.Key("kfo11y.outcome")

	Kfo11ySearchIndexName             = attribute.Key("kfo11y.search.index.name")
	Kfo11ySearchResultCount           = attribute.Key("kfo11y.search.result.count")
	Kfo11ySearchIndexRevision         = attribute.Key("kfo11y.search.index.revision")
	Kfo11ySearchIndexPreviousRevision = attribute.Key("kfo11y.search.index.previous_revision")
	Kfo11ySearchIndexDocumentCount    = attribute.Key("kfo11y.search.index.document.count")

	Kfo11yDBQueryCount        = attribute.Key("kfo11y.db.query.count")
	Kfo11yDBQueryDuration     = attribute.Key("kfo11y.db.query.duration")
	Kfo11yCacheHit            = attribute.Key("kfo11y.cache.hit")
	Kfo11yCacheRetryCount     = attribute.Key("kfo11y.cache.retry.count")
	Kfo11yFeatureFlagsEnabled = attribute.Key("kfo11y.feature_flags.enabled")

	Kfo11yJobName = attribute.Key("kfo11y.job.name")

	Kfo11yWideEventTruncated    = attribute.Key("kfo11y.wide_event.truncated")
	Kfo11yWideEventDroppedCount = attribute.Key("kfo11y.wide_event.dropped_attributes.count")
)

// Wide-event names (LogRecord EventName).
const (
	EventRequestSummary = "kfo11y.request.summary"
	EventJobSummary     = "kfo11y.job.summary"
)

// Point-in-time event names (LogRecord EventName), emitted with wide.Log.
const (
	EventSearchIndexActivation = "kfo11y.search.index.activation"
)

// Outcome is a value of kfo11y.outcome.
type Outcome string

// kfo11y.outcome values.
const (
	OutcomeSuccess       Outcome = "success"
	OutcomeClientError   Outcome = "client_error"
	OutcomeUpstreamError Outcome = "upstream_error"
	OutcomeServerError   Outcome = "server_error"
)
