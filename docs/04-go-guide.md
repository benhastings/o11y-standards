# 04 — Go Instrumentation Guide (otelkit)

`otelkit` gives a Go service the whole standard in about ten lines: provider
setup, HTTP tracing and metrics with correct route naming, and one wide event
per request or job.

## Install

```sh
go get github.com/benhastings/o11y-standards/otelkit@v0.1.0
```

Requires Go 1.26+ and OpenTelemetry Go v1.47.0+ (the first release with a
stable Logs API).

## 1. Set up once, in `main`

```go
func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	shutdown, err := otelkit.Setup(ctx, otelkit.Config{
		ServiceName:      "policy-api",
		ServiceNamespace: "policy",
		Team:             "policy-team",
	})
	if err != nil {
		log.Fatalf("telemetry setup: %v", err)
	}
	defer func() {
		sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = shutdown(sctx) // flushes wide events, metrics, traces
	}()

	srv := &http.Server{Addr: ":8080", Handler: wide.Handler(routes())}
	// ... ListenAndServe, wait for ctx.Done(), srv.Shutdown
}
```

Everything in `Config` can be overridden by the environment; see the
deployment snippet in [01](01-telemetry-standard.md#resource-attributes).
Exporter settings come from the standard `OTEL_*` variables, so nothing about
endpoints or protocols is hard-coded.

## 2. Wrap your router

```go
mux := http.NewServeMux()
mux.Handle("GET /v1/policies/{id}", authn(http.HandlerFunc(getPolicy)))
mux.Handle("POST /v1/authz/allow", authn(http.HandlerFunc(allow)))

handler := wide.Handler(mux)
```

`wide.Handler` creates the SERVER span, names it `GET /v1/policies/{id}`,
adds `http.route` to the span and to `http.server.request.duration`, and emits
`kfo11y.request.summary` when the request finishes, including on panic.

Put `wide.Handler` directly around the `ServeMux`. Route detection reads the
pattern ServeMux sets on the request, so a middleware in between that copies
the request (`r.WithContext(...)`) hides it. Wrap individual routes instead,
as `authn` does above, or call `wide.SetRoute`.

Using chi or another router? Report the template after routing:

```go
r.Use(func(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		next.ServeHTTP(w, req)
		wide.SetRoute(req.Context(), chi.RouteContext(req.Context()).RoutePattern())
	})
})
```

## 3. Enrich the event where the knowledge lives

Get the event from the context and add to it. All methods are nil-safe.

```go
// Auth: as soon as you know who's calling.
wide.FromContext(ctx).Set(
	attrs.UserID.String(claims.Subject),
	attrs.Kfo11yUserTeamID.String(claims.Team),
)

// Cache.
wide.FromContext(ctx).Set(attrs.Kfo11yCacheHit.Bool(hit))

// Database: count and time every query.
done := wide.FromContext(ctx).Track(attrs.Kfo11yDBQueryCount, attrs.Kfo11yDBQueryDuration)
rows, err := db.QueryContext(ctx, q, args...)
done()

// Authorization decision. A deny is a correct answer, not an error.
ev := wide.FromContext(ctx)
ev.Set(attrs.Kfo11yAuthzDecision.String(string(attrs.DecisionDeny)),
	attrs.Kfo11yAuthzPolicyID.String(p.ID))
ev.SetOutcome(attrs.OutcomeDenied)
```

Put the `Set`/`Track` calls in your shared packages (auth middleware, DB
wrapper, cache client) once, and every handler gets them for free.

## 4. Classify errors

`error.type` must be low-cardinality. Give your error types a name:

```go
type UpstreamTimeoutError struct{ Dep string }

func (e *UpstreamTimeoutError) Error() string     { return e.Dep + " timed out" }
func (e *UpstreamTimeoutError) ErrorType() string { return "upstream_timeout" }
```

```go
if err != nil {
	ev := wide.FromContext(ctx)
	ev.SetError(err)                          // error.type = "upstream_timeout"
	ev.SetOutcome(attrs.OutcomeUpstreamError) // you decide whose fault it was
	http.Error(w, "unavailable", http.StatusServiceUnavailable)
	return
}
```

Without an `ErrorType()` method, `SetError` records `timeout`, `canceled`, or
`_OTHER`. If you set nothing, a 5xx response gets the status code as
`error.type` and outcome `server_error`.

## 5. Background work

```go
err := wide.Run(ctx, "policy-cache-refresh", func(ctx context.Context) error {
	// Outbound calls in here become children of an INTERNAL span,
	// and one kfo11y.job.summary is emitted at the end.
	return refresh(ctx)
})
```

## 6. Outbound HTTP

Use an instrumented transport so the downstream service joins the trace:

```go
client := &http.Client{Transport: otelhttp.NewTransport(http.DefaultTransport)}
```

## Do and don't

| Do | Don't |
|---|---|
| Use keys from the `attrs` package | Type attribute names as string literals |
| Put IDs in attribute values | Put IDs in span names or routes |
| Record a deny as `OutcomeDenied` | Treat a correct "no" as an error |
| Store a reference to a large payload | Put request bodies, stack traces, or tokens on events |
| Add detail to the wide event | Call `span.AddEvent` / `span.RecordException` (deprecated API) |
| Call `shutdown` on exit | `os.Exit` without flushing |

## Local debugging

```sh
OTEL_TRACES_EXPORTER=console OTEL_LOGS_EXPORTER=console OTEL_METRICS_EXPORTER=none go run ./cmd/server
```

To check your telemetry against the standard before pushing, run the gate
locally ([03](03-weaver-gate.md#running-it-locally)). To see it land in
ClickHouse and try the queries in [02](02-logging-standard-wide-events.md#querying-wide-events),
run the `dev/` stack (README).
