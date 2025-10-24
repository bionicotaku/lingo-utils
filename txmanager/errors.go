package txmanager

import (
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"
)

var ErrRetryableTx = errors.New("txmanager: retryable transaction")

// wrapRetryable annotates the provided error as retryable while preserving the
// original cause for downstream inspection.
func wrapRetryable(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%w: %w", ErrRetryableTx, err)
}

// classifyPgError inspects pg errors and reports whether a retry makes sense.
func classifyPgError(err error) (retryable bool, sqlState string) {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		sqlState = pgErr.SQLState()
		switch sqlState {
		case "40001", // serialization_failure
			"40P01", // deadlock_detected
			"55P03": // lock_not_available
			return true, sqlState
		default:
			return false, sqlState
		}
	}
	return false, ""
}

// IsRetryable reports whether the error came from a retryable transaction
// failure (deadlock / serialization / lock timeout).
func IsRetryable(err error) bool {
	return errors.Is(err, ErrRetryableTx)
}
