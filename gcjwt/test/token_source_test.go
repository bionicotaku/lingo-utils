package gcjwt_test

import (
	"context"
	"errors"
	"io"
	"sync/atomic"
	"testing"

	"github.com/bionicotaku/lingo-utils/gcjwt"
	"github.com/go-kratos/kratos/v2/log"
	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"
)

func TestTokenSource_Success(t *testing.T) {

	const accessToken = "fake-token"
	var calls atomic.Int32

	gcjwt.SetTokenSourceFactory(func(ctx context.Context, audience string) (oauth2.TokenSource, error) {
		calls.Add(1)
		return oauth2.StaticTokenSource(&oauth2.Token{AccessToken: accessToken}), nil
	})
	t.Cleanup(func() { gcjwt.SetTokenSourceFactory(nil) })

	ts := gcjwt.NewTokenSource("https://service", log.NewStdLogger(io.Discard))

	got, err := ts.Token(context.Background())
	require.NoError(t, err)
	require.Equal(t, accessToken, got)

	// Second call should reuse the same underlying oauth2.TokenSource.
	got, err = ts.Token(context.Background())
	require.NoError(t, err)
	require.Equal(t, accessToken, got)
	require.Equal(t, int32(1), calls.Load())
}

func TestTokenSource_InitError(t *testing.T) {

	gcjwt.SetTokenSourceFactory(func(ctx context.Context, audience string) (oauth2.TokenSource, error) {
		return nil, errors.New("metadata unreachable")
	})
	t.Cleanup(func() { gcjwt.SetTokenSourceFactory(nil) })

	ts := gcjwt.NewTokenSource("https://service", log.NewStdLogger(io.Discard))

	token, err := ts.Token(context.Background())
	require.Error(t, err)
	require.ErrorIs(t, err, gcjwt.ErrTokenSourceInit)
	require.Empty(t, token)
}
