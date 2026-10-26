import { ZoneContextManager } from '@opentelemetry/context-zone';
import { W3CTraceContextPropagator } from '@opentelemetry/core';
import { logs } from '@opentelemetry/api-logs';
import { OTLPLogExporter } from '@opentelemetry/exporter-logs-otlp-http';
import { OTLPTraceExporter } from '@opentelemetry/exporter-trace-otlp-http';
import { registerInstrumentations } from '@opentelemetry/instrumentation';
import { DocumentLoadInstrumentation } from '@opentelemetry/instrumentation-document-load';
import { FetchInstrumentation } from '@opentelemetry/instrumentation-fetch';
import { XMLHttpRequestInstrumentation } from '@opentelemetry/instrumentation-xml-http-request';
import { resourceFromAttributes } from '@opentelemetry/resources';
import { BatchLogRecordProcessor, LoggerProvider } from '@opentelemetry/sdk-logs';
import { BatchSpanProcessor } from '@opentelemetry/sdk-trace-base';
import { WebTracerProvider } from '@opentelemetry/sdk-trace-web';

import {
  ATTR_KFO11Y_SERVICE_TEAM,
  ATTR_DEPLOYMENT_ENVIRONMENT_NAME,
  ATTR_SERVICE_INSTANCE_ID,
  ATTR_SERVICE_NAME,
  ATTR_SERVICE_NAMESPACE,
  ATTR_SERVICE_VERSION,
  ATTR_VCS_REF_HEAD_REVISION,
} from './attrs';
import { recordApiCall } from './interaction';

export interface TelemetryConfig {
  serviceName: string;
  /** Product area, e.g. "search". Same value the Go APIs use. */
  serviceNamespace: string;
  serviceVersion: string;
  team: string;
  /** One of the allowed deployment.environment.name values (docs/01). */
  environment: string;
  gitSha?: string;
  /**
   * Base URL of the OTLP/HTTP endpoint. Default "/otlp": a same-origin path
   * your ingress routes to the collector, so no CORS is needed.
   */
  otlpBaseUrl?: string;
  /** API origins that should receive the traceparent header. */
  propagateTo: RegExp[];
}

let started = false;

/** Call once, before React renders. */
export function initTelemetry(cfg: TelemetryConfig): void {
  if (started) return;
  started = true;

  const resource = resourceFromAttributes({
    [ATTR_SERVICE_NAME]: cfg.serviceName,
    [ATTR_SERVICE_NAMESPACE]: cfg.serviceNamespace,
    [ATTR_SERVICE_VERSION]: cfg.serviceVersion,
    [ATTR_SERVICE_INSTANCE_ID]: crypto.randomUUID(),
    [ATTR_DEPLOYMENT_ENVIRONMENT_NAME]: cfg.environment,
    [ATTR_KFO11Y_SERVICE_TEAM]: cfg.team,
    ...(cfg.gitSha ? { [ATTR_VCS_REF_HEAD_REVISION]: cfg.gitSha } : {}),
  });
  const base = cfg.otlpBaseUrl ?? '/otlp';

  const tracerProvider = new WebTracerProvider({
    resource,
    spanProcessors: [new BatchSpanProcessor(new OTLPTraceExporter({ url: `${base}/v1/traces` }))],
  });
  // ZoneContextManager keeps the interaction span active across callbacks and
  // promises, so fetch CLIENT spans become its children. Known limit: zone.js
  // can't follow native async/await, so calls after the first `await` in a
  // tracked action may lose their parent unless the app compiles to ES2016 or
  // lower. Good enough to start; see docs/05 "Notes".
  tracerProvider.register({
    contextManager: new ZoneContextManager(),
    propagator: new W3CTraceContextPropagator(),
  });

  const loggerProvider = new LoggerProvider({
    resource,
    processors: [new BatchLogRecordProcessor({ exporter: new OTLPLogExporter({ url: `${base}/v1/logs` }) })],
  });
  logs.setGlobalLoggerProvider(loggerProvider);

  const ignoreOtlp = new RegExp(`${base.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')}/`);
  registerInstrumentations({
    instrumentations: [
      new DocumentLoadInstrumentation(),
      new FetchInstrumentation({
        propagateTraceHeaderCorsUrls: cfg.propagateTo,
        ignoreUrls: [ignoreOtlp],
        applyCustomAttributesOnSpan: (_span, _request, result) => {
          recordApiCall((result as { status?: number }).status ?? 0);
        },
      }),
      new XMLHttpRequestInstrumentation({
        propagateTraceHeaderCorsUrls: cfg.propagateTo,
        ignoreUrls: [ignoreOtlp],
        applyCustomAttributesOnSpan: (_span, xhr) => {
          recordApiCall(xhr.status);
        },
      }),
    ],
  });

  // Browsers kill pages without warning; flush whenever the tab is hidden.
  addEventListener('visibilitychange', () => {
    if (document.visibilityState === 'hidden') {
      void tracerProvider.forceFlush();
      void loggerProvider.forceFlush();
    }
  });
}
