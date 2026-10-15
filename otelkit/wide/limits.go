package wide

import (
	"unicode/utf8"

	"go.opentelemetry.io/otel/attribute"

	"github.com/benhastings/o11y-standards/otelkit/attrs"
)

// Limits bound wide-event size (docs/02-logging-standard-wide-events.md).
type Limits struct {
	// HardBytes is the approximate encoded size above which attributes are
	// dropped. The Weaver gate fails events above 16 KiB.
	HardBytes int
	// MaxStringLen caps each string value, in bytes.
	MaxStringLen int
	// MaxSliceLen caps each array value, in elements.
	MaxSliceLen int
}

// DefaultLimits matches the logging standard: 8 KiB target, 16 KiB hard limit.
// The target is advisory and enforced by review and the gate, not by code.
var DefaultLimits = Limits{HardBytes: 16 << 10, MaxStringLen: 1024, MaxSliceLen: 32}

// protected attributes are never dropped to fit the hard limit.
var protected = map[attribute.Key]bool{
	attrs.HTTPRequestMethod:      true,
	attrs.HTTPRoute:              true,
	attrs.HTTPResponseStatusCode: true,
	attrs.Kfo11yJobName:          true,
	attrs.Kfo11yOutcome:          true,
	attrs.ErrorType:              true,
	attrs.Kfo11yDuration:         true,
	attrs.UserID:                 true,
	attrs.Kfo11yUserTeamID:       true,
}

func applyLimits(kvs []attribute.KeyValue, l Limits) []attribute.KeyValue {
	changed := false
	for i := range kvs {
		if v, ok := clip(kvs[i].Value, l); ok {
			kvs[i].Value = v
			changed = true
		}
	}
	dropped := 0
	for approxSize(kvs) > l.HardBytes {
		idx := lastDroppable(kvs)
		if idx < 0 {
			break
		}
		kvs = append(kvs[:idx], kvs[idx+1:]...)
		dropped++
	}
	if changed || dropped > 0 {
		kvs = append(kvs, attrs.Kfo11yWideEventTruncated.Bool(true))
	}
	if dropped > 0 {
		kvs = append(kvs, attrs.Kfo11yWideEventDroppedCount.Int(dropped))
	}
	return kvs
}

func clip(v attribute.Value, l Limits) (attribute.Value, bool) {
	switch v.Type() {
	case attribute.STRING:
		if s := v.AsString(); len(s) > l.MaxStringLen {
			return attribute.StringValue(truncate(s, l.MaxStringLen)), true
		}
	case attribute.STRINGSLICE:
		ss, changed := v.AsStringSlice(), false
		if len(ss) > l.MaxSliceLen {
			ss, changed = ss[:l.MaxSliceLen], true
		}
		for i, s := range ss {
			if len(s) > l.MaxStringLen {
				ss[i], changed = truncate(s, l.MaxStringLen), true
			}
		}
		if changed {
			return attribute.StringSliceValue(ss), true
		}
	case attribute.INT64SLICE:
		if s := v.AsInt64Slice(); len(s) > l.MaxSliceLen {
			return attribute.Int64SliceValue(s[:l.MaxSliceLen]), true
		}
	case attribute.FLOAT64SLICE:
		if s := v.AsFloat64Slice(); len(s) > l.MaxSliceLen {
			return attribute.Float64SliceValue(s[:l.MaxSliceLen]), true
		}
	case attribute.BOOLSLICE:
		if s := v.AsBoolSlice(); len(s) > l.MaxSliceLen {
			return attribute.BoolSliceValue(s[:l.MaxSliceLen]), true
		}
	}
	return v, false
}

// truncate cuts s to at most n bytes on a UTF-8 boundary and marks the cut.
func truncate(s string, n int) string {
	const mark = "…"
	n -= len(mark)
	if n <= 0 {
		return mark
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n] + mark
}

// approxSize estimates the JSON-encoded size of the attributes.
func approxSize(kvs []attribute.KeyValue) int {
	total := 0
	for _, kv := range kvs {
		total += len(kv.Key) + 6 + len(kv.Value.Emit())
	}
	return total
}

func lastDroppable(kvs []attribute.KeyValue) int {
	for i := len(kvs) - 1; i >= 0; i-- {
		if !protected[kvs[i].Key] {
			return i
		}
	}
	return -1
}
