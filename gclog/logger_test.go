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

	"github.com/go-kratos/kratos/v2/log"
	"github.com/stretchr/testify/require"
)

func TestNewLoggerValidation(t *testing.T) {
	_, _, err := NewLogger()
	require.Error(t, err)

	_, _, err = NewLogger(WithService("svc"))
	require.Error(t, err)

	logger, flush, err := NewLogger(WithService("svc"), WithVersion("v1"))
	require.NoError(t, err)
	require.NotNil(t, logger)
	require.NoError(t, flush(context.Background()))
}

func TestLoggerWritesExpectedJSON(t *testing.T) {
	logger, buf, _, err := NewTestLogger(
		WithService("catalog"),
		WithVersion("2025.10.21"),
		WithComponentTypes("catalog"),
	)
	require.NoError(t, err)

	err = logger.Log(
		log.LevelInfo,
		log.DefaultMessageKey, "accepted",
		"component", "catalog",
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
	require.Equal(t, "catalog", labels["component"])

	payload := entry["jsonPayload"].(map[string]any)
	require.Equal(t, "vid-1", payload["payload"].(map[string]any)["video_id"])
}

func TestInvalidComponentRecordsStatus(t *testing.T) {
	logger, buf, _, err := NewTestLogger(
		WithService("svc"),
		WithVersion("v1"),
		WithComponentTypes("allowed"),
	)
	require.NoError(t, err)

	err = logger.Log(
		log.LevelInfo,
		log.DefaultMessageKey, "msg",
		"component", "denied",
	)
	require.NoError(t, err)

	entry := decodeEntry(t, buf.String())
	payload := entry["jsonPayload"].(map[string]any)
	require.Equal(t, "invalid:denied", payload["component_status"])
}

func TestPayloadTypeError(t *testing.T) {
	logger, _, err := NewLogger(WithService("svc"), WithVersion("v1"))
	require.NoError(t, err)

	err = logger.Log(
		log.LevelInfo,
		log.DefaultMessageKey, "msg",
		"payload", "oops",
	)
	require.Error(t, err)
}

func TestWithTraceAndHelper(t *testing.T) {
	logger, buf, _, err := NewTestLogger(
		WithService("svc"),
		WithVersion("v1"),
	)
	require.NoError(t, err)

	ctx := StubTraceContext(context.Background(), "1234abcd", "0011")
	helper := NewHelper(WithTrace(ctx, "proj", logger)).WithComponent("component")
	helper.InfoWithPayload("done", map[string]any{"status": "ok"})

	entry := decodeEntry(t, buf.String())
	require.Contains(t, entry["trace"], "projects/proj/traces/")
	require.Equal(t, "component", entry["labels"].(map[string]any)["component"])
}

func TestLoggerRejectsUnsupportedKey(t *testing.T) {
	logger, _, err := NewLogger(WithService("svc"), WithVersion("v1"))
	require.NoError(t, err)

	err = logger.Log(log.LevelInfo, "foo", "bar")
	require.Error(t, err)
}

func TestInstanceIDLabels(t *testing.T) {
	logger, buf, _, err := NewTestLogger(
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
	logger, buf, _, err := NewTestLogger(
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
	kvs := AppendTrace(ctx, "my-project", nil)
	require.Len(t, kvs, 4)
	require.Contains(t, kvs[1], "projects/my-project/traces/")
	require.Equal(t, "1234abcd1234abcd", kvs[3])
}

func TestHelperWithPayload(t *testing.T) {
	logger, buf, _, err := NewTestLogger(WithService("svc"), WithVersion("v1"))
	require.NoError(t, err)

	helper := NewHelper(logger).WithComponent("catalog").WithPayload(map[string]any{"k": "v"})
	helper.InfoWithPayload("hello", map[string]any{"id": 1})

	entry := decodeEntry(t, buf.String())
	payload := entry["jsonPayload"].(map[string]any)
	inner := payload["payload"].(map[string]any)
	require.Equal(t, float64(1), inner["id"])
	labels := entry["labels"].(map[string]any)
	require.Equal(t, "catalog", labels["component"])
}

func TestConcurrentWrites(t *testing.T) {
	buf := &bytes.Buffer{}
	logger, _, err := NewLogger(WithService("svc"), WithVersion("v1"), WithWriter(buf))
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

func decodeEntry(t *testing.T, raw string) map[string]any {
	t.Helper()
	raw = strings.TrimSpace(raw)
	require.NotEmpty(t, raw, "log output must not be empty")
	var entry map[string]any
	require.NoError(t, json.Unmarshal([]byte(raw), &entry))
	return entry
}
