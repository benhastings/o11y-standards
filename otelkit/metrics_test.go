package otelkit

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/benhastings/o11y-standards/otelkit/attrs"
	"github.com/benhastings/o11y-standards/otelkit/wide"
)

func TestHTTPServerMetricsDropHostAttributes(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader), sdkmetric.WithView(metricViews()...))
	t.Cleanup(func() { _ = mp.Shutdown(context.Background()) })

	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/documents/{id}", func(w http.ResponseWriter, _ *http.Request) {})
	srv := httptest.NewServer(wide.Handler(mux, otelhttp.WithMeterProvider(mp)))
	t.Cleanup(srv.Close)

	resp, err := srv.Client().Get(srv.URL + "/v1/documents/doc-1")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatal(err)
	}

	checked := 0
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != "http.server.request.duration" {
				continue
			}
			for _, dp := range m.Data.(metricdata.Histogram[float64]).DataPoints {
				checked++
				for _, k := range []attribute.Key{attrs.ServerAddress, attrs.ServerPort} {
					if dp.Attributes.HasValue(k) {
						t.Errorf("http.server.request.duration has %s; it should be dropped", k)
					}
				}
				for _, k := range []attribute.Key{attrs.HTTPRequestMethod, attrs.HTTPRoute, attrs.HTTPResponseStatusCode} {
					if !dp.Attributes.HasValue(k) {
						t.Errorf("http.server.request.duration is missing %s", k)
					}
				}
			}
		}
	}
	if checked == 0 {
		t.Fatal("no http.server.request.duration data points recorded")
	}
}
