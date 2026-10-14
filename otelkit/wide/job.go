package wide

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"gitlab.example.com/sre/otelkit/attrs"
)

// Run executes one unit of background work (a scheduled job, a cache refresh,
// a queue message) under an INTERNAL span and emits one kfo11y.job.summary
// event. name must be low-cardinality: it becomes the span name and
// kfo11y.job.name.
//
// Starting from an INTERNAL span means outbound calls made by fn are CLIENT
// children, never CLIENT roots (which the gate rejects).
func Run(ctx context.Context, name string, fn func(context.Context) error) error {
	tracer := otel.Tracer(scopeName, trace.WithInstrumentationVersion(scopeVersion))
	ctx, span := tracer.Start(ctx, name, trace.WithSpanKind(trace.SpanKindInternal))
	defer span.End()

	ctx, ev := Start(ctx, attrs.EventJobSummary)
	ev.Set(attrs.Kfo11yJobName.String(name))

	err := fn(ctx)
	if err != nil {
		ev.SetError(err)
		if ev.outcomeUnset() {
			ev.SetOutcome(attrs.OutcomeServerError)
		}
		span.SetStatus(codes.Error, ErrorType(err))
	} else if ev.outcomeUnset() {
		ev.SetOutcome(attrs.OutcomeSuccess)
	}
	ev.Emit(ctx)
	return err
}
