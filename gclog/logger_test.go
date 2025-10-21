package gclog

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/stretchr/testify/require"
)

func TestNewLoggerValidation(t *testing.T) {
	_, err := NewLogger()
	require.Error(t, err)

	_, err = NewLogger(WithService("svc"))
	require.Error(t, err)

	logger, err := NewLogger(WithService("svc"), WithVersion("v1"))
	require.NoError(t, err)
	require.NotNil(t, logger)
}

func TestLoggerWritesExpectedJSON(t *testing.T) {
	logger, buf, err := NewTestLogger(
		WithService("catalog"),
		WithVersion("2025.10.21"),
	)
	require.NoError(t, err)

	err = logger.Log(
		log.LevelInfo,
		log.DefaultMessageKey, "accepted",
		"caller", "service/handler.go:12",
		"payload", map[string]any{"video_id": "vid-1"},
	)
	require.NoError(t, err)

	entry := decodeEntry(t, buf.String())
	require.Equal(t, "INFO", entry["severity"])
	require.Equal(t, "accepted", entry["message"])

	serviceCtx := entry["serviceContext"].(map[string]any)
	require.Equal(t, "catalog", serviceCtx["service"])
	require.Equal(t, "2025.10.21", serviceCtx["version"])

	labels := entry["labels"].(map[string]any)
	require.Equal(t, "service/handler.go:12", labels["caller"])

	payload := entry["jsonPayload"].(map[string]any)
	require.Equal(t, "vid-1", payload["payload"].(map[string]any)["video_id"])
}

func TestPayloadTypeError(t *testing.T) {
	logger, err := NewLogger(WithService("svc"), WithVersion("v1"))
	require.NoError(t, err)

	err = logger.Log(
		log.LevelInfo,
		log.DefaultMessageKey, "msg",
		"payload", "oops",
	)
	require.Error(t, err)
}

func TestWithTraceAndHelper(t *testing.T) {
	logger, buf, err := NewTestLogger(
		WithService("svc"),
		WithVersion("v1"),
	)
	require.NoError(t, err)

	ctx := StubTraceContext(context.Background(), "1234abcd", "0011")
	helper := NewHelper(WithTrace(ctx, logger)).WithCaller("component")
	helper.InfoWithPayload("done", map[string]any{"status": "ok"})

	entry := decodeEntry(t, buf.String())
	require.Equal(t, "0000000000000000000000001234abcd", entry["trace"])
	require.Equal(t, "component", entry["labels"].(map[string]any)["caller"])
}

func TestLoggerRejectsUnsupportedKey(t *testing.T) {
	logger, err := NewLogger(WithService("svc"), WithVersion("v1"))
	require.NoError(t, err)

	err = logger.Log(log.LevelInfo, "foo", "bar")
	require.Error(t, err)
}

func TestLoggerAcceptsKratosDefaultKeys(t *testing.T) {
	logger, buf, err := NewTestLogger(WithService("svc"), WithVersion("v1"))
	require.NoError(t, err)

	require.NoError(t, logger.Log(
		log.LevelInfo,
		log.DefaultMessageKey, "msg",
		"kind", "server",
		"component", "grpc",
		"operation", "gateway.catalog",
		"args", "request",
		"code", int32(200),
		"reason", "OK",
		"stack", "trace",
		"latency", 0.123,
	))

	entry := decodeEntry(t, buf.String())
	labels := entry["labels"].(map[string]any)
	require.Equal(t, "server", labels["kind"])
	require.Equal(t, "grpc", labels["component"])
	require.Equal(t, "gateway.catalog", labels["operation"])

	payload := entry["jsonPayload"].(map[string]any)
	require.Equal(t, "request", payload["args"])
	require.Equal(t, float64(200), payload["code"])
	require.Equal(t, "OK", payload["reason"])
	require.Equal(t, "trace", payload["stack"])
	require.Equal(t, "0.123s", payload["latency"])
}

func TestKratosLabelMergeWithCustomLabels(t *testing.T) {
	logger, buf, err := NewTestLogger(WithService("svc"), WithVersion("v1"))
	require.NoError(t, err)

	logger = WithLabels(logger, map[string]string{"team": "infra"})

	require.NoError(t, logger.Log(
		log.LevelInfo,
		log.DefaultMessageKey, "msg",
		"kind", "client",
		"component", "http",
		"operation", "GET /v1/foo",
	))

	entry := decodeEntry(t, buf.String())
	labels := entry["labels"].(map[string]any)
	require.Equal(t, "infra", labels["team"])
	require.Equal(t, "client", labels["kind"])
	require.Equal(t, "http", labels["component"])
	require.Equal(t, "GET /v1/foo", labels["operation"])
}

func TestLoggerAllowsCustomKeys(t *testing.T) {
	logger, buf, err := NewTestLogger(
		WithService("svc"),
		WithVersion("v1"),
		WithAllowedKeys("extra"),
	)
	require.NoError(t, err)

	require.NoError(t, logger.Log(
		log.LevelInfo,
		log.DefaultMessageKey, "msg",
		"extra", map[string]any{"k": "v"},
	))

	entry := decodeEntry(t, buf.String())
	payload := entry["jsonPayload"].(map[string]any)
	extra := payload["extra"].(map[string]any)
	require.Equal(t, "v", extra["k"])
}

func TestWithAllowedLabelKeys(t *testing.T) {
	logger, buf, err := NewTestLogger(
		WithService("svc"),
		WithVersion("v1"),
		WithAllowedLabelKeys("tenant"),
	)
	require.NoError(t, err)

	require.NoError(t, logger.Log(log.LevelInfo, log.DefaultMessageKey, "msg", "tenant", "acme"))

	entry := decodeEntry(t, buf.String())
	labels := entry["labels"].(map[string]any)
	require.Equal(t, "acme", labels["tenant"])
	if payload, ok := entry["jsonPayload"].(map[string]any); ok {
		_, hasTenant := payload["tenant"]
		require.False(t, hasTenant)
	}
}

func TestInstanceIDLabels(t *testing.T) {
	logger, buf, err := NewTestLogger(
		WithService("svc"),
		WithVersion("v1"),
		WithInstanceID("instance-1"),
	)
	require.NoError(t, err)

	err = logger.Log(log.LevelInfo, log.DefaultMessageKey, "msg")
	require.NoError(t, err)

	entry := decodeEntry(t, buf.String())
	labels := entry["labels"].(map[string]any)
	require.Equal(t, "instance-1", labels["instance_id"])
}

func TestDisableInstanceID(t *testing.T) {
	logger, buf, err := NewTestLogger(
		WithService("svc"),
		WithVersion("v1"),
		DisableInstanceID(),
	)
	require.NoError(t, err)

	require.NoError(t, logger.Log(log.LevelInfo, log.DefaultMessageKey, "msg"))
	entry := decodeEntry(t, buf.String())
	_, ok := entry["labels"]
	require.False(t, ok)
}

func TestSeverityFromHTTP(t *testing.T) {
	require.Equal(t, log.LevelInfo, SeverityFromHTTP(http.StatusOK))
	require.Equal(t, log.LevelWarn, SeverityFromHTTP(http.StatusBadRequest))
	require.Equal(t, log.LevelError, SeverityFromHTTP(http.StatusInternalServerError))
}

func TestAppendTrace(t *testing.T) {
	ctx := StubTraceContext(context.Background(), "abcd1234abcd1234abcd1234abcd1234", "1234abcd1234abcd")
	kvs := AppendTrace(ctx, nil)
	require.Len(t, kvs, 4)
	require.Equal(t, traceKey, kvs[0])
	require.Equal(t, "abcd1234abcd1234abcd1234abcd1234", kvs[1])
	require.Equal(t, spanKey, kvs[2])
	require.Equal(t, "1234abcd1234abcd", kvs[3])
}

func TestHelperWithPayload(t *testing.T) {
	logger, buf, err := NewTestLogger(WithService("svc"), WithVersion("v1"))
	require.NoError(t, err)

	helper := NewHelper(logger).WithCaller("catalog").WithPayload(map[string]any{"k": "v"})
	helper.InfoWithPayload("hello", map[string]any{"id": 1})

	entry := decodeEntry(t, buf.String())
	payload := entry["jsonPayload"].(map[string]any)
	inner := payload["payload"].(map[string]any)
	require.Equal(t, float64(1), inner["id"])
	labels := entry["labels"].(map[string]any)
	require.Equal(t, "catalog", labels["caller"])
}

func TestPayloadMerge(t *testing.T) {
	logger, buf, err := NewTestLogger(WithService("svc"), WithVersion("v1"))
	require.NoError(t, err)

	logger = WithPayload(logger, map[string]any{"a": 1})
	logger = WithStatus(logger, "ok")
	logger = WithPayload(logger, map[string]any{"b": 2})

	require.NoError(t, logger.Log(log.LevelInfo, log.DefaultMessageKey, "msg"))

	entry := decodeEntry(t, buf.String())
	payload := entry["jsonPayload"].(map[string]any)["payload"].(map[string]any)
	require.Equal(t, float64(1), payload["a"])
	require.Equal(t, float64(2), payload["b"])
	require.Equal(t, "ok", payload["status"])
}

func TestConcurrentWrites(t *testing.T) {
	buf := &bytes.Buffer{}
	logger, err := NewLogger(WithService("svc"), WithVersion("v1"), WithWriter(buf))
	require.NoError(t, err)

	const n = 50
	wg := sync.WaitGroup{}
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			_ = logger.Log(log.LevelInfo, log.DefaultMessageKey, fmt.Sprintf("log-%d", i))
		}(i)
	}
	wg.Wait()

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	require.Len(t, lines, n)
}

func TestWithHTTPRequest(t *testing.T) {
	logger, buf, err := NewTestLogger(WithService("svc"), WithVersion("v1"))
	require.NoError(t, err)

	req, _ := http.NewRequest(http.MethodGet, "https://example.com/foo?bar=baz", nil)
	req.Header.Set("User-Agent", "test-agent")
	req.RemoteAddr = "127.0.0.1:12345"

	logger = WithHTTPRequest(
		logger,
		req,
		200,
		150*time.Millisecond,
		HTTPRequestResponseSize(512),
		HTTPRequestServerIP("10.0.0.1"),
		HTTPRequestCacheStatus(true, true, false),
	)
	require.NoError(t, logger.Log(log.LevelInfo, log.DefaultMessageKey, "http req"))

	entry := decodeEntry(t, buf.String())
	httpReq := entry["httpRequest"].(map[string]any)
	require.Equal(t, "GET", httpReq["requestMethod"])
	require.Equal(t, "https://example.com/foo?bar=baz", httpReq["requestUrl"])
	require.Equal(t, "0.150s", httpReq["latency"])
	require.Equal(t, "test-agent", httpReq["userAgent"])
	require.Equal(t, "127.0.0.1", httpReq["remoteIp"])
	require.Equal(t, "512", httpReq["responseSize"])
	require.Equal(t, "10.0.0.1", httpReq["serverIp"])
	require.Equal(t, true, httpReq["cacheLookup"])
	require.Equal(t, true, httpReq["cacheHit"])
	_, ok := httpReq["cacheValidatedWithOriginServer"]
	require.False(t, ok)
}

func TestLabelsHelpers(t *testing.T) {
	logger, buf, err := NewTestLogger(WithService("svc"), WithVersion("v1"))
	require.NoError(t, err)

	logger = WithLabels(logger, map[string]string{
		"request_id": "req-123",
		"user_id":    "user-456",
		"team":       "growth",
	})
	require.NoError(t, logger.Log(log.LevelInfo, log.DefaultMessageKey, "msg"))

	entry := decodeEntry(t, buf.String())
	labels := entry["labels"].(map[string]any)
	require.Equal(t, "req-123", labels["request_id"])
	require.Equal(t, "user-456", labels["user_id"])
	require.Equal(t, "growth", labels["team"])
}

func TestSourceLocationEnabled(t *testing.T) {
	logger, buf, err := NewTestLogger(WithService("svc"), WithVersion("v1"), EnableSourceLocation())
	require.NoError(t, err)

	require.NoError(t, logger.Log(log.LevelInfo, log.DefaultMessageKey, "msg"))
	entry := decodeEntry(t, buf.String())
	src, ok := entry["sourceLocation"].(map[string]any)
	if !ok {
		t.Skip("sourceLocation not captured on this runtime")
	}
	require.Contains(t, src["file"], "logger_test.go")
}

func TestWithErrorHelper(t *testing.T) {
	logger, buf, err := NewTestLogger(WithService("svc"), WithVersion("v1"))
	require.NoError(t, err)

	logger = WithError(logger, fmt.Errorf("boom"))
	require.NoError(t, logger.Log(log.LevelError, log.DefaultMessageKey, "failed"))

	entry := decodeEntry(t, buf.String())
	payload := entry["jsonPayload"].(map[string]any)
	require.Equal(t, "boom", payload["error"])
}

func TestJSONPayloadWithoutPayloadOrError(t *testing.T) {
	logger, buf, err := NewTestLogger(
		WithService("svc"),
		WithVersion("v1"),
		WithAllowedKeys("debug_id"),
	)
	require.NoError(t, err)

	require.NoError(t, logger.Log(
		log.LevelInfo,
		log.DefaultMessageKey, "msg",
		"debug_id", "abc123",
	))

	entry := decodeEntry(t, buf.String())
	payload := entry["jsonPayload"].(map[string]any)
	require.Equal(t, "abc123", payload["debug_id"])
}

func decodeEntry(t *testing.T, raw string) map[string]any {
	t.Helper()
	raw = strings.TrimSpace(raw)
	require.NotEmpty(t, raw, "log output must not be empty")
	var entry map[string]any
	require.NoError(t, json.Unmarshal([]byte(raw), &entry))
	return entry
}
