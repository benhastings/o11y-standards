// Attribute keys from the org registry (weaver/registry) and OpenTelemetry
// semantic conventions v1.43.0.
//
// Unstable semconv keys are copied here rather than imported from
// '@opentelemetry/semantic-conventions/incubating', which is what
// opentelemetry-js recommends for development-stability conventions.

// OpenTelemetry semantic conventions v1.43.0
export const ATTR_SERVICE_NAME = 'service.name';
export const ATTR_SERVICE_NAMESPACE = 'service.namespace';
export const ATTR_SERVICE_VERSION = 'service.version';
export const ATTR_SERVICE_INSTANCE_ID = 'service.instance.id';
export const ATTR_DEPLOYMENT_ENVIRONMENT_NAME = 'deployment.environment.name';
export const ATTR_VCS_REF_HEAD_REVISION = 'vcs.ref.head.revision';
export const ATTR_USER_ID = 'user.id';
export const ATTR_SESSION_ID = 'session.id';
export const ATTR_ERROR_TYPE = 'error.type';

// Org registry
export const ATTR_KFO11Y_SERVICE_TEAM = 'kfo11y.service.team';
export const ATTR_KFO11Y_USER_TEAM_ID = 'kfo11y.user.team.id';
export const ATTR_KFO11Y_DURATION = 'kfo11y.duration';
export const ATTR_KFO11Y_OUTCOME = 'kfo11y.outcome';
export const ATTR_KFO11Y_UI_ROUTE = 'kfo11y.ui.route';
export const ATTR_KFO11Y_UI_ACTION = 'kfo11y.ui.action';
export const ATTR_KFO11Y_UI_API_CALL_COUNT = 'kfo11y.ui.api_call.count';
export const ATTR_KFO11Y_UI_API_CALL_ERROR_COUNT = 'kfo11y.ui.api_call.error.count';
export const ATTR_KFO11Y_WIDE_EVENT_TRUNCATED = 'kfo11y.wide_event.truncated';

export const EVENT_UI_INTERACTION_SUMMARY = 'kfo11y.ui.interaction.summary';

export type Outcome =
  | 'success'
  | 'denied'
  | 'client_error'
  | 'upstream_error'
  | 'server_error'
  | 'ui_error';
