DROP TABLE IF EXISTS bitrix_inbound_events;

DROP INDEX IF EXISTS integration_outbox_dispatch_idx;

ALTER TABLE integration_outbox
    DROP CONSTRAINT IF EXISTS integration_outbox_dedupe_key_key,
    DROP COLUMN IF EXISTS dedupe_key,
    DROP COLUMN IF EXISTS locked_at;

ALTER TABLE integration_outbox
    RENAME COLUMN next_attempt_at TO available_at;

DELETE FROM integration_outbox AS duplicate
USING integration_outbox AS retained
WHERE
    duplicate.event_type = retained.event_type
    AND duplicate.aggregate_id = retained.aggregate_id
    AND (
        duplicate.created_at > retained.created_at
        OR (
            duplicate.created_at = retained.created_at
            AND duplicate.id > retained.id
        )
    );

ALTER TABLE integration_outbox
    ADD CONSTRAINT integration_outbox_event_type_aggregate_id_key
    UNIQUE (event_type, aggregate_id);

CREATE INDEX integration_outbox_dispatch_idx
    ON integration_outbox(status, available_at, created_at)
    WHERE status IN ('pending', 'failed');

DROP INDEX IF EXISTS clients_bitrix_contact_id_unique_idx;

ALTER TABLE clients
    DROP COLUMN IF EXISTS bitrix_contact_id;
