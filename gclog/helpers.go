package gclog

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/go-kratos/kratos/v2/log"
	"go.opentelemetry.io/otel/trace"
)

// AppendTrace appends Cloud Logging compatible trace/span identifiers to kvs.
func AppendTrace(ctx context.Context, project string, kvs []interface{}) []interface{} {
	sc := trace.SpanContextFromContext(ctx)
	if sc.HasTraceID() {
		traceValue := sc.TraceID().String()
		if project != "" {
			traceValue = fmt.Sprintf("projects/%s/traces/%s", project, traceValue)
		}
		kvs = append(kvs, traceKey, traceValue)
	}
	if sc.HasSpanID() {
		kvs = append(kvs, spanKey, sc.SpanID().String())
	}
	return kvs
}

// AppendLabels 附加一组 label 键值到 kvs。
func AppendLabels(kvs []interface{}, labels map[string]string) []interface{} {
	if len(labels) == 0 {
		return kvs
	}
	return append(kvs, labelsKey, labels)
}

// WithTrace binds trace/span fields to the logger while preserving context.
func WithTrace(ctx context.Context, project string, base log.Logger) log.Logger {
	kvs := AppendTrace(ctx, project, nil)
	if len(kvs) == 0 {
		return log.WithContext(ctx, base)
	}
	return log.WithContext(ctx, log.With(base, kvs...))
}

// WithCaller appends caller information (e.g. package.Func:line) as a label.
func WithCaller(logger log.Logger, caller string) log.Logger {
	if caller == "" {
		return logger
	}
	return log.With(logger, callerKey, caller)
}

// WithLabels merges the provided label map into the entry.
func WithLabels(logger log.Logger, labels map[string]string) log.Logger {
	if len(labels) == 0 {
		return logger
	}
	return log.With(logger, labelsKey, labels)
}

// WithRequestID adds a request identifier label.
func WithRequestID(logger log.Logger, id string) log.Logger {
	if id == "" {
		return logger
	}
	return WithLabels(logger, map[string]string{"request_id": id})
}

// WithUser adds a user identifier label.
func WithUser(logger log.Logger, userID string) log.Logger {
	if userID == "" {
		return logger
	}
	return WithLabels(logger, map[string]string{"user_id": userID})
}

// WithPayload attaches a payload map to the logger.
func WithPayload(logger log.Logger, payload map[string]any) log.Logger {
	if payload == nil {
		payload = map[string]any{}
	}
	return log.With(logger, payloadKey, payload)
}

// WithStatus 将业务状态码写入 payload。
func WithStatus(logger log.Logger, status string) log.Logger {
	if status == "" {
		return logger
	}
	return log.With(logger, payloadKey, map[string]any{"status": status})
}

// WithError attaches an error string to the payload.
func WithError(logger log.Logger, err error) log.Logger {
	if err == nil {
		return logger
	}
	return log.With(logger, errorKey, err.Error())
}

// WithHTTPRequest records HTTP request summary for Cloud Logging.
func WithHTTPRequest(logger log.Logger, req *http.Request, status int, latency time.Duration) log.Logger {
	if req == nil {
		return logger
	}
	httpReq := &httpRequest{
		RequestMethod: req.Method,
		RequestURL:    req.URL.String(),
		Status:        status,
		UserAgent:     req.UserAgent(),
		RemoteIP:      clientIP(req),
		Referer:       req.Referer(),
		Protocol:      req.Proto,
	}
	if req.ContentLength > 0 {
		httpReq.RequestSize = fmt.Sprintf("%d", req.ContentLength)
	}
	if latency > 0 {
		httpReq.Latency = formatDuration(latency)
	}
	return log.With(logger, httpRequestKey, httpReq)
}

func clientIP(req *http.Request) string {
	if req == nil {
		return ""
	}
	if ip := req.Header.Get("X-Forwarded-For"); ip != "" {
		parts := strings.Split(ip, ",")
		return strings.TrimSpace(parts[0])
	}
	if ip := req.Header.Get("X-Real-IP"); ip != "" {
		return ip
	}
	host, _, err := net.SplitHostPort(req.RemoteAddr)
	if err == nil {
		return host
	}
	return req.RemoteAddr
}

// SeverityFromHTTP converts HTTP status codes to log levels.
func SeverityFromHTTP(status int) log.Level {
	switch {
	case status >= http.StatusInternalServerError:
		return log.LevelError
	case status >= http.StatusBadRequest:
		return log.LevelWarn
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
	return &Helper{base: logger, helper: log.NewHelper(logger)}
}

// Logger exposes the underlying logger.
func (h *Helper) Logger() log.Logger { return h.base }

// WithCaller returns a helper with caller label applied.
func (h *Helper) WithCaller(caller string) *Helper {
	return NewHelper(WithCaller(h.base, caller))
}

// WithLabels returns a helper with additional labels.
func (h *Helper) WithLabels(labels map[string]string) *Helper {
	return NewHelper(WithLabels(h.base, labels))
}

// WithPayload returns a helper with payload pre-attached.
func (h *Helper) WithPayload(payload map[string]any) *Helper {
	return NewHelper(WithPayload(h.base, payload))
}

// InfoWithPayload logs an info message并带 payload。
func (h *Helper) InfoWithPayload(msg string, payload map[string]any, kvs ...interface{}) {
	if payload == nil {
		payload = map[string]any{}
	}
	args := append([]interface{}{log.DefaultMessageKey, msg, payloadKey, payload}, kvs...)
	h.helper.Log(log.LevelInfo, args...)
}

// RequestLogger 组合 trace + caller + labels + payload。
func RequestLogger(ctx context.Context, base log.Logger, projectID string, caller string, labels map[string]string, payload map[string]any) *Helper {
	logger := WithTrace(ctx, projectID, base)
	logger = WithCaller(logger, caller)
	logger = WithLabels(logger, labels)
	logger = WithPayload(logger, payload)
	return NewHelper(logger)
}

// LabelsFromKVs 将 kv slice 转换为 map[string]interface{}，常用于 logging.WithFields。
func LabelsFromKVs(kvs []interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(kvs)/2)
	for i := 0; i+1 < len(kvs); i += 2 {
		key, ok := kvs[i].(string)
		if !ok {
			continue
		}
		out[key] = kvs[i+1]
	}
	return out
}

// NewTestLogger 返回用于测试的 logger、缓冲区和 flush。
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

// WriterOption 兼容旧版本 Option 命名。
func WriterOption(w io.Writer) Option { return WithWriter(w) }

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

func formatDuration(d time.Duration) string {
	if d <= 0 {
		return "0s"
	}
	return fmt.Sprintf("%.3fs", d.Seconds())
}
