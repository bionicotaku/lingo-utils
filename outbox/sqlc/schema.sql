CREATE TABLE outbox_events (
  event_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  aggregate_type TEXT NOT NULL,
  aggregate_id UUID NOT NULL,
  event_type TEXT NOT NULL,
  payload BYTEA NOT NULL,
  headers JSONB NOT NULL DEFAULT '{}'::jsonb,
  occurred_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  available_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  published_at TIMESTAMPTZ,
  delivery_attempts INTEGER NOT NULL DEFAULT 0 CHECK (delivery_attempts >= 0),
  last_error TEXT,
  lock_token TEXT,
  locked_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS outbox_events_available_idx
  ON outbox_events (available_at)
  WHERE published_at IS NULL;

CREATE INDEX IF NOT EXISTS outbox_events_lock_idx
  ON outbox_events (lock_token)
  WHERE lock_token IS NOT NULL;

CREATE TABLE inbox_events (
  event_id UUID PRIMARY KEY,
  source_service TEXT NOT NULL,
  event_type TEXT NOT NULL,
  aggregate_type TEXT,
  aggregate_id TEXT,
  payload BYTEA NOT NULL,
  received_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  processed_at TIMESTAMPTZ,
  last_error TEXT
);

CREATE INDEX IF NOT EXISTS inbox_events_processed_idx
  ON inbox_events (processed_at);
