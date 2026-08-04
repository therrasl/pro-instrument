CREATE TABLE client_push_tokens (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    client_id UUID NOT NULL REFERENCES clients(id) ON DELETE CASCADE,
    token TEXT NOT NULL UNIQUE,
    platform TEXT NOT NULL CHECK (platform IN ('android', 'ios')),
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    disabled_at TIMESTAMPTZ
);

CREATE INDEX client_push_tokens_client_enabled_idx
    ON client_push_tokens(client_id, enabled);

CREATE TABLE push_notification_events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    rental_request_id UUID NOT NULL REFERENCES rental_requests(id) ON DELETE CASCADE,
    client_id UUID NOT NULL REFERENCES clients(id) ON DELETE CASCADE,
    status TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (rental_request_id, status)
);

CREATE TABLE push_notification_deliveries (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    event_id UUID NOT NULL REFERENCES push_notification_events(id) ON DELETE CASCADE,
    push_token_id UUID NOT NULL REFERENCES client_push_tokens(id) ON DELETE CASCADE,
    status TEXT NOT NULL DEFAULT 'pending' CHECK (
        status IN ('pending', 'processing', 'ticketed', 'checking', 'sent', 'failed')
    ),
    attempts INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    receipt_attempts INTEGER NOT NULL DEFAULT 0 CHECK (receipt_attempts >= 0),
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    locked_at TIMESTAMPTZ,
    ticket_id TEXT,
    last_error TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    sent_at TIMESTAMPTZ,
    UNIQUE (event_id, push_token_id)
);

CREATE INDEX push_notification_deliveries_dispatch_idx
    ON push_notification_deliveries(status, next_attempt_at, created_at)
    WHERE status IN ('pending', 'processing', 'ticketed', 'checking', 'failed');

CREATE FUNCTION enqueue_rental_push_notification()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    notification_event_id UUID;
    notification_client_id UUID;
BEGIN
    IF NEW.to_status NOT IN (
        'paid',
        'preparing',
        'ready',
        'handed_to_courier',
        'rented',
        'awaiting_return',
        'inspection',
        'completed',
        'cancelled',
        'payment_expired'
    ) THEN
        RETURN NEW;
    END IF;

    SELECT client_id
    INTO notification_client_id
    FROM rental_requests
    WHERE id = NEW.rental_request_id;

    INSERT INTO push_notification_events (
        rental_request_id,
        client_id,
        status,
        created_at
    )
    VALUES (
        NEW.rental_request_id,
        notification_client_id,
        NEW.to_status,
        NEW.created_at
    )
    ON CONFLICT (rental_request_id, status) DO NOTHING
    RETURNING id INTO notification_event_id;

    IF notification_event_id IS NULL THEN
        RETURN NEW;
    END IF;

    INSERT INTO push_notification_deliveries (
        event_id,
        push_token_id,
        next_attempt_at,
        created_at
    )
    SELECT
        notification_event_id,
        token.id,
        NEW.created_at,
        NEW.created_at
    FROM client_push_tokens AS token
    WHERE token.client_id = notification_client_id
      AND token.enabled = TRUE
    ON CONFLICT (event_id, push_token_id) DO NOTHING;

    RETURN NEW;
END;
$$;

CREATE TRIGGER rental_status_push_notification
AFTER INSERT ON rental_status_history
FOR EACH ROW
EXECUTE FUNCTION enqueue_rental_push_notification();
