//go:build o11ysmoke

// Telemetry smoke test. Run by the Weaver gate (ci/weaver.gitlab-ci.yml):
// it exercises each route once with telemetry exporting to Weaver, so the
// gate can check what the service actually emits.
//
// Copy this file into your service and replace the cases with your routes.
// Locally: start `weaver registry live-check` (docs/03), then
//
//	OTEL_EXPORTER_OTLP_PROTOCOL=grpc go test -tags=o11ysmoke -run TestTelemetrySmoke ./...
package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/benhastings/o11y-standards/otelkit"
	"github.com/benhastings/o11y-standards/otelkit/wide"
)

func TestTelemetrySmoke(t *testing.T) {
	ctx := context.Background()
	shutdown, err := otelkit.Setup(ctx, otelkit.Config{
		ServiceName:      "example-search-api",
		ServiceNamespace: "search",
		Team:             "search-team",
	})
	if err != nil {
		t.Fatalf("telemetry setup: %v", err)
	}

	app := newApp()
	if err := app.activateIndex(ctx, "example-1", exampleSnapshot); err != nil {
		t.Fatalf("search index: %v", err)
	}
	// A failed activation too, so the gate checks the event's failure shape.
	failing := func() ([]document, error) { return nil, context.DeadlineExceeded }
	if err := app.activateIndex(ctx, "example-2", failing); err == nil {
		t.Fatal("failing snapshot activated")
	}

	srv := httptest.NewServer(wide.Handler(app.routes()))

	cases := []struct {
		name, method, path, user, body string
		want                           int
	}{
		{"document found", "GET", "/v1/documents/doc-1", "usr_1", "", 200},
		{"document missing", "GET", "/v1/documents/doc-404", "usr_1", "", 404},
		{"unauthenticated", "GET", "/v1/documents/doc-1", "", "", 401},
		{"search hits", "GET", "/v1/search?q=release", "usr_1", "", 200},
		{"search no results", "GET", "/v1/search?q=zzz", "usr_2", "", 200},
		{"search missing query", "GET", "/v1/search", "usr_1", "", 400},
		{"unknown route", "GET", "/nope", "usr_1", "", 404},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req, _ := http.NewRequest(c.method, srv.URL+c.path, strings.NewReader(c.body))
			if c.user != "" {
				req.Header.Set("X-User-Id", c.user)
			}
			resp, err := srv.Client().Do(req)
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()
			if resp.StatusCode != c.want {
				t.Errorf("status = %d, want %d", resp.StatusCode, c.want)
			}
		})
	}
	srv.Close()

	// One run of each background job, so kfo11y.job.summary is checked too.
	if err := wide.Run(ctx, "search-cache-refresh", func(context.Context) error { return nil }); err != nil {
		t.Errorf("job: %v", err)
	}

	sctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := shutdown(sctx); err != nil {
		t.Fatalf("flushing telemetry (is the OTLP endpoint up?): %v", err)
	}
}
