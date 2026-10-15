package otelkit

import (
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"

	"github.com/benhastings/o11y-standards/otelkit/attrs"
)

// metricViews keep metric attributes low-cardinality (docs/01, "SDK metrics").
//
// otelhttp records server.address and server.port on the http.server.*
// metrics. Semconv makes them opt-in there: they come from the Host header,
// which the client controls, so they can inflate cardinality. They stay on
// the SERVER span.
func metricViews() []sdkmetric.View {
	return []sdkmetric.View{
		sdkmetric.NewView(
			sdkmetric.Instrument{Name: "http.server.*"},
			sdkmetric.Stream{AttributeFilter: attribute.NewDenyKeysFilter(attrs.ServerAddress, attrs.ServerPort)},
		),
	}
}
