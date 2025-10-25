package pgxpoolx

import (
	"errors"
	"strings"
	"time"
)

const (
	defaultHealthCheckTimeout = 5 * time.Second
)

var defaultSearchPath = []string{"public"}

// Config captures PostgreSQL connection pool settings applied during component
// initialization. All fields are optional except DSN; zero values trigger
// sensible defaults that align with Supabase recommendations.
type Config struct {
	DSN                string
	MaxConns           int32
	MinConns           int32
	MaxConnLifetime    time.Duration
	MaxConnIdleTime    time.Duration
	HealthCheckPeriod  time.Duration
	HealthCheckTimeout time.Duration
	Schema             string
	SearchPath         []string
	EnablePreparedStmt *bool
	MetricsEnabled     *bool
}

// Sanitize validates mandatory fields and applies default values. It returns a
// new Config instance, leaving the original untouched.
func (c Config) Sanitize() (Config, error) {
	if strings.TrimSpace(c.DSN) == "" {
		return Config{}, errors.New("pgxpoolx: dsn is required")
	}

	s := c
	s.DSN = strings.TrimSpace(c.DSN)

	if s.HealthCheckTimeout <= 0 {
		s.HealthCheckTimeout = defaultHealthCheckTimeout
	}

	if len(s.SearchPath) == 0 {
		if schema := strings.TrimSpace(s.Schema); schema != "" {
			s.SearchPath = []string{schema, defaultSearchPath[0]}
		} else {
			s.SearchPath = append([]string{}, defaultSearchPath...)
		}
	}

	if s.EnablePreparedStmt == nil {
		s.EnablePreparedStmt = boolPtr(false)
	}

	if s.MetricsEnabled == nil {
		s.MetricsEnabled = boolPtr(false)
	}

	return s, nil
}

func (c Config) PreparedStatementsEnabled() bool {
	if c.EnablePreparedStmt == nil {
		return false
	}
	return *c.EnablePreparedStmt
}

func (c Config) MetricsEnabledValue() bool {
	if c.MetricsEnabled == nil {
		return false
	}
	return *c.MetricsEnabled
}

func boolPtr(v bool) *bool {
	b := v
	return &b
}
