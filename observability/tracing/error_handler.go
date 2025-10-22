package tracing

import (
	"errors"

	"go.opentelemetry.io/otel"
)

// loggedExporterError 用于标记已经由 exporterLogger 处理过的错误，避免在全局 error handler 重复输出。
type loggedExporterError struct {
	err error
}

func (e *loggedExporterError) Error() string {
	if e.err == nil {
		return ""
	}
	return e.err.Error()
}

func (e *loggedExporterError) Unwrap() error {
	return e.err
}

type otelErrorHandler struct {
	logger *exporterLogger
}

func newErrorHandler(logger *exporterLogger) otel.ErrorHandler {
	return &otelErrorHandler{
		logger: logger,
	}
}

func (h *otelErrorHandler) Handle(err error) {
	if err == nil {
		return
	}

	var logged *loggedExporterError
	if errors.As(err, &logged) {
		// 已经输出过日志，避免重复。
		return
	}

	h.logger.logUnhandled(err)
}
