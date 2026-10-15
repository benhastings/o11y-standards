package wide

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/trace"

	"github.com/benhastings/o11y-standards/otelkit/attrs"
)

// Handler wraps next (usually your router) with OpenTelemetry server tracing
// and metrics, and emits one kfo11y.request.summary event per request.
//
// With Go's http.ServeMux (method and wildcard patterns) the route template is
// detected automatically. With other routers, call SetRoute from a middleware
// that runs after routing, e.g. for chi:
//
//	wide.SetRoute(r.Context(), chi.RouteContext(r.Context()).RoutePattern())
func Handler(next http.Handler, opts ...otelhttp.Option) http.Handler {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, ev := Start(r.Context(), attrs.EventRequestSummary)
		ev.Set(attrs.HTTPRequestMethod.String(normalizeMethod(r.Method)))
		rw := &responseWriter{ResponseWriter: w}
		r = r.WithContext(ctx)

		defer func() {
			if p := recover(); p != nil {
				ev.SetOutcome(attrs.OutcomeServerError)
				ev.SetErrorType("panic")
				finish(r, ev, rw.statusOr(http.StatusInternalServerError))
				ev.Emit(r.Context())
				panic(p)
			}
		}()

		next.ServeHTTP(rw, r)
		finish(r, ev, rw.statusOr(http.StatusOK))
		ev.Emit(r.Context())
	})

	// Until routing runs, the span is named by method only, per semconv. It is
	// renamed to "{method} {route}" in finish.
	base := []otelhttp.Option{
		otelhttp.WithSpanNameFormatter(func(_ string, r *http.Request) string {
			return normalizeMethod(r.Method)
		}),
	}
	return otelhttp.NewHandler(inner, "http.server", append(base, opts...)...)
}

// SetRoute records the matched route template for routers other than
// http.ServeMux. Pass the template ("/v1/policies/{id}"), never the raw path.
func SetRoute(ctx context.Context, route string) {
	FromContext(ctx).setRoute(route)
}

func finish(r *http.Request, ev *Event, status int) {
	ctx := r.Context()
	ev.Set(attrs.HTTPResponseStatusCode.Int(status))

	route := ev.routeOverride()
	if route == "" {
		route = patternRoute(r.Pattern)
	}
	if route != "" {
		kv := attrs.HTTPRoute.String(route)
		ev.Set(kv)
		span := trace.SpanFromContext(ctx)
		span.SetName(normalizeMethod(r.Method) + " " + route)
		span.SetAttributes(kv)
		if l, ok := otelhttp.LabelerFromContext(ctx); ok {
			l.Add(kv) // adds http.route to the http.server.request.duration metric
		}
	}

	if ev.outcomeUnset() {
		switch {
		case status >= 500:
			ev.SetOutcome(attrs.OutcomeServerError)
		case status >= 400:
			ev.SetOutcome(attrs.OutcomeClientError)
		default:
			ev.SetOutcome(attrs.OutcomeSuccess)
		}
	}
	if status >= 500 && ev.errorTypeUnset() {
		ev.SetErrorType(strconv.Itoa(status)) // semconv: status code as error.type
	}
}

// patternRoute turns a ServeMux pattern ("GET host/v1/x/{id}") into a route
// template ("/v1/x/{id}").
func patternRoute(p string) string {
	if p == "" {
		return ""
	}
	if i := strings.IndexByte(p, ' '); i >= 0 {
		p = strings.TrimSpace(p[i+1:])
	}
	if !strings.HasPrefix(p, "/") {
		if i := strings.IndexByte(p, '/'); i >= 0 {
			p = p[i:]
		}
	}
	return p
}

var knownMethods = map[string]bool{
	http.MethodGet: true, http.MethodHead: true, http.MethodPost: true,
	http.MethodPut: true, http.MethodPatch: true, http.MethodDelete: true,
	http.MethodConnect: true, http.MethodOptions: true, http.MethodTrace: true,
	"QUERY": true,
}

// normalizeMethod returns "_OTHER" for non-standard methods, per semconv,
// so arbitrary client input can't explode cardinality.
func normalizeMethod(m string) string {
	if knownMethods[m] {
		return m
	}
	return "_OTHER"
}

type responseWriter struct {
	http.ResponseWriter
	status int
}

func (w *responseWriter) WriteHeader(code int) {
	if w.status == 0 && code >= 200 {
		w.status = code
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *responseWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(b)
}

func (w *responseWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Unwrap lets http.ResponseController reach the underlying writer.
func (w *responseWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (w *responseWriter) statusOr(def int) int {
	if w.status == 0 {
		return def
	}
	return w.status
}
