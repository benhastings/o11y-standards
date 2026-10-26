package wide

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/benhastings/o11y-standards/otelkit/attrs"
)

// Step runs fn under an INTERNAL child span named name: a part of the current
// request or job worth seeing on its own in a trace (a ranking pass, a
// fan-out, a slow computation). name must be low-cardinality.
//
// fn's context carries the new span, so outbound calls made in fn nest under
// it, and FromContext still returns the request's or job's wide event. Add
// span-only detail with trace.SpanFromContext(ctx).SetAttributes.
//
// If fn returns an error, the span gets status Error and error.type, and no
// span event (RecordError belongs to the deprecated Span Event API). The error
// is returned unchanged; recording it on the wide event and choosing the
// outcome stay with the caller.
func Step(ctx context.Context, name string, fn func(context.Context) error) error {
	tracer := otel.Tracer(scopeName, trace.WithInstrumentationVersion(scopeVersion))
	ctx, span := tracer.Start(ctx, name, trace.WithSpanKind(trace.SpanKindInternal))
	defer span.End()

	err := fn(ctx)
	if err != nil {
		t := ErrorType(err)
		span.SetAttributes(attrs.ErrorType.String(t))
		span.SetStatus(codes.Error, t)
	}
	return err
}
