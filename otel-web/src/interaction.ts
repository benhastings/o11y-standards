import { SpanKind, SpanStatusCode, trace, type Attributes } from '@opentelemetry/api';
import { logs, SeverityNumber } from '@opentelemetry/api-logs';

import {
  ATTR_KFO11Y_DURATION,
  ATTR_KFO11Y_OUTCOME,
  ATTR_KFO11Y_UI_ACTION,
  ATTR_KFO11Y_UI_API_CALL_COUNT,
  ATTR_KFO11Y_UI_API_CALL_ERROR_COUNT,
  ATTR_KFO11Y_UI_ROUTE,
  ATTR_KFO11Y_WIDE_EVENT_TRUNCATED,
  ATTR_ERROR_TYPE,
  ATTR_SESSION_ID,
  ATTR_USER_ID,
  EVENT_UI_INTERACTION_SUMMARY,
  type Outcome,
} from './attrs';

const SCOPE = '@kfo11y/otel-web';
const VERSION = '0.1.0';
const MAX_STRING = 1024;

/** One user interaction. Enrich it inside trackInteraction's callback. */
export class Interaction {
  readonly startedAt = performance.now();
  apiCalls = 0;
  apiErrors = 0;
  outcome?: Outcome;
  errorType?: string;
  private readonly attrs: Attributes = {};

  constructor(
    readonly action: string,
    readonly route: string,
  ) {}

  /** Add attributes. Keys must exist in the registry, or the gate fails them. */
  set(attrs: Attributes): void {
    Object.assign(this.attrs, attrs);
  }

  /** Record a business result such as a denied action. */
  setOutcome(outcome: Outcome): void {
    this.outcome = outcome;
  }

  /** @internal */
  extra(): Attributes {
    return this.attrs;
  }
}

const active: Interaction[] = [];
let userId: string | undefined;
const sessionId = crypto.randomUUID();

/** Call after sign-in (and with undefined on sign-out). */
export function setUser(id: string | undefined): void {
  userId = id;
}

/** @internal Called by the fetch/XHR instrumentation for every API response. */
export function recordApiCall(status: number): void {
  const current = active[active.length - 1];
  if (!current) return;
  current.apiCalls++;
  if (status === 0 || status >= 400) current.apiErrors++;
}

/**
 * Run fn as one tracked user interaction: an INTERNAL span that parents the
 * API calls fn makes, plus one kfo11y.ui.interaction.summary event.
 *
 * action and route must be low-cardinality: "save_policy", "/policies/:policyId".
 */
export function trackInteraction<T>(
  action: string,
  route: string,
  fn: (ix: Interaction) => Promise<T> | T,
): Promise<T> {
  const ix = new Interaction(action, route);
  const tracer = trace.getTracer(SCOPE, VERSION);
  return tracer.startActiveSpan(
    `ui.${action}`,
    { kind: SpanKind.INTERNAL, attributes: { [ATTR_KFO11Y_UI_ACTION]: action, [ATTR_KFO11Y_UI_ROUTE]: route } },
    async (span) => {
      active.push(ix);
      try {
        const result = await fn(ix);
        ix.outcome ??= ix.apiErrors > 0 ? 'upstream_error' : 'success';
        return result;
      } catch (err) {
        ix.outcome ??= ix.apiErrors > 0 ? 'upstream_error' : 'ui_error';
        ix.errorType ??= err instanceof Error ? err.name : '_OTHER';
        span.setStatus({ code: SpanStatusCode.ERROR, message: ix.errorType });
        throw err;
      } finally {
        active.splice(active.indexOf(ix), 1);
        emit(ix); // inside the span's context, so the record is correlated
        span.end();
      }
    },
  );
}

function emit(ix: Interaction): void {
  const failed = ix.outcome !== 'success' && ix.outcome !== 'denied';
  // The registry requires error.type whenever the interaction failed.
  if (failed) ix.errorType ??= '_OTHER';
  const attributes: Attributes = {
    ...ix.extra(),
    [ATTR_KFO11Y_UI_ACTION]: ix.action,
    [ATTR_KFO11Y_UI_ROUTE]: ix.route,
    [ATTR_KFO11Y_OUTCOME]: ix.outcome,
    [ATTR_KFO11Y_DURATION]: (performance.now() - ix.startedAt) / 1000,
    [ATTR_KFO11Y_UI_API_CALL_COUNT]: ix.apiCalls,
    [ATTR_KFO11Y_UI_API_CALL_ERROR_COUNT]: ix.apiErrors,
    [ATTR_SESSION_ID]: sessionId,
    [ATTR_USER_ID]: userId,
    [ATTR_ERROR_TYPE]: ix.errorType,
  };
  const clean = clip(attributes);

  logs.getLogger(SCOPE, VERSION).emit({
    eventName: EVENT_UI_INTERACTION_SUMMARY,
    severityNumber: failed ? SeverityNumber.ERROR : SeverityNumber.INFO,
    severityText: failed ? 'ERROR' : 'INFO',
    body: `${ix.action} ${ix.route} ${ix.outcome}`,
    attributes: clean,
  });
}

// Drop undefined values and cap string length (docs/02 guardrails).
function clip(input: Attributes): Attributes {
  const out: Attributes = {};
  let truncated = false;
  for (const [k, v] of Object.entries(input)) {
    if (v === undefined) continue;
    if (typeof v === 'string' && v.length > MAX_STRING) {
      out[k] = `${v.slice(0, MAX_STRING - 1)}…`;
      truncated = true;
    } else {
      out[k] = v;
    }
  }
  if (truncated) out[ATTR_KFO11Y_WIDE_EVENT_TRUNCATED] = true;
  return out;
}
