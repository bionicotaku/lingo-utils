package gcjwt_test

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/bionicotaku/lingo-utils/gcjwt"
	"github.com/go-kratos/kratos/v2/log"
	"github.com/go-kratos/kratos/v2/transport"
	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"
)

func TestClientMiddlewareInjectsToken(t *testing.T) {

	const expectedToken = "injected-token"
	gcjwt.SetTokenSourceFactory(func(ctx context.Context, audience string) (oauth2.TokenSource, error) {
		return oauth2.StaticTokenSource(&oauth2.Token{AccessToken: expectedToken}), nil
	})
	t.Cleanup(func() { gcjwt.SetTokenSourceFactory(nil) })

	header := newMockHeader()
	tr := &mockClientTransport{header: header}
	ctx := transport.NewClientContext(context.Background(), tr)

	called := false
	next := func(ctx context.Context, req interface{}) (interface{}, error) {
		called = true
		return "ok", nil
	}

	mw := gcjwt.Client(
		gcjwt.WithAudience("https://service"),
		gcjwt.WithClientLogger(log.NewStdLogger(io.Discard)),
	)

	resp, err := mw(next)(ctx, "payload")
	require.NoError(t, err)
	require.Equal(t, "ok", resp)
	require.True(t, called)
	require.Equal(t, "Bearer "+expectedToken, header.Get("authorization"))
}

func TestClientMiddlewareDisabled(t *testing.T) {

	header := newMockHeader()
	tr := &mockClientTransport{header: header}
	ctx := transport.NewClientContext(context.Background(), tr)

	mw := gcjwt.Client(
		gcjwt.WithAudience("https://service"),
		gcjwt.WithClientDisabled(true),
		gcjwt.WithClientLogger(log.NewStdLogger(io.Discard)),
	)

	_, err := mw(func(ctx context.Context, req interface{}) (interface{}, error) {
		return "ok", nil
	})(ctx, "payload")

	require.NoError(t, err)
	require.Equal(t, "", header.Get("authorization"))
}

func TestClientMiddlewareTransportMissing(t *testing.T) {

	gcjwt.SetTokenSourceFactory(func(ctx context.Context, audience string) (oauth2.TokenSource, error) {
		return oauth2.StaticTokenSource(&oauth2.Token{AccessToken: "token"}), nil
	})
	t.Cleanup(func() { gcjwt.SetTokenSourceFactory(nil) })

	mw := gcjwt.Client(
		gcjwt.WithAudience("https://service"),
		gcjwt.WithClientLogger(log.NewStdLogger(io.Discard)),
	)

	_, err := mw(func(ctx context.Context, req interface{}) (interface{}, error) {
		return nil, nil
	})(context.Background(), "payload")

	require.Error(t, err)
}

func TestClientMiddlewareTokenError(t *testing.T) {

	errSentinel := errors.New("boom")
	gcjwt.SetTokenSourceFactory(func(ctx context.Context, audience string) (oauth2.TokenSource, error) {
		return oauth2.StaticTokenSource(nil), errSentinel
	})
	t.Cleanup(func() { gcjwt.SetTokenSourceFactory(nil) })

	header := newMockHeader()
	tr := &mockClientTransport{header: header}
	ctx := transport.NewClientContext(context.Background(), tr)

	mw := gcjwt.Client(
		gcjwt.WithAudience("https://service"),
		gcjwt.WithClientLogger(log.NewStdLogger(io.Discard)),
	)

	_, err := mw(func(ctx context.Context, req interface{}) (interface{}, error) {
		return nil, nil
	})(ctx, "payload")

	require.Error(t, err)
	require.Contains(t, err.Error(), "gcjwt client")
}
