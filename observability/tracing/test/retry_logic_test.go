package tracing_test

import (
	"testing"
	"time"

	"github.com/bionicotaku/lingo-utils/observability/tracing"
	"github.com/stretchr/testify/require"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	errdetails "google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/durationpb"
)

func TestClassifyExportErrorResourceExhaustedWithoutRetryInfo(t *testing.T) {
	err := status.New(codes.ResourceExhausted, "quota").Err()
	retryable, code, throttle := tracing.TestingClassifyExportError(err)

	require.True(t, retryable)
	require.Equal(t, codes.ResourceExhausted, code)
	require.Zero(t, throttle)
}

func TestClassifyExportErrorResourceExhaustedWithRetryInfo(t *testing.T) {
	retryInfo := &errdetails.RetryInfo{
		RetryDelay: durationpb.New(3 * time.Second),
	}
	st, err := status.New(codes.ResourceExhausted, "quota").WithDetails(retryInfo)
	require.NoError(t, err)

	retryable, code, throttle := tracing.TestingClassifyExportError(st.Err())
	require.True(t, retryable)
	require.Equal(t, codes.ResourceExhausted, code)
	require.Equal(t, 3*time.Second, throttle)
}

func TestSpanCountAggregatesAllScopeSpans(t *testing.T) {
	spans := []*tracepb.ResourceSpans{
		{
			ScopeSpans: []*tracepb.ScopeSpans{
				{Spans: []*tracepb.Span{{}, {}}},
				{Spans: []*tracepb.Span{{}}},
			},
		},
		nil,
		{
			ScopeSpans: []*tracepb.ScopeSpans{
				nil,
				{Spans: []*tracepb.Span{{}, {}, {}}},
			},
		},
	}

	require.Equal(t, 6, tracing.TestingSpanCount(spans))
}
