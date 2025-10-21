package gclog

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/go-kratos/kratos/v2/log"
	"go.opentelemetry.io/otel/trace"
)

// AppendTrace appends Cloud Logging compatible trace/span identifiers to keyvals.
func AppendTrace(ctx context.Context, project string, kvs []interface{}) []interface{} {
	sc := trace.SpanContextFromContext(ctx)
	if sc.HasTraceID() && project != "" {
		traceValue := fmt.Sprintf("projects/%s/traces/%s", project, sc.TraceID())
		kvs = append(kvs, "trace_id", traceValue)
	}
	if sc.HasSpanID() {
		kvs = append(kvs, "span_id", sc.SpanID().String())
	}
	return kvs
}

// WithTrace returns a logger bound to the supplied context and span information.
func WithTrace(ctx context.Context, project string, base log.Logger) log.Logger {
	kvs := AppendTrace(ctx, project, nil)
	if len(kvs) == 0 {
		return log.WithContext(ctx, base)
	}
	return log.WithContext(ctx, log.With(base, kvs...))
}

// WithComponent adds a component label to the logger and returns the updated logger.
func WithComponent(logger log.Logger, component string) log.Logger {
	if component == "" {
		return logger
	}
	return log.With(logger, "component", component)
}

// WithPayload attaches a payload map to the logger.
func WithPayload(logger log.Logger, payload map[string]any) log.Logger {
	if payload == nil {
		payload = map[string]any{}
	}
	return log.With(logger, "payload", payload)
}

// SeverityFromHTTP converts HTTP status codes to log levels.
func SeverityFromHTTP(status int) log.Level {
	switch {
	case status >= http.StatusInternalServerError:
		return log.LevelError
	case status >= http.StatusBadRequest:
		return log.LevelWarn
	case status >= http.StatusMultipleChoices:
		return log.LevelInfo
	default:
		return log.LevelInfo
	}
}

// Helper wraps log.Helper with convenience helpers.
type Helper struct {
	base   log.Logger
	helper *log.Helper
}

// NewHelper constructs a Helper.
func NewHelper(logger log.Logger) *Helper {
	return &Helper{
		base:   logger,
		helper: log.NewHelper(logger),
	}
}

// Logger exposes the underlying logger.
func (h *Helper) Logger() log.Logger {
	return h.base
}

// WithComponent returns a helper with the component label applied.
func (h *Helper) WithComponent(component string) *Helper {
	return NewHelper(WithComponent(h.base, component))
}

// WithPayload returns a helper with payload pre-attached.
func (h *Helper) WithPayload(payload map[string]any) *Helper {
	return NewHelper(WithPayload(h.base, payload))
}

// InfoWithPayload logs an info message with structured payload.
func (h *Helper) InfoWithPayload(msg string, payload map[string]any, kvs ...interface{}) {
	if payload == nil {
		payload = map[string]any{}
	}
	args := append([]interface{}{log.DefaultMessageKey, msg, "payload", payload}, kvs...)
	h.helper.Log(log.LevelInfo, args...)
}

// RequestLogger constructs a helper with trace, component and payload information.
func RequestLogger(ctx context.Context, projectID string, base log.Logger, component string, payload map[string]any) *Helper {
	logger := WithTrace(ctx, projectID, base)
	logger = WithComponent(logger, component)
	logger = WithPayload(logger, payload)
	return NewHelper(logger)
}

// NewTestLogger returns a logger backed by an in-memory buffer and flush func.
func NewTestLogger(opts ...Option) (log.Logger, *bytes.Buffer, FlushFunc, error) {
	buf := &bytes.Buffer{}
	options := append(opts, WithWriter(buf))
	logger, flush, err := NewLogger(options...)
	if err != nil {
		return nil, nil, nil, err
	}
	return logger, buf, flush, nil
}

// StubTraceContext returns a context carrying deterministic trace & span IDs for testing.
func StubTraceContext(ctx context.Context, traceID, spanID string) context.Context {
	if traceID == "" && spanID == "" {
		return ctx
	}
	var tid trace.TraceID
	var sid trace.SpanID
	if traceID != "" {
		if b, err := hex.DecodeString(normaliseHex(traceID, 32)); err == nil {
			copy(tid[:], b)
		}
	}
	if spanID != "" {
		if b, err := hex.DecodeString(normaliseHex(spanID, 16)); err == nil {
			copy(sid[:], b)
		}
	}
	sc := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    tid,
		SpanID:     sid,
		TraceFlags: trace.FlagsSampled,
	})
	return trace.ContextWithSpanContext(ctx, sc)
}

// WriterOption allows passing arbitrary io.Writer instead of using WithWriter.
func WriterOption(w io.Writer) Option {
	return WithWriter(w)
}

func normaliseHex(value string, length int) string {
	value = strings.TrimSpace(strings.TrimPrefix(strings.ToLower(value), "0x"))
	if len(value) > length {
		return value[len(value)-length:]
	}
	if len(value) < length {
		return strings.Repeat("0", length-len(value)) + value
	}
	return value
}
