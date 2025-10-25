package store

import (
	"context"
	"time"

	"github.com/bionicotaku/lingo-utils/outbox/sqlc"
	"github.com/bionicotaku/lingo-utils/txmanager"
	"github.com/google/uuid"
)

// InboxMessage 表示需要记录的外部事件。
type InboxMessage struct {
	EventID       uuid.UUID
	SourceService string
	EventType     string
	AggregateType *string
	AggregateID   *string
	Payload       []byte
}

// InboxEvent 表示已记录的外部事件。
type InboxEvent struct {
	EventID       uuid.UUID
	SourceService string
	EventType     string
	AggregateType *string
	AggregateID   *string
	Payload       []byte
	ReceivedAt    time.Time
	ProcessedAt   *time.Time
	LastError     *string
}

// RecordInboxEvent 记录外部事件（幂等）。
func (r *Repository) RecordInboxEvent(ctx context.Context, sess txmanager.Session, msg InboxMessage) error {
	queries := r.queries(sess)
	params := outboxsql.InsertInboxEventParams{
		EventID:       msg.EventID,
		SourceService: msg.SourceService,
		EventType:     msg.EventType,
		AggregateType: textFromPtr(msg.AggregateType),
		AggregateID:   textFromPtr(msg.AggregateID),
		Payload:       msg.Payload,
	}
	if err := queries.InsertInboxEvent(ctx, params); err != nil {
		r.log.WithContext(ctx).Errorw("failed to insert inbox event", "event_id", msg.EventID, "source_service", msg.SourceService, "event_type", msg.EventType, "error", err)
		return err
	}
	return nil
}

// GetInboxEvent 获取已记录的外部事件。
func (r *Repository) GetInboxEvent(ctx context.Context, sess txmanager.Session, eventID uuid.UUID) (*InboxEvent, error) {
	rec, err := r.queries(sess).GetInboxEvent(ctx, eventID)
	if err != nil {
		r.log.WithContext(ctx).Errorw("failed to get inbox event", "event_id", eventID, "error", err)
		return nil, err
	}
	var processedAt *time.Time
	if rec.ProcessedAt.Valid {
		value := rec.ProcessedAt.Time
		processedAt = &value
	}
	var lastErr *string
	if rec.LastError.Valid {
		value := rec.LastError.String
		lastErr = &value
	}
	return &InboxEvent{
		EventID:       rec.EventID,
		SourceService: rec.SourceService,
		EventType:     rec.EventType,
		AggregateType: textPtr(rec.AggregateType),
		AggregateID:   textPtr(rec.AggregateID),
		Payload:       rec.Payload,
		ReceivedAt:    mustTimestamp(rec.ReceivedAt),
		ProcessedAt:   processedAt,
		LastError:     lastErr,
	}, nil
}

// MarkInboxProcessed 标记事件处理成功。
func (r *Repository) MarkInboxProcessed(ctx context.Context, sess txmanager.Session, eventID uuid.UUID, processedAt time.Time) error {
	queries := r.queries(sess)
	params := outboxsql.MarkInboxEventProcessedParams{
		EventID:     eventID,
		ProcessedAt: timestamptzFromTime(processedAt),
	}
	if err := queries.MarkInboxEventProcessed(ctx, params); err != nil {
		r.log.WithContext(ctx).Errorw("failed to mark inbox event as processed", "event_id", eventID, "processed_at", processedAt, "error", err)
		return err
	}
	return nil
}

// RecordInboxError 更新事件处理错误信息。
func (r *Repository) RecordInboxError(ctx context.Context, sess txmanager.Session, eventID uuid.UUID, errMsg string) error {
	queries := r.queries(sess)
	params := outboxsql.RecordInboxEventErrorParams{
		EventID:   eventID,
		LastError: textFromNullableString(errMsg),
	}
	if err := queries.RecordInboxEventError(ctx, params); err != nil {
		r.log.WithContext(ctx).Errorw("failed to record inbox event error", "event_id", eventID, "error_msg", errMsg, "error", err)
		return err
	}
	return nil
}
