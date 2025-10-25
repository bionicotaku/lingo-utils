package pgxpoolx_test

import (
	"context"
	"io"
	"testing"

	"github.com/bionicotaku/lingo-utils/pgxpoolx"
	"github.com/go-kratos/kratos/v2/log"
	"github.com/stretchr/testify/require"
)

func TestProvideComponentInvalidDSN(t *testing.T) {
	cfg := pgxpoolx.Config{DSN: "invalid"}
	logger := log.NewStdLogger(io.Discard)
	comp, cleanup, err := pgxpoolx.ProvideComponent(context.Background(), cfg, logger)
	require.Error(t, err)
	require.Nil(t, comp)
	require.Nil(t, cleanup)
}

func TestProvidePoolNilComponent(t *testing.T) {
	require.Nil(t, pgxpoolx.ProvidePool(nil))
}
