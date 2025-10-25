package pgxpoolx_test

import (
	"testing"
	"time"

	"github.com/bionicotaku/lingo-utils/pgxpoolx"
	"github.com/stretchr/testify/require"
)

func TestConfigSanitizeDefaults(t *testing.T) {
	cfg := pgxpoolx.Config{DSN: "postgres://user:pass@localhost:5432/db"}
	sanitized, err := cfg.Sanitize()
	require.NoError(t, err)

	require.Equal(t, 5*time.Second, sanitized.HealthCheckTimeout)
	require.False(t, sanitized.PreparedStatementsEnabled())
	require.False(t, sanitized.MetricsEnabledValue())
	require.ElementsMatch(t, []string{"public"}, sanitized.SearchPath)
}

func TestConfigSanitizeWithSchema(t *testing.T) {
	cfg := pgxpoolx.Config{
		DSN:    "postgres://user:pass@localhost:5432/db",
		Schema: "catalog",
	}
	sanitized, err := cfg.Sanitize()
	require.NoError(t, err)
	require.Equal(t, []string{"catalog", "public"}, sanitized.SearchPath)
	require.Equal(t, "catalog", sanitized.Schema)
}

func TestConfigSanitizeMissingDSN(t *testing.T) {
	_, err := pgxpoolx.Config{}.Sanitize()
	require.Error(t, err)
}
