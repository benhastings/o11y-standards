// Package wide implements the wide-event (canonical log) pattern: accumulate
// context for one unit of work, then emit exactly one OpenTelemetry
// log-based event, correlated to the active span, when the work finishes.
//
// All Event methods are nil-safe, so library code can enrich whatever event
// is in the context without checking:
//
//	wide.FromContext(ctx).Set(attrs.Kfo11yCacheHit.Bool(hit))
package wide

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	otellog "go.opentelemetry.io/otel/log"
	"go.opentelemetry.io/otel/trace"

	"github.com/benhastings/o11y-standards/otelkit/attrs"
)

const (
	scopeName    = "github.com/benhastings/o11y-standards/otelkit/wide"
	scopeVersion = "0.1.0"
)

// promoted lists event attributes that are also copied onto the active span,
// so a trace search can filter on them.
var promoted = map[attribute.Key]bool{
	attrs.UserID:              true,
	attrs.Kfo11yUserTeamID:    true,
	attrs.Kfo11yOutcome:       true,
	attrs.ErrorType:           true,
	attrs.Kfo11yAuthzDecision: true,
}

// Event accumulates attributes for one unit of work.
type Event struct {
	name  string
	start time.Time

	mu        sync.Mutex
	keys      []attribute.Key
	vals      map[attribute.Key]attribute.Value
	counts    map[attribute.Key]int64
	durations map[attribute.Key]time.Duration
	outcome   attrs.Outcome
	errType   string
	route     string
	emitted   bool
	limits    Limits
}

type ctxKey struct{}

// Start creates an event and stores it in the returned context.
func Start(ctx context.Context, name string) (context.Context, *Event) {
	e := &Event{
		name:      name,
		start:     time.Now(),
		vals:      map[attribute.Key]attribute.Value{},
		counts:    map[attribute.Key]int64{},
		durations: map[attribute.Key]time.Duration{},
		limits:    DefaultLimits,
	}
	return context.WithValue(ctx, ctxKey{}, e), e
}

// FromContext returns the event in ctx, or nil. Methods on a nil *Event are no-ops.
func FromContext(ctx context.Context) *Event {
	e, _ := ctx.Value(ctxKey{}).(*Event)
	return e
}

// Set adds or replaces attributes. kfo11y.outcome and error.type are routed to
// SetOutcome and SetErrorType; kfo11y.duration is computed and ignored here.
func (e *Event) Set(kvs ...attribute.KeyValue) {
	if e == nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, kv := range kvs {
		switch {
		case !kv.Valid():
		case kv.Key == attrs.Kfo11yOutcome:
			e.outcome = attrs.Outcome(kv.Value.AsString())
		case kv.Key == attrs.ErrorType:
			e.errType = kv.Value.AsString()
		case kv.Key == attrs.Kfo11yDuration:
		default:
			if _, seen := e.vals[kv.Key]; !seen {
				e.keys = append(e.keys, kv.Key)
			}
			e.vals[kv.Key] = kv.Value
		}
	}
}

// Inc adds delta to an integer counter attribute (e.g. kfo11y.db.query.count).
func (e *Event) Inc(key attribute.Key, delta int64) {
	if e == nil {
		return
	}
	e.mu.Lock()
	e.counts[key] += delta
	e.mu.Unlock()
}

// AddDuration adds d to a duration attribute, reported in seconds.
func (e *Event) AddDuration(key attribute.Key, d time.Duration) {
	if e == nil {
		return
	}
	e.mu.Lock()
	e.durations[key] += d
	e.mu.Unlock()
}

// Track times one dependency call. It increments count and adds the elapsed
// time to total when the returned func runs:
//
//	done := wide.FromContext(ctx).Track(attrs.Kfo11yDBQueryCount, attrs.Kfo11yDBQueryDuration)
//	defer done()
func (e *Event) Track(count, total attribute.Key) func() {
	if e == nil {
		return func() {}
	}
	start := time.Now()
	return func() {
		e.Inc(count, 1)
		e.AddDuration(total, time.Since(start))
	}
}

// SetOutcome sets kfo11y.outcome. The last call wins.
func (e *Event) SetOutcome(o attrs.Outcome) {
	if e == nil {
		return
	}
	e.mu.Lock()
	e.outcome = o
	e.mu.Unlock()
}

// SetErrorType sets error.type to a low-cardinality value.
func (e *Event) SetErrorType(t string) {
	if e == nil || t == "" {
		return
	}
	e.mu.Lock()
	e.errType = t
	e.mu.Unlock()
}

// SetError records error.type for err (see ErrorType). It does not change the
// outcome: whether an error is upstream_error or server_error is yours to say.
func (e *Event) SetError(err error) {
	e.SetErrorType(ErrorType(err))
}

// SetLimits overrides DefaultLimits for this event.
func (e *Event) SetLimits(l Limits) {
	if e == nil {
		return
	}
	e.mu.Lock()
	e.limits = l
	e.mu.Unlock()
}

// Typed lets an error report its own low-cardinality error.type.
type Typed interface{ ErrorType() string }

// ErrorType maps err to a low-cardinality error.type value: the ErrorType()
// of the first error in the chain that implements Typed, "timeout" or
// "canceled" for context errors, otherwise "_OTHER".
func ErrorType(err error) string {
	var t Typed
	switch {
	case err == nil:
		return ""
	case errors.As(err, &t):
		return t.ErrorType()
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	case errors.Is(err, context.Canceled):
		return "canceled"
	default:
		return "_OTHER"
	}
}

// Emit sends the event once. Later calls are no-ops. ctx must carry the span
// the work ran under, so the log record is correlated to it.
func (e *Event) Emit(ctx context.Context) {
	if e == nil {
		return
	}
	e.mu.Lock()
	if e.emitted {
		e.mu.Unlock()
		return
	}
	e.emitted = true
	kvs := e.snapshotLocked()
	outcome, errType, limits := e.outcome, e.errType, e.limits
	e.mu.Unlock()

	kvs = applyLimits(kvs, limits)
	promote(ctx, kvs, outcome, errType)

	sev, sevText := severity(outcome)
	now := time.Now()
	var rec otellog.Record
	rec.SetEventName(e.name)
	rec.SetTimestamp(now)
	rec.SetObservedTimestamp(now)
	rec.SetSeverity(sev)
	rec.SetSeverityText(sevText)
	rec.SetBody(attribute.StringValue(summary(kvs)))
	rec.AddAttributes(kvs...)

	otel.Logger(scopeName, otellog.WithInstrumentationVersion(scopeVersion)).Emit(ctx, rec)
}

func (e *Event) snapshotLocked() []attribute.KeyValue {
	out := make([]attribute.KeyValue, 0, len(e.keys)+len(e.counts)+len(e.durations)+3)
	for _, k := range e.keys {
		out = append(out, attribute.KeyValue{Key: k, Value: e.vals[k]})
	}
	for _, k := range sortedKeys(e.counts) {
		out = append(out, k.Int64(e.counts[k]))
	}
	for _, k := range sortedKeys(e.durations) {
		out = append(out, k.Float64(e.durations[k].Seconds()))
	}
	if e.outcome != "" {
		out = append(out, attrs.Kfo11yOutcome.String(string(e.outcome)))
	}
	if e.errType != "" {
		out = append(out, attrs.ErrorType.String(e.errType))
	}
	out = append(out, attrs.Kfo11yDuration.Float64(time.Since(e.start).Seconds()))
	return out
}

func (e *Event) setRoute(route string) {
	if e == nil {
		return
	}
	e.mu.Lock()
	e.route = route
	e.mu.Unlock()
}

func (e *Event) routeOverride() string {
	if e == nil {
		return ""
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.route
}

func (e *Event) outcomeUnset() bool {
	if e == nil {
		return false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.outcome == ""
}

func (e *Event) errorTypeUnset() bool {
	if e == nil {
		return false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.errType == ""
}

func promote(ctx context.Context, kvs []attribute.KeyValue, outcome attrs.Outcome, errType string) {
	span := trace.SpanFromContext(ctx)
	if !span.IsRecording() {
		return
	}
	for _, kv := range kvs {
		if promoted[kv.Key] {
			span.SetAttributes(kv)
		}
	}
	if outcome == attrs.OutcomeServerError || outcome == attrs.OutcomeUpstreamError {
		span.SetStatus(codes.Error, errType)
	}
}

func severity(o attrs.Outcome) (otellog.Severity, string) {
	switch o {
	case attrs.OutcomeServerError, attrs.OutcomeUpstreamError:
		return otellog.SeverityError, "ERROR"
	case attrs.OutcomeClientError:
		return otellog.SeverityWarn, "WARN"
	default:
		return otellog.SeverityInfo, "INFO"
	}
}

// summary is a short, human-scannable body. The attributes are the record.
func summary(kvs []attribute.KeyValue) string {
	pick := map[attribute.Key]string{}
	for _, kv := range kvs {
		switch kv.Key {
		case attrs.HTTPRequestMethod, attrs.HTTPRoute, attrs.HTTPResponseStatusCode,
			attrs.Kfo11yJobName, attrs.Kfo11yOutcome, attrs.ErrorType:
			pick[kv.Key] = kv.Value.Emit()
		}
	}
	var parts []string
	for _, k := range []attribute.Key{
		attrs.HTTPRequestMethod, attrs.HTTPRoute, attrs.Kfo11yJobName,
		attrs.HTTPResponseStatusCode, attrs.Kfo11yOutcome, attrs.ErrorType,
	} {
		if v := pick[k]; v != "" {
			parts = append(parts, v)
		}
	}
	return strings.Join(parts, " ")
}

func sortedKeys[V any](m map[attribute.Key]V) []attribute.Key {
	keys := make([]attribute.Key, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	return keys
}
