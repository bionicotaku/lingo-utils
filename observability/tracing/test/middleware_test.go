package tracing_test

import (
	"context"
	"testing"

	"github.com/bionicotaku/lingo-utils/observability/tracing"
	"github.com/go-kratos/kratos/v2/middleware"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	nooptrace "go.opentelemetry.io/otel/trace/noop"
)

// mockHandler 用于测试中间件链
func mockHandler(ctx context.Context, req interface{}) (interface{}, error) {
	return "response", nil
}

func TestServerMiddleware_DefaultSkipper(t *testing.T) {
	tp := nooptrace.NewTracerProvider()
	t.Cleanup(func() {
		otel.SetTracerProvider(nooptrace.NewTracerProvider())
	})

	mw := tracing.Server(
		tracing.WithServerTracerProvider(tp),
	)

	// 验证中间件可以正常构建和调用
	handler := mw(mockHandler)
	require.NotNil(t, handler)

	// 测试一个简单的调用
	ctx := context.Background()
	resp, err := handler(ctx, "request")
	require.NoError(t, err)
	require.Equal(t, "response", resp)
}

func TestServerMiddleware_CustomSkipper(t *testing.T) {
	tp := nooptrace.NewTracerProvider()
	t.Cleanup(func() {
		otel.SetTracerProvider(nooptrace.NewTracerProvider())
	})

	customSkipper := func(operation string) bool {
		return operation == "/custom/skip"
	}

	mw := tracing.Server(
		tracing.WithServerTracerProvider(tp),
		tracing.WithServerSkipper(customSkipper),
	)

	handler := mw(mockHandler)
	require.NotNil(t, handler)

	ctx := context.Background()
	resp, err := handler(ctx, "request")
	require.NoError(t, err)
	require.Equal(t, "response", resp)
}

func TestServerMiddleware_NilSkipper(t *testing.T) {
	tp := nooptrace.NewTracerProvider()
	t.Cleanup(func() {
		otel.SetTracerProvider(nooptrace.NewTracerProvider())
	})

	// 传递 nil skipper 应该被忽略，使用默认 skipper
	mw := tracing.Server(
		tracing.WithServerTracerProvider(tp),
		tracing.WithServerSkipper(nil),
	)

	handler := mw(mockHandler)
	require.NotNil(t, handler)

	ctx := context.Background()
	resp, err := handler(ctx, "request")
	require.NoError(t, err)
	require.Equal(t, "response", resp)
}

func TestServerMiddleware_WithCustomPropagator(t *testing.T) {
	tp := nooptrace.NewTracerProvider()
	propagator := propagation.NewCompositeTextMapPropagator(propagation.TraceContext{})

	t.Cleanup(func() {
		otel.SetTracerProvider(nooptrace.NewTracerProvider())
	})

	mw := tracing.Server(
		tracing.WithServerTracerProvider(tp),
		tracing.WithServerPropagator(propagator),
	)

	handler := mw(mockHandler)
	resp, err := handler(context.Background(), "request")

	require.NoError(t, err)
	require.Equal(t, "response", resp)
}

func TestServerMiddleware_WithCustomTracerName(t *testing.T) {
	tp := nooptrace.NewTracerProvider()

	t.Cleanup(func() {
		otel.SetTracerProvider(nooptrace.NewTracerProvider())
	})

	mw := tracing.Server(
		tracing.WithServerTracerProvider(tp),
		tracing.WithServerTracerName("custom-tracer"),
	)

	handler := mw(mockHandler)
	resp, err := handler(context.Background(), "request")

	require.NoError(t, err)
	require.Equal(t, "response", resp)
}

func TestClientMiddleware_BasicFunctionality(t *testing.T) {
	tp := nooptrace.NewTracerProvider()

	t.Cleanup(func() {
		otel.SetTracerProvider(nooptrace.NewTracerProvider())
	})

	mw := tracing.Client(
		tracing.WithClientTracerProvider(tp),
	)

	handler := mw(mockHandler)
	resp, err := handler(context.Background(), "request")

	require.NoError(t, err)
	require.Equal(t, "response", resp)
}

func TestClientMiddleware_WithCustomSkipper(t *testing.T) {
	tp := nooptrace.NewTracerProvider()

	t.Cleanup(func() {
		otel.SetTracerProvider(nooptrace.NewTracerProvider())
	})

	customSkipper := func(operation string) bool {
		return operation == "/skip-this"
	}

	mw := tracing.Client(
		tracing.WithClientTracerProvider(tp),
		tracing.WithClientSkipper(customSkipper),
	)

	handler := mw(mockHandler)
	resp, err := handler(context.Background(), "request")

	require.NoError(t, err)
	require.Equal(t, "response", resp)
}

func TestClientMiddleware_NilSkipper(t *testing.T) {
	tp := nooptrace.NewTracerProvider()

	t.Cleanup(func() {
		otel.SetTracerProvider(nooptrace.NewTracerProvider())
	})

	// nil skipper 应该被忽略
	mw := tracing.Client(
		tracing.WithClientTracerProvider(tp),
		tracing.WithClientSkipper(nil),
	)

	handler := mw(mockHandler)
	resp, err := handler(context.Background(), "request")

	require.NoError(t, err)
	require.Equal(t, "response", resp)
}

func TestClientMiddleware_WithCustomPropagator(t *testing.T) {
	tp := nooptrace.NewTracerProvider()
	propagator := propagation.NewCompositeTextMapPropagator(propagation.Baggage{})

	t.Cleanup(func() {
		otel.SetTracerProvider(nooptrace.NewTracerProvider())
	})

	mw := tracing.Client(
		tracing.WithClientTracerProvider(tp),
		tracing.WithClientPropagator(propagator),
	)

	handler := mw(mockHandler)
	resp, err := handler(context.Background(), "request")

	require.NoError(t, err)
	require.Equal(t, "response", resp)
}

func TestClientMiddleware_WithCustomTracerName(t *testing.T) {
	tp := nooptrace.NewTracerProvider()

	t.Cleanup(func() {
		otel.SetTracerProvider(nooptrace.NewTracerProvider())
	})

	mw := tracing.Client(
		tracing.WithClientTracerProvider(tp),
		tracing.WithClientTracerName("custom-client-tracer"),
	)

	handler := mw(mockHandler)
	resp, err := handler(context.Background(), "request")

	require.NoError(t, err)
	require.Equal(t, "response", resp)
}

func TestMiddleware_MultipleOptionsComposition(t *testing.T) {
	tp := nooptrace.NewTracerProvider()
	propagator := propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	)

	t.Cleanup(func() {
		otel.SetTracerProvider(nooptrace.NewTracerProvider())
	})

	// 测试服务器中间件的多个选项组合
	serverMW := tracing.Server(
		tracing.WithServerTracerProvider(tp),
		tracing.WithServerPropagator(propagator),
		tracing.WithServerTracerName("server-tracer"),
		tracing.WithServerSkipper(func(operation string) bool {
			return operation == "/skip"
		}),
	)

	// 测试客户端中间件的多个选项组合
	clientMW := tracing.Client(
		tracing.WithClientTracerProvider(tp),
		tracing.WithClientPropagator(propagator),
		tracing.WithClientTracerName("client-tracer"),
		tracing.WithClientSkipper(func(operation string) bool {
			return operation == "/skip"
		}),
	)

	// 验证服务器中间件
	serverHandler := serverMW(mockHandler)
	resp, err := serverHandler(context.Background(), "request")
	require.NoError(t, err)
	require.Equal(t, "response", resp)

	// 验证客户端中间件
	clientHandler := clientMW(mockHandler)
	resp, err = clientHandler(context.Background(), "request")
	require.NoError(t, err)
	require.Equal(t, "response", resp)
}

func TestMiddleware_ChainWithOtherMiddleware(t *testing.T) {
	tp := nooptrace.NewTracerProvider()

	t.Cleanup(func() {
		otel.SetTracerProvider(nooptrace.NewTracerProvider())
	})

	tracingMW := tracing.Server(
		tracing.WithServerTracerProvider(tp),
	)

	// 模拟另一个中间件
	loggingMW := func(handler middleware.Handler) middleware.Handler {
		return func(ctx context.Context, req interface{}) (interface{}, error) {
			// 模拟日志记录
			return handler(ctx, req)
		}
	}

	// 构建中间件链
	chain := middleware.Chain(tracingMW, loggingMW)

	handler := chain(mockHandler)
	resp, err := handler(context.Background(), "request")

	require.NoError(t, err)
	require.Equal(t, "response", resp)
}
