package txmanager

import (
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// Convenience aliases mirroring pgx enums for improved readability.
const (
	ReadCommitted = pgx.ReadCommitted
	Serializable  = pgx.Serializable
	ReadWrite     = pgx.ReadWrite
	ReadOnly      = pgx.ReadOnly
)

// TxOptions captures per-call overrides controlling transaction behaviour.
type TxOptions struct {
	Isolation   pgx.TxIsoLevel
	AccessMode  pgx.TxAccessMode
	Timeout     time.Duration
	LockTimeout time.Duration
	TraceName   string
}

func mergeTxOptions(base, override TxOptions) TxOptions {
	result := base
	if override.Isolation != "" {
		result.Isolation = override.Isolation
	}
	if override.AccessMode != "" {
		result.AccessMode = override.AccessMode
	}
	if override.Timeout > 0 {
		result.Timeout = override.Timeout
	}
	if override.LockTimeout > 0 {
		result.LockTimeout = override.LockTimeout
	}
	if override.TraceName != "" {
		result.TraceName = override.TraceName
	}
	return result
}

func parseIsolation(value string) pgx.TxIsoLevel {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "serializable", "serial":
		return pgx.Serializable
	case "repeatable_read", "repeatable-read":
		return pgx.RepeatableRead
	case "read_uncommitted", "read-uncommitted":
		return pgx.ReadUncommitted
	case "read_committed", "read-committed", "":
		fallthrough
	default:
		return pgx.ReadCommitted
	}
}
