package wide

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	otellog "go.opentelemetry.io/otel/log"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"github.com/benhastings/o11y-standards/otelkit/attrs"
)

// memExporter keeps exported log records in memory.
type memExporter struct {
	mu   sync.Mutex
	recs []sdklog.Record
}

func (m *memExporter) Export(_ context.Context, recs []sdklog.Record) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, r := range recs {
		m.recs = append(m.recs, r.Clone())
	}
	return nil
}
func (m *memExporter) Shutdown(context.Context) error   { return nil }
func (m *memExporter) ForceFlush(context.Context) error { return nil }

func (m *memExporter) records() []sdklog.Record {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]sdklog.Record(nil), m.recs...)
}

// install sets global providers that record everything, and restores the
// previous ones when the test ends.
func install(t *testing.T) (*memExporter, *tracetest.SpanRecorder) {
	t.Helper()
	logs := &memExporter{}
	spans := tracetest.NewSpanRecorder()
	prevL, prevT := otel.GetLoggerProvider(), otel.GetTracerProvider()
	otel.SetLoggerProvider(sdklog.NewLoggerProvider(sdklog.WithProcessor(sdklog.NewSimpleProcessor(logs))))
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spans)))
	t.Cleanup(func() {
		otel.SetLoggerProvider(prevL)
		otel.SetTracerProvider(prevT)
	})
	return logs, spans
}

func attrMap(r sdklog.Record) map[attribute.Key]attribute.Value {
	m := map[attribute.Key]attribute.Value{}
	r.WalkAttributes(func(kv attribute.KeyValue) bool {
		m[kv.Key] = kv.Value
		return true
	})
	return m
}

func only(t *testing.T, logs *memExporter) (sdklog.Record, map[attribute.Key]attribute.Value) {
	t.Helper()
	recs := logs.records()
	if len(recs) != 1 {
		t.Fatalf("got %d log records, want exactly 1", len(recs))
	}
	return recs[0], attrMap(recs[0])
}

func serve(h http.Handler, method, target string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, target, nil))
	return rec
}

func TestHandlerEmitsOneCorrelatedEvent(t *testing.T) {
	logs, spans := install(t)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/documents/{id}", func(w http.ResponseWriter, r *http.Request) {
		FromContext(r.Context()).Set(attrs.UserID.String("usr_1"))
		w.WriteHeader(http.StatusOK)
	})

	serve(Handler(mux), "GET", "/v1/documents/doc-1")

	rec, a := only(t, logs)
	if rec.EventName() != attrs.EventRequestSummary {
		t.Errorf("EventName = %q", rec.EventName())
	}
	if got := a[attrs.HTTPRoute].AsString(); got != "/v1/documents/{id}" {
		t.Errorf("http.route = %q", got)
	}
	if got := a[attrs.HTTPResponseStatusCode].AsInt64(); got != 200 {
		t.Errorf("status = %d", got)
	}
	if got := a[attrs.Kfo11yOutcome].AsString(); got != "success" {
		t.Errorf("outcome = %q", got)
	}
	if _, ok := a[attrs.Kfo11yDuration]; !ok {
		t.Error("kfo11y.duration missing")
	}

	ended := spans.Ended()
	if len(ended) != 1 {
		t.Fatalf("got %d spans, want 1", len(ended))
	}
	span := ended[0]
	if span.Name() != "GET /v1/documents/{id}" {
		t.Errorf("span name = %q", span.Name())
	}
	if rec.TraceID() != span.SpanContext().TraceID() || rec.SpanID() != span.SpanContext().SpanID() {
		t.Error("log record is not correlated with the server span")
	}
	promoted := map[attribute.Key]bool{}
	for _, kv := range span.Attributes() {
		promoted[kv.Key] = true
	}
	if !promoted[attrs.UserID] || !promoted[attrs.Kfo11yOutcome] || !promoted[attrs.HTTPRoute] {
		t.Errorf("span is missing promoted attributes: %v", span.Attributes())
	}
}

func TestHandlerOutcomeClassification(t *testing.T) {
	cases := []struct {
		name     string
		handler  http.HandlerFunc
		outcome  string
		errType  string
		severity otellog.Severity
	}{
		{"ok", func(w http.ResponseWriter, _ *http.Request) {}, "success", "", otellog.SeverityInfo},
		{"not found", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(404) }, "client_error", "", otellog.SeverityWarn},
		{"server error", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(500) }, "server_error", "500", otellog.SeverityError},
		{"upstream with typed error", func(w http.ResponseWriter, r *http.Request) {
			ev := FromContext(r.Context())
			ev.SetOutcome(attrs.OutcomeUpstreamError)
			ev.SetError(fmt.Errorf("lookup: %w", typedErr("db_unavailable")))
			w.WriteHeader(503)
		}, "upstream_error", "db_unavailable", otellog.SeverityError},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			logs, _ := install(t)
			mux := http.NewServeMux()
			mux.Handle("GET /x", c.handler)
			serve(Handler(mux), "GET", "/x")

			rec, a := only(t, logs)
			if got := a[attrs.Kfo11yOutcome].AsString(); got != c.outcome {
				t.Errorf("outcome = %q, want %q", got, c.outcome)
			}
			if got := a[attrs.ErrorType].AsString(); got != c.errType {
				t.Errorf("error.type = %q, want %q", got, c.errType)
			}
			if rec.Severity() != c.severity {
				t.Errorf("severity = %v, want %v", rec.Severity(), c.severity)
			}
		})
	}
}

func TestHandlerEmitsOnPanic(t *testing.T) {
	logs, _ := install(t)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /boom", func(http.ResponseWriter, *http.Request) { panic("boom") })

	func() {
		defer func() {
			if recover() == nil {
				t.Error("panic was swallowed; it must propagate to net/http")
			}
		}()
		serve(Handler(mux), "GET", "/boom")
	}()

	_, a := only(t, logs)
	if a[attrs.Kfo11yOutcome].AsString() != "server_error" || a[attrs.ErrorType].AsString() != "panic" {
		t.Errorf("outcome/error.type = %q/%q", a[attrs.Kfo11yOutcome].AsString(), a[attrs.ErrorType].AsString())
	}
	if a[attrs.HTTPResponseStatusCode].AsInt64() != 500 {
		t.Errorf("status = %d", a[attrs.HTTPResponseStatusCode].AsInt64())
	}
}

func TestSetRouteOverridesPattern(t *testing.T) {
	logs, _ := install(t)
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		SetRoute(r.Context(), "/v1/things/{id}")
	})
	serve(Handler(h), "GET", "/v1/things/42")

	_, a := only(t, logs)
	if got := a[attrs.HTTPRoute].AsString(); got != "/v1/things/{id}" {
		t.Errorf("http.route = %q", got)
	}
}

func TestUnmatchedRouteHasNoHTTPRoute(t *testing.T) {
	logs, _ := install(t)
	serve(Handler(http.NewServeMux()), "GET", "/nope")

	_, a := only(t, logs)
	if _, ok := a[attrs.HTTPRoute]; ok {
		t.Errorf("unexpected http.route %q on a 404", a[attrs.HTTPRoute].AsString())
	}
}

func TestNonStandardMethodIsOther(t *testing.T) {
	logs, _ := install(t)
	serve(Handler(http.NewServeMux()), "FROB", "/")

	_, a := only(t, logs)
	if got := a[attrs.HTTPRequestMethod].AsString(); got != "_OTHER" {
		t.Errorf("http.request.method = %q", got)
	}
}

func TestPatternRoute(t *testing.T) {
	cases := map[string]string{
		"":                        "",
		"/v1/x":                   "/v1/x",
		"GET /v1/x/{id}":          "/v1/x/{id}",
		"GET api.example.com/v1/": "/v1/",
		"api.example.com/v1/x":    "/v1/x",
		"POST  /spaced":           "/spaced",
	}
	for in, want := range cases {
		if got := patternRoute(in); got != want {
			t.Errorf("patternRoute(%q) = %q, want %q", in, got, want)
		}
	}
}

type typedErr string

func (e typedErr) Error() string     { return string(e) }
func (e typedErr) ErrorType() string { return string(e) }

func TestErrorType(t *testing.T) {
	cases := []struct {
		err  error
		want string
	}{
		{nil, ""},
		{errors.New("x"), "_OTHER"},
		{typedErr("db_unavailable"), "db_unavailable"},
		{fmt.Errorf("wrap: %w", typedErr("cache_timeout")), "cache_timeout"},
		{fmt.Errorf("wrap: %w", context.DeadlineExceeded), "timeout"},
		{context.Canceled, "canceled"},
	}
	for _, c := range cases {
		if got := ErrorType(c.err); got != c.want {
			t.Errorf("ErrorType(%v) = %q, want %q", c.err, got, c.want)
		}
	}
}

func TestNilEventIsSafe(t *testing.T) {
	var e *Event
	if FromContext(context.Background()) != nil {
		t.Fatal("FromContext on an empty context should be nil")
	}
	e.Set(attrs.UserID.String("u"))
	e.Inc(attrs.Kfo11yDBQueryCount, 1)
	e.AddDuration(attrs.Kfo11yDBQueryDuration, 1)
	e.Track(attrs.Kfo11yDBQueryCount, attrs.Kfo11yDBQueryDuration)()
	e.SetOutcome(attrs.OutcomeSuccess)
	e.SetErrorType("x")
	e.SetError(errors.New("x"))
	e.SetLimits(DefaultLimits)
	e.Emit(context.Background())
	SetRoute(context.Background(), "/x")
}

func TestEmitOnce(t *testing.T) {
	logs, _ := install(t)
	ctx, ev := Start(context.Background(), attrs.EventJobSummary)
	ev.Emit(ctx)
	ev.Emit(ctx)
	if n := len(logs.records()); n != 1 {
		t.Errorf("got %d records, want 1", n)
	}
}

func TestCountersAndDurations(t *testing.T) {
	logs, _ := install(t)
	ctx, ev := Start(context.Background(), attrs.EventJobSummary)
	ev.Track(attrs.Kfo11yDBQueryCount, attrs.Kfo11yDBQueryDuration)()
	ev.Track(attrs.Kfo11yDBQueryCount, attrs.Kfo11yDBQueryDuration)()
	ev.Set(attrs.Kfo11yDuration.Float64(999)) // computed, not settable
	ev.Emit(ctx)

	_, a := only(t, logs)
	if got := a[attrs.Kfo11yDBQueryCount].AsInt64(); got != 2 {
		t.Errorf("db.query.count = %d", got)
	}
	if _, ok := a[attrs.Kfo11yDBQueryDuration]; !ok {
		t.Error("db.query.duration missing")
	}
	if got := a[attrs.Kfo11yDuration].AsFloat64(); got >= 999 {
		t.Errorf("kfo11y.duration = %v; Set must not override it", got)
	}
}

func TestStringAndSliceTruncation(t *testing.T) {
	l := Limits{HardBytes: 1 << 20, MaxStringLen: 16, MaxSliceLen: 2}
	kvs := applyLimits([]attribute.KeyValue{
		attribute.String("kfo11y.long", strings.Repeat("a", 100)),
		attribute.StringSlice("kfo11y.many", []string{"a", "b", "c"}),
		attribute.String("kfo11y.short", "ok"),
	}, l)
	a := map[attribute.Key]attribute.Value{}
	for _, kv := range kvs {
		a[kv.Key] = kv.Value
	}
	if s := a["kfo11y.long"].AsString(); len(s) > 16 || !strings.HasSuffix(s, "…") {
		t.Errorf("long string not truncated: %q", s)
	}
	if n := len(a["kfo11y.many"].AsStringSlice()); n != 2 {
		t.Errorf("slice len = %d", n)
	}
	if !a[attrs.Kfo11yWideEventTruncated].AsBool() {
		t.Error("truncated flag not set")
	}
	if _, ok := a[attrs.Kfo11yWideEventDroppedCount]; ok {
		t.Error("nothing was dropped, but the dropped count is set")
	}
}

func TestHardLimitDropsOnlyUnprotected(t *testing.T) {
	l := Limits{HardBytes: 200, MaxStringLen: 1024, MaxSliceLen: 32}
	in := []attribute.KeyValue{
		attrs.HTTPRequestMethod.String("GET"),
		attrs.Kfo11yOutcome.String("success"),
		attrs.UserID.String("usr_1"),
	}
	for i := range 10 {
		in = append(in, attribute.String(fmt.Sprintf("kfo11y.filler%d", i), strings.Repeat("x", 50)))
	}
	kvs := applyLimits(in, l)
	a := map[attribute.Key]attribute.Value{}
	for _, kv := range kvs {
		a[kv.Key] = kv.Value
	}
	for _, k := range []attribute.Key{attrs.HTTPRequestMethod, attrs.Kfo11yOutcome, attrs.UserID} {
		if _, ok := a[k]; !ok {
			t.Errorf("protected attribute %s was dropped", k)
		}
	}
	if a[attrs.Kfo11yWideEventDroppedCount].AsInt64() == 0 || !a[attrs.Kfo11yWideEventTruncated].AsBool() {
		t.Error("dropped count / truncated flag not set")
	}
	if approxSize(kvs) > l.HardBytes+64 { // the two flags are appended after the check
		t.Errorf("approx size %d still well over the limit %d", approxSize(kvs), l.HardBytes)
	}
}

func TestTruncateKeepsUTF8Valid(t *testing.T) {
	s := strings.Repeat("é", 20) // 2 bytes per rune
	for n := 1; n < 12; n++ {
		got := truncate(s, n)
		if !utf8.ValidString(got) {
			t.Errorf("truncate(%d) produced invalid UTF-8: %q", n, got)
		}
		if len(got) > n && got != "…" {
			t.Errorf("truncate(%d) = %d bytes", n, len(got))
		}
	}
}

func TestRunClassifiesOutcome(t *testing.T) {
	logs, spans := install(t)
	_ = Run(context.Background(), "ok-job", func(context.Context) error { return nil })
	err := Run(context.Background(), "bad-job", func(context.Context) error { return typedErr("export_failed") })
	if err == nil || err.Error() != "export_failed" {
		t.Errorf("Run must return fn's error, got %v", err)
	}

	recs := logs.records()
	if len(recs) != 2 {
		t.Fatalf("got %d records, want 2", len(recs))
	}
	ok, bad := attrMap(recs[0]), attrMap(recs[1])
	if ok[attrs.Kfo11yJobName].AsString() != "ok-job" || ok[attrs.Kfo11yOutcome].AsString() != "success" {
		t.Errorf("ok job: %v", ok)
	}
	if bad[attrs.Kfo11yOutcome].AsString() != "server_error" || bad[attrs.ErrorType].AsString() != "export_failed" {
		t.Errorf("bad job: %v", bad)
	}
	if recs[0].EventName() != attrs.EventJobSummary {
		t.Errorf("EventName = %q", recs[0].EventName())
	}
	if n := len(spans.Ended()); n != 2 {
		t.Errorf("got %d spans, want 2 INTERNAL roots", n)
	}
}

func TestStepNestsUnderParentAndKeepsTheEvent(t *testing.T) {
	logs, spans := install(t)
	err := Run(context.Background(), "job", func(ctx context.Context) error {
		return Step(ctx, "rank", func(ctx context.Context) error {
			FromContext(ctx).Set(attrs.Kfo11ySearchIndexName.String("documents"))
			return nil
		})
	})
	if err != nil {
		t.Fatal(err)
	}

	ended := spans.Ended()
	if len(ended) != 2 {
		t.Fatalf("got %d spans, want 2", len(ended))
	}
	step, job := ended[0], ended[1]
	if step.Name() != "rank" || step.SpanKind() != trace.SpanKindInternal {
		t.Errorf("step span = %q kind %v", step.Name(), step.SpanKind())
	}
	if step.Parent().SpanID() != job.SpanContext().SpanID() {
		t.Error("step span is not a child of the job span")
	}
	if step.Status().Code != codes.Unset {
		t.Errorf("status = %v", step.Status())
	}
	_, a := only(t, logs)
	if a[attrs.Kfo11ySearchIndexName].AsString() != "documents" {
		t.Error("attribute set inside Step did not reach the job's wide event")
	}
}

func TestStepErrorSetsStatusWithoutSpanEvents(t *testing.T) {
	_, spans := install(t)
	err := Step(context.Background(), "rank", func(context.Context) error {
		return fmt.Errorf("wrap: %w", typedErr("index_missing"))
	})
	if err == nil || err.Error() != "wrap: index_missing" {
		t.Errorf("Step must return fn's error, got %v", err)
	}

	span := spans.Ended()[0]
	if span.Status().Code != codes.Error || span.Status().Description != "index_missing" {
		t.Errorf("status = %v", span.Status())
	}
	var errType string
	for _, kv := range span.Attributes() {
		if kv.Key == attrs.ErrorType {
			errType = kv.Value.AsString()
		}
	}
	if errType != "index_missing" {
		t.Errorf("error.type = %q", errType)
	}
	if n := len(span.Events()); n != 0 {
		t.Errorf("got %d span events, want none", n)
	}
}

func TestLogEmitsNamedCorrelatedEvent(t *testing.T) {
	logs, spans := install(t)
	ctx, span := otel.Tracer("test").Start(context.Background(), "parent")
	Log(ctx, "kfo11y.test.happened", otellog.SeverityWarn,
		attrs.Kfo11ySearchIndexName.String("documents"),
		attribute.String("kfo11y.long", strings.Repeat("a", 2000)),
	)
	span.End()

	rec, a := only(t, logs)
	if rec.EventName() != "kfo11y.test.happened" {
		t.Errorf("EventName = %q", rec.EventName())
	}
	if rec.Severity() != otellog.SeverityWarn || rec.SeverityText() != "WARN" {
		t.Errorf("severity = %v / %q", rec.Severity(), rec.SeverityText())
	}
	if rec.TraceID() != spans.Ended()[0].SpanContext().TraceID() {
		t.Error("log record is not correlated with the span in ctx")
	}
	if a[attrs.Kfo11ySearchIndexName].AsString() != "documents" {
		t.Error("attribute missing")
	}
	if !a[attrs.Kfo11yWideEventTruncated].AsBool() {
		t.Error("limits were not applied")
	}
}
