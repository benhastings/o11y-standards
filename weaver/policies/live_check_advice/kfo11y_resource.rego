package live_check_advice

import rego.v1

# Resource attributes every service must set itself.
deny contains kfo11y_finding(
	"kfo11y_missing_resource_attribute",
	"violation",
	{"attribute_key": required},
	sprintf("Resource is missing required attribute '%s'. Set it via otelkit config or the deployment env (docs/01-telemetry-standard.md#resource).", [required]),
) if {
	input.sample.resource
	names := {a.name | some a in input.sample.resource.attributes}
	some required in data.kfo11y.required_resource_attributes
	not required in names
}

# deployment.environment.name must be one of the agreed values.
deny contains kfo11y_finding(
	"kfo11y_unknown_environment",
	"violation",
	{"attribute_key": "deployment.environment.name", "attribute_value": a.value},
	sprintf("deployment.environment.name '%v' is not in the allowed list %v.", [a.value, data.kfo11y.allowed_environments]),
) if {
	input.sample.resource
	some a in input.sample.resource.attributes
	a.name == "deployment.environment.name"
	not a.value in data.kfo11y.allowed_environments
}

# Helper. Prefixed so it can't collide with helpers in the upstream policy file.
kfo11y_finding(id, level, context, message) := {
	"id": id,
	"level": level,
	"context": context,
	"message": message,
}
