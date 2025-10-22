package tracing

import (
	"sync"
	"time"

	"github.com/go-kratos/kratos/v2/log"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// exporterLogger 负责将遥测导出相关的诊断信息统一输出到 Kratos logger。
type exporterLogger struct {
	helper *log.Helper

	mu           sync.Mutex
	consecutive  int
	lastGrpcCode codes.Code
}

func newExporterLogger(logger log.Logger) *exporterLogger {
	return &exporterLogger{
		helper: log.NewHelper(logger),
	}
}

func (l *exporterLogger) logRetry(err error, attempt int, nextBackoff time.Duration, throttle time.Duration, code codes.Code) {
	l.mu.Lock()
	l.consecutive = attempt
	l.lastGrpcCode = code
	l.mu.Unlock()

	fields := []interface{}{
		"msg", "otel exporter retry scheduled",
		"attempt", attempt,
		"grpc_code", code.String(),
		"error", err,
	}
	if nextBackoff > 0 {
		fields = append(fields, "next_backoff", nextBackoff)
	}
	if throttle > 0 {
		fields = append(fields, "throttle_delay", throttle)
	}

	l.helper.Warnw(fields...)
}

func (l *exporterLogger) logPermanentFailure(err error, attempt int, code codes.Code) {
	l.mu.Lock()
	l.consecutive = attempt
	l.lastGrpcCode = code
	l.mu.Unlock()

	l.helper.Errorw(
		"msg", "otel exporter permanent failure",
		"attempt", attempt,
		"grpc_code", code.String(),
		"error", err,
	)
}

func (l *exporterLogger) logContextFailure(err error, attempt int) {
	l.mu.Lock()
	l.consecutive = attempt
	l.lastGrpcCode = codes.Canceled
	l.mu.Unlock()

	l.helper.Errorw(
		"msg", "otel exporter aborted by context",
		"attempt", attempt,
		"error", err,
	)
}

func (l *exporterLogger) logRecovery(spanCount int, attempts int, elapsed time.Duration) {
	l.mu.Lock()
	hadFailures := l.consecutive > 0
	l.consecutive = 0
	prevCode := l.lastGrpcCode
	l.lastGrpcCode = codes.OK
	l.mu.Unlock()

	if !hadFailures {
		return
	}

	fields := []interface{}{
		"msg", "otel exporter recovered",
		"attempts", attempts,
		"duration", elapsed,
		"span_count", spanCount,
	}
	if prevCode != codes.OK {
		fields = append(fields, "last_grpc_code", prevCode.String())
	}

	l.helper.Infow(fields...)
}

func (l *exporterLogger) logUnhandled(err error) {
	if err == nil {
		return
	}

	var fields []interface{}
	if st, ok := status.FromError(err); ok {
		fields = []interface{}{
			"msg", "otel error",
			"grpc_code", st.Code().String(),
			"error", err,
		}
	} else {
		fields = []interface{}{
			"msg", "otel error",
			"error", err,
		}
	}
	l.helper.Errorw(fields...)
}
