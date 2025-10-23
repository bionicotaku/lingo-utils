package gcjwt_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"testing"
	"time"

	"github.com/bionicotaku/lingo-utils/gcjwt"
	"github.com/go-kratos/kratos/v2/log"
	"github.com/go-kratos/kratos/v2/transport"
	"github.com/stretchr/testify/require"
)

func makeToken(t *testing.T, aud, email string, exp time.Time) string {
	t.Helper()
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT"}`))
	payloadBytes, err := json.Marshal(map[string]interface{}{
		"aud":   aud,
		"email": email,
		"exp":   exp.Unix(),
	})
	require.NoError(t, err)
	payload := base64.RawURLEncoding.EncodeToString(payloadBytes)
	return header + "." + payload + ".signature"
}

func TestServerMiddleware(t *testing.T) {
	t.Parallel()

	now := time.Now()
	audience := "https://service-b.run.app/"
	token := makeToken(t, audience, "service-a@example.com", now.Add(time.Hour))

	header := newMockHeader()
	header.Set("authorization", "Bearer "+token)

	ctx := transport.NewServerContext(context.Background(), &mockServerTransport{header: header})
	mw := gcjwt.Server(
		gcjwt.WithExpectedAudience(audience),
		gcjwt.WithServerLogger(log.NewStdLogger(io.Discard)),
	)

	next := func(ctx context.Context, req interface{}) (interface{}, error) {
		claims, ok := gcjwt.FromContext(ctx)
		require.True(t, ok)
		require.Equal(t, audience, claims.Audience)
		require.Equal(t, "service-a@example.com", claims.Email)
		return "ok", nil
	}

	resp, err := mw(next)(ctx, "payload")
	require.NoError(t, err)
	require.Equal(t, "ok", resp)
}

func TestServerMiddlewareAudienceMismatch(t *testing.T) {
	t.Parallel()

	header := newMockHeader()
	header.Set("authorization", "Bearer "+makeToken(t, "https://other", "svc@example.com", time.Now().Add(time.Hour)))

	ctx := transport.NewServerContext(context.Background(), &mockServerTransport{header: header})

	mw := gcjwt.Server(
		gcjwt.WithExpectedAudience("https://service"),
		gcjwt.WithServerLogger(log.NewStdLogger(io.Discard)),
	)

	_, err := mw(dummyNext)(ctx, "req")
	require.ErrorIs(t, err, gcjwt.ErrInvalidAudience)
}

func TestServerMiddlewareMissingToken(t *testing.T) {
	t.Parallel()

	ctx := transport.NewServerContext(context.Background(), &mockServerTransport{header: newMockHeader()})
	mw := gcjwt.Server(
		gcjwt.WithExpectedAudience("https://service"),
		gcjwt.WithServerLogger(log.NewStdLogger(io.Discard)),
	)

	_, err := mw(dummyNext)(ctx, "req")
	require.ErrorIs(t, err, gcjwt.ErrMissingToken)
}

func TestServerMiddlewareSkipValidate(t *testing.T) {
	t.Parallel()

	header := newMockHeader()
	header.Set("authorization", "Bearer "+makeToken(t, "https://other", "svc@example.com", time.Now().Add(time.Hour)))

	ctx := transport.NewServerContext(context.Background(), &mockServerTransport{header: header})
	mw := gcjwt.Server(
		gcjwt.WithSkipValidate(true),
		gcjwt.WithTokenRequired(false),
		gcjwt.WithServerLogger(log.NewStdLogger(io.Discard)),
	)

	_, err := mw(dummyNext)(ctx, "req")
	require.NoError(t, err)
}

func dummyNext(ctx context.Context, req interface{}) (interface{}, error) {
	return nil, nil
}
