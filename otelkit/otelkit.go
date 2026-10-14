// Package otelkit bootstraps OpenTelemetry tracing, metrics, and logs for Go
// services that follow the org telemetry standard
// (docs/01-telemetry-standard.md).
//
// Exporters, endpoints, protocol, and sampling come from the standard OTEL_*
// environment variables, so the same binary exports OTLP/HTTP to the
// collector in Kubernetes and OTLP/gRPC to Weaver live-check in CI.
//
// Use package wide for HTTP middleware and wide events.
package otelkit

import (
	"context"
	"errors"

	"go.opentelemetry.io/contrib/exporters/autoexport"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// Config holds code-level defaults. Environment variables override every
// field, so deployments stay in control.
type Config struct {
	// ServiceName (service.name). OTEL_SERVICE_NAME overrides it.
	ServiceName string
	// ServiceNamespace (service.namespace): the product area, e.g. "policy".
	// Not the Kubernetes namespace, and never environment-specific.
	// SERVICE_NAMESPACE overrides it.
	ServiceNamespace string
	// Team (kfo11y.service.team). SERVICE_TEAM overrides it.
	Team string
	// Version (service.version). SERVICE_VERSION overrides it; falls back to
	// the module version in Go build info.
	Version string
}

// ShutdownFunc flushes buffered telemetry and stops all providers. Call it
// before the process exits, with a deadline.
type ShutdownFunc func(context.Context) error

// Setup configures the global tracer, meter, and logger providers and the
// W3C trace-context and baggage propagators.
func Setup(ctx context.Context, cfg Config) (ShutdownFunc, error) {
	var shutdowns []func(context.Context) error
	shutdown := func(ctx context.Context) error {
		var errs []error
		// Reverse order: logs (wide events) flush first, traces last.
		for i := len(shutdowns) - 1; i >= 0; i-- {
			errs = append(errs, shutdowns[i](ctx))
		}
		return errors.Join(errs...)
	}
	fail := func(err error) (ShutdownFunc, error) {
		return nil, errors.Join(err, shutdown(ctx))
	}

	res, err := NewResource(ctx, cfg)
	if err != nil {
		return fail(err)
	}

	spanExporter, err := autoexport.NewSpanExporter(ctx)
	if err != nil {
		return fail(err)
	}
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(spanExporter),
		sdktrace.WithResource(res),
	)
	shutdowns = append(shutdowns, tp.Shutdown)

	metricReader, err := autoexport.NewMetricReader(ctx)
	if err != nil {
		return fail(err)
	}
	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(metricReader),
		sdkmetric.WithResource(res),
	)
	shutdowns = append(shutdowns, mp.Shutdown)

	logExporter, err := autoexport.NewLogExporter(ctx)
	if err != nil {
		return fail(err)
	}
	lp := sdklog.NewLoggerProvider(
		sdklog.WithProcessor(sdklog.NewBatchProcessor(logExporter)),
		sdklog.WithResource(res),
	)
	shutdowns = append(shutdowns, lp.Shutdown)

	otel.SetTracerProvider(tp)
	otel.SetMeterProvider(mp)
	otel.SetLoggerProvider(lp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))
	return shutdown, nil
}
