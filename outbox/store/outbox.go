package store

import (
	"context"
	"fmt"
	"time"

	"github.com/bionicotaku/lingo-utils/outbox/sqlc"
	"github.com/bionicotaku/lingo-utils/txmanager"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
)

// Message 描述待写入 outbox 的事件。
type Message struct {
	EventID       uuid.UUID
	AggregateType string
	AggregateID   uuid.UUID
	EventType     string
	Payload       []byte
	Headers       map[string]string
	AvailableAt   time.Time
}

// Event 表示从 outbox_events 读取的事件。
type Event struct {
	EventID          uuid.UUID
	AggregateType    string
	AggregateID      uuid.UUID
	EventType        string
	Payload          []byte
	Headers          map[string]string
	OccurredAt       time.Time
	AvailableAt      time.Time
	PublishedAt      *time.Time
	DeliveryAttempts int32
	LastError        *string
	LockToken        *string
	LockedAt         *time.Time
}

// Enqueue 在事务内插入 Outbox 事件。
func (r *Repository) Enqueue(ctx context.Context, sess txmanager.Session, msg Message) error {
	queries := r.queries(sess)

	availableAt := msg.AvailableAt.UTC()
	if availableAt.IsZero() {
		availableAt = time.Now().UTC()
	}

	headersJSON, err := encodeHeaders(msg.Headers)
	if err != nil {
		return fmt.Errorf("marshal headers: %w", err)
	}
	r.log.WithContext(ctx).Errorf("debug outbox headers: %s", headersJSON)

	params := outboxsql.InsertOutboxEventParams{
		EventID:       msg.EventID,
		AggregateType: msg.AggregateType,
		AggregateID:   msg.AggregateID,
		EventType:     msg.EventType,
		Payload:       msg.Payload,
		Headers:       headersJSON,
		AvailableAt:   timestamptzFromTime(availableAt),
	}

	if _, err := queries.InsertOutboxEvent(ctx, params); err != nil {
		r.log.WithContext(ctx).Errorf("insert outbox event failed: event_id=%s err=%v", msg.EventID, err)
		if pgErr, ok := err.(*pgconn.PgError); ok {
			r.log.WithContext(ctx).Errorw("pg error detail", "event_id", msg.EventID, "pg_message", pgErr.Message, "pg_detail", pgErr.Detail, "pg_hint", pgErr.Hint, "pg_position", pgErr.Position)
		}
		return fmt.Errorf("insert outbox event: %w", err)
	}

	r.log.WithContext(ctx).Debugf("outbox event enqueued: aggregate=%s id=%s", msg.AggregateType, msg.AggregateID)
	return nil
}

// ClaimPending 认领待发布事件。
func (r *Repository) ClaimPending(ctx context.Context, availableBefore, staleBefore time.Time, limit int, lockToken string) ([]Event, error) {
	params := outboxsql.ClaimPendingOutboxEventsParams{
		AvailableAt: timestamptzFromTime(availableBefore),
		LockedAt:    timestamptzFromTime(staleBefore),
		Limit:       int32(limit),
		LockToken:   textFromString(lockToken),
	}
	records, err := r.base.ClaimPendingOutboxEvents(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("claim outbox events: %w", err)
	}

	events := make([]Event, 0, len(records))
	for _, rec := range records {
		events = append(events, eventFromRecord(rec))
	}
	return events, nil
}

// MarkPublished 标记事件已发布。
func (r *Repository) MarkPublished(ctx context.Context, sess txmanager.Session, eventID uuid.UUID, lockToken string, publishedAt time.Time) error {
	queries := r.queries(sess)
	params := outboxsql.MarkOutboxEventPublishedParams{
		EventID:     eventID,
		LockToken:   textFromString(lockToken),
		PublishedAt: timestamptzFromTime(publishedAt),
	}
	if err := queries.MarkOutboxEventPublished(ctx, params); err != nil {
		return fmt.Errorf("mark outbox published: %w", err)
	}
	return nil
}

// Reschedule 将事件重新安排在未来时间发布，并记录错误。
func (r *Repository) Reschedule(ctx context.Context, sess txmanager.Session, eventID uuid.UUID, lockToken string, nextAvailable time.Time, lastErr string) error {
	queries := r.queries(sess)
	params := outboxsql.RescheduleOutboxEventParams{
		EventID:     eventID,
		LockToken:   textFromString(lockToken),
		LastError:   textFromNullableString(lastErr),
		AvailableAt: timestamptzFromTime(nextAvailable),
	}
	if err := queries.RescheduleOutboxEvent(ctx, params); err != nil {
		return fmt.Errorf("reschedule outbox event: %w", err)
	}
	return nil
}

// CountPending 返回当前未发布的 Outbox 事件数量。
func (r *Repository) CountPending(ctx context.Context) (int64, error) {
	count, err := r.base.CountPendingOutboxEvents(ctx)
	if err != nil {
		return 0, fmt.Errorf("count pending outbox events: %w", err)
	}
	return count, nil
}

func eventFromRecord(rec outboxsql.OutboxEvent) Event {
	var publishedAt *time.Time
	if rec.PublishedAt.Valid {
		value := rec.PublishedAt.Time
		publishedAt = &value
	}
	var lastErr *string
	if rec.LastError.Valid {
		value := rec.LastError.String
		lastErr = &value
	}
	var lockToken *string
	if rec.LockToken.Valid {
		value := rec.LockToken.String
		lockToken = &value
	}
	var lockedAt *time.Time
	if rec.LockedAt.Valid {
		value := rec.LockedAt.Time
		lockedAt = &value
	}

	return Event{
		EventID:          rec.EventID,
		AggregateType:    rec.AggregateType,
		AggregateID:      rec.AggregateID,
		EventType:        rec.EventType,
		Payload:          rec.Payload,
		Headers:          decodeHeaders(rec.Headers),
		OccurredAt:       mustTimestamp(rec.OccurredAt),
		AvailableAt:      mustTimestamp(rec.AvailableAt),
		PublishedAt:      publishedAt,
		DeliveryAttempts: rec.DeliveryAttempts,
		LastError:        lastErr,
		LockToken:        lockToken,
		LockedAt:         lockedAt,
	}
}
