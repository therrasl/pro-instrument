ALTER TABLE clients
    ADD COLUMN bitrix_contact_id TEXT;

CREATE UNIQUE INDEX clients_bitrix_contact_id_unique_idx
    ON clients(bitrix_contact_id)
    WHERE bitrix_contact_id IS NOT NULL;

DROP INDEX integration_outbox_dispatch_idx;

ALTER TABLE integration_outbox
    DROP CONSTRAINT integration_outbox_event_type_aggregate_id_key;

ALTER TABLE integration_outbox
    RENAME COLUMN available_at TO next_attempt_at;

ALTER TABLE integration_outbox
    ADD COLUMN dedupe_key TEXT,
    ADD COLUMN locked_at TIMESTAMPTZ;

UPDATE integration_outbox
SET dedupe_key = event_type || ':' || aggregate_id::text;

ALTER TABLE integration_outbox
    ALTER COLUMN dedupe_key SET NOT NULL,
    ADD CONSTRAINT integration_outbox_dedupe_key_key UNIQUE (dedupe_key);

CREATE INDEX integration_outbox_dispatch_idx
    ON integration_outbox(status, next_attempt_at, created_at)
    WHERE status IN ('pending', 'processing', 'failed');

CREATE TABLE bitrix_inbound_events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    event_key TEXT NOT NULL UNIQUE,
    bitrix_deal_id TEXT NOT NULL,
    raw_payload JSONB NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending' CHECK (
        status IN ('pending', 'processing', 'completed', 'rejected', 'failed')
    ),
    attempts INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    locked_at TIMESTAMPTZ,
    last_error TEXT,
    rental_request_id UUID REFERENCES rental_requests(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    processed_at TIMESTAMPTZ
);

CREATE INDEX bitrix_inbound_events_dispatch_idx
    ON bitrix_inbound_events(status, next_attempt_at, created_at)
    WHERE status IN ('pending', 'processing', 'failed');
