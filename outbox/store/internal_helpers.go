package store

import (
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

func encodeHeaders(attrs map[string]string) ([]byte, error) {
	if attrs == nil {
		attrs = map[string]string{}
	}
	return json.Marshal(attrs)
}

func decodeHeaders(value []byte) map[string]string {
	if len(value) == 0 {
		return map[string]string{}
	}
	var attrs map[string]string
	if err := json.Unmarshal(value, &attrs); err != nil {
		return map[string]string{}
	}
	return attrs
}

func timestamptzFromTime(t time.Time) pgtype.Timestamptz {
	if t.IsZero() {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{Time: t.UTC(), Valid: true}
}

func mustTimestamp(ts pgtype.Timestamptz) time.Time {
	if !ts.Valid {
		return time.Time{}
	}
	return ts.Time
}

func textFromString(value string) pgtype.Text {
	if value == "" {
		return pgtype.Text{}
	}
	return pgtype.Text{String: value, Valid: true}
}

func textFromNullableString(value string) pgtype.Text {
	if value == "" {
		return pgtype.Text{}
	}
	return pgtype.Text{String: value, Valid: true}
}

func textFromPtr(value *string) pgtype.Text {
	if value == nil {
		return pgtype.Text{}
	}
	return pgtype.Text{String: *value, Valid: true}
}

func textPtr(value pgtype.Text) *string {
	if !value.Valid {
		return nil
	}
	return &value.String
}
