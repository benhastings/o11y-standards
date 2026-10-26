package wide

import (
	"context"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	otellog "go.opentelemetry.io/otel/log"
)

// Log emits one named, point-in-time event: something that happens outside a
// request or job summary, such as a cache reload, a search index activation
// or a certificate rotation. name must be an event defined in the registry;
// detail about a request or job belongs on its wide event instead.
//
// The record is correlated to the span in ctx, and DefaultLimits apply.
//
//	wide.Log(ctx, attrs.EventSearchIndexActivation, otellog.SeverityInfo,
//		attrs.Kfo11ySearchIndexName.String("documents"),
//		attrs.Kfo11ySearchIndexRevision.String(rev),
//		attrs.Kfo11yOutcome.String(string(attrs.OutcomeSuccess)))
func Log(ctx context.Context, name string, sev otellog.Severity, kvs ...attribute.KeyValue) {
	kvs = applyLimits(append([]attribute.KeyValue(nil), kvs...), DefaultLimits)
	emit(ctx, name, sev, name, kvs)
}

// emit sends one log-based event through the global LoggerProvider.
func emit(ctx context.Context, name string, sev otellog.Severity, body string, kvs []attribute.KeyValue) {
	now := time.Now()
	var rec otellog.Record
	rec.SetEventName(name)
	rec.SetTimestamp(now)
	rec.SetObservedTimestamp(now)
	rec.SetSeverity(sev)
	rec.SetSeverityText(sev.String())
	rec.SetBody(attribute.StringValue(body))
	rec.AddAttributes(kvs...)

	otel.Logger(scopeName, otellog.WithInstrumentationVersion(scopeVersion)).Emit(ctx, rec)
}
