-- Outbox / Inbox 共享 SQL 查询

-- name: InsertOutboxEvent :one
INSERT INTO outbox_events (
    event_id,
    aggregate_type,
    aggregate_id,
    event_type,
    payload,
    headers,
    available_at
) VALUES (
    $1,
    $2,
    $3,
    $4,
    $5,
    $6,
    $7
)
RETURNING
    event_id,
    aggregate_type,
    aggregate_id,
    event_type,
    payload,
    headers,
    occurred_at,
    available_at,
    published_at,
    delivery_attempts,
    last_error;

-- name: ClaimPendingOutboxEvents :many
WITH candidates AS (
    SELECT o.event_id
    FROM outbox_events o
    WHERE o.published_at IS NULL
      AND o.available_at <= $1
      AND (o.lock_token IS NULL OR o.locked_at <= $2)
    ORDER BY o.available_at
    FOR UPDATE SKIP LOCKED
    LIMIT $3
)
UPDATE outbox_events AS o
SET lock_token = $4,
    locked_at = now()
FROM candidates
WHERE o.event_id = candidates.event_id
RETURNING
    o.event_id,
    o.aggregate_type,
    o.aggregate_id,
    o.event_type,
    o.payload,
    o.headers,
    o.occurred_at,
    o.available_at,
    o.published_at,
    o.delivery_attempts,
    o.last_error,
    o.lock_token,
    o.locked_at;

-- name: MarkOutboxEventPublished :exec
UPDATE outbox_events
SET published_at = $3,
    delivery_attempts = delivery_attempts + 1,
    last_error = NULL,
    lock_token = NULL,
    locked_at = NULL
WHERE event_id = $1 AND lock_token = $2;

-- name: RescheduleOutboxEvent :exec
UPDATE outbox_events
SET delivery_attempts = delivery_attempts + 1,
    last_error = $3,
    available_at = $4,
    lock_token = NULL,
    locked_at = NULL
WHERE event_id = $1 AND lock_token = $2;

-- name: CountPendingOutboxEvents :one
SELECT COUNT(*)::bigint
FROM outbox_events
WHERE published_at IS NULL;

-- Inbox 相关查询

-- name: InsertInboxEvent :exec
INSERT INTO inbox_events (
    event_id,
    source_service,
    event_type,
    aggregate_type,
    aggregate_id,
    payload
) VALUES (
    $1,
    $2,
    $3,
    $4,
    $5,
    $6
)
ON CONFLICT (event_id) DO NOTHING;

-- name: MarkInboxEventProcessed :exec
UPDATE inbox_events
SET processed_at = $2,
    last_error = NULL
WHERE event_id = $1;

-- name: RecordInboxEventError :exec
UPDATE inbox_events
SET last_error = $2,
    processed_at = NULL
WHERE event_id = $1;

-- name: GetInboxEvent :one
SELECT
    event_id,
    source_service,
    event_type,
    aggregate_type,
    aggregate_id,
    payload,
    received_at,
    processed_at,
    last_error
FROM inbox_events
WHERE event_id = $1;
