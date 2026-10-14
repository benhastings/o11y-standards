package live_check_advice

import rego.v1

# Wide events over the hard limit fail the gate.
deny contains kfo11y_finding(
	"kfo11y_wide_event_too_large",
	"violation",
	{"event_name": log.event_name, "approx_bytes": size},
	sprintf("Wide event '%s' is ~%d bytes; the hard limit is %d. Drop payload dumps, store references instead.", [log.event_name, size, data.kfo11y.wide_event.hard_limit_bytes]),
) if {
	log := input.sample.log
	log.event_name in data.kfo11y.wide_event_names
	size := count(json.marshal(log.attributes))
	size > data.kfo11y.wide_event.hard_limit_bytes
}

# Wide events over the target size get advice, not a failure.
deny contains kfo11y_finding(
	"kfo11y_wide_event_over_target",
	"improvement",
	{"event_name": log.event_name, "approx_bytes": size},
	sprintf("Wide event '%s' is ~%d bytes; the target is %d.", [log.event_name, size, data.kfo11y.wide_event.target_bytes]),
) if {
	log := input.sample.log
	log.event_name in data.kfo11y.wide_event_names
	size := count(json.marshal(log.attributes))
	size > data.kfo11y.wide_event.target_bytes
	size <= data.kfo11y.wide_event.hard_limit_bytes
}

# Log records without an event name are free-text logs.
deny contains kfo11y_finding(
	"kfo11y_unnamed_log",
	data.kfo11y.unnamed_log_level,
	{},
	"Log record has no event name. Add detail to the wide event, or emit a named event defined in the registry.",
) if {
	log := input.sample.log
	object.get(log, "event_name", "") == ""
}

# Attribute names that suggest secrets or credentials never leave the process.
deny contains kfo11y_finding(
	"kfo11y_sensitive_attribute",
	"violation",
	{"attribute_key": input.sample.attribute.name},
	sprintf("Attribute '%s' looks like a credential or secret. Remove it at the source.", [input.sample.attribute.name]),
) if {
	input.sample.attribute
	regex.match(`(?i)(^|[._-])(password|passwd|secret|token|authorization|cookie|set-cookie|api[_-]?key)($|[._-])`, input.sample.attribute.name)
}
