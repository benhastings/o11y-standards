package live_check_advice

import rego.v1

# "A span with no parent is never CLIENT" can't be checked here: live-check
# samples carry no parent span id. The ClickHouse hygiene query in docs/03
# covers it.

# Span names must be low-cardinality: no IDs, no UUIDs, no numeric path segments.
deny contains kfo11y_finding(
	"kfo11y_high_cardinality_span_name",
	"violation",
	{"span_name": span.name},
	sprintf("Span name '%s' contains an identifier. Use the route template (e.g. 'GET /v1/policies/{id}').", [span.name]),
) if {
	span := input.sample.span
	regex.match(`(/[0-9]+(/|$| ))|([0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12})`, span.name)
}

# HTTP server spans that matched a route must carry http.route.
deny contains kfo11y_finding(
	"kfo11y_missing_http_route",
	"violation",
	{"span_name": span.name},
	sprintf("HTTP server span '%s' has no http.route. Use wide.Handler, or call wide.SetRoute with your router's pattern.", [span.name]),
) if {
	span := input.sample.span
	lower(span.kind) == "server"
	names := {a.name | some a in span.attributes}
	"http.request.method" in names
	not "http.route" in names
	not kfo11y_unrouted_status(span)
}

# Span events: the Span Event API is deprecated upstream (OTEP 4430).
deny contains kfo11y_finding(
	"kfo11y_span_event_deprecated",
	data.kfo11y.span_event_level,
	{"span_event_name": input.sample.span_event.name},
	sprintf("Span event '%s' uses the deprecated Span Event API. Emit a named log-based event or add the detail to the wide event (docs/02-logging-standard-wide-events.md).", [input.sample.span_event.name]),
) if {
	input.sample.span_event
}

# 404/405 responses legitimately have no matched route.
kfo11y_unrouted_status(span) if {
	some a in span.attributes
	a.name == "http.response.status_code"
	a.value in {404, 405}
}
