CREATE EXTENSION IF NOT EXISTS btree_gist;

CREATE TABLE rental_requests (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    client_id UUID NOT NULL REFERENCES clients(id) ON DELETE RESTRICT,
    tool_id UUID NOT NULL REFERENCES tools(id) ON DELETE RESTRICT,
    tool_unit_id UUID NOT NULL REFERENCES tool_units(id) ON DELETE RESTRICT,
    start_date DATE NOT NULL,
    end_date DATE NOT NULL,
    rental_period DATERANGE GENERATED ALWAYS AS (
        daterange(start_date, end_date, '[]')
    ) STORED,
    rental_days INTEGER NOT NULL CHECK (rental_days > 0),
    rental_price BIGINT NOT NULL CHECK (rental_price >= 0),
    deposit_amount BIGINT NOT NULL CHECK (deposit_amount >= 0),
    delivery_cost BIGINT NOT NULL CHECK (delivery_cost >= 0),
    total_amount BIGINT NOT NULL CHECK (total_amount >= 0),
    delivery_method TEXT NOT NULL CHECK (
        delivery_method IN ('self_pickup', 'courier')
    ),
    delivery_address TEXT,
    status TEXT NOT NULL CHECK (
        status IN (
            'pending_manager',
            'awaiting_payment',
            'paid',
            'preparing',
            'ready',
            'handed_to_courier',
            'rented',
            'awaiting_return',
            'inspection',
            'completed',
            'rejected',
            'cancelled',
            'payment_expired'
        )
    ),
    expires_at TIMESTAMPTZ NOT NULL,
    bitrix_deal_id TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (end_date >= start_date),
    CHECK (rental_days = (end_date - start_date) + 1),
    CHECK (
        (delivery_method = 'courier' AND NULLIF(BTRIM(delivery_address), '') IS NOT NULL)
        OR (delivery_method = 'self_pickup' AND delivery_address IS NULL)
    ),
    CHECK (total_amount = rental_price + deposit_amount + delivery_cost),
    EXCLUDE USING gist (
        tool_unit_id WITH =,
        rental_period WITH &&
    ) WHERE (
        status IN (
            'pending_manager',
            'awaiting_payment',
            'paid',
            'preparing',
            'ready',
            'handed_to_courier',
            'rented',
            'awaiting_return',
            'inspection'
        )
    )
);

CREATE INDEX rental_requests_client_created_idx
    ON rental_requests(client_id, created_at DESC, id DESC);
CREATE INDEX rental_requests_tool_period_idx
    ON rental_requests(tool_id, start_date, end_date);
CREATE INDEX rental_requests_tool_unit_period_idx
    ON rental_requests(tool_unit_id, start_date, end_date);
CREATE INDEX rental_requests_status_expires_idx
    ON rental_requests(status, expires_at);

CREATE TABLE tool_holds (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    rental_request_id UUID NOT NULL UNIQUE
        REFERENCES rental_requests(id) ON DELETE CASCADE,
    tool_unit_id UUID NOT NULL REFERENCES tool_units(id) ON DELETE RESTRICT,
    start_date DATE NOT NULL,
    end_date DATE NOT NULL,
    rental_period DATERANGE GENERATED ALWAYS AS (
        daterange(start_date, end_date, '[]')
    ) STORED,
    status TEXT NOT NULL CHECK (
        status IN ('active', 'confirmed', 'expired', 'released', 'cancelled')
    ),
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (end_date >= start_date),
    EXCLUDE USING gist (
        tool_unit_id WITH =,
        rental_period WITH &&
    ) WHERE (status IN ('active', 'confirmed'))
);

CREATE INDEX tool_holds_status_expires_idx
    ON tool_holds(status, expires_at);
CREATE INDEX tool_holds_tool_unit_period_idx
    ON tool_holds USING gist(tool_unit_id, rental_period);

CREATE TABLE rental_status_history (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    rental_request_id UUID NOT NULL
        REFERENCES rental_requests(id) ON DELETE CASCADE,
    from_status TEXT CHECK (
        from_status IS NULL OR from_status IN (
            'pending_manager',
            'awaiting_payment',
            'paid',
            'preparing',
            'ready',
            'handed_to_courier',
            'rented',
            'awaiting_return',
            'inspection',
            'completed',
            'rejected',
            'cancelled',
            'payment_expired'
        )
    ),
    to_status TEXT NOT NULL CHECK (
        to_status IN (
            'pending_manager',
            'awaiting_payment',
            'paid',
            'preparing',
            'ready',
            'handed_to_courier',
            'rented',
            'awaiting_return',
            'inspection',
            'completed',
            'rejected',
            'cancelled',
            'payment_expired'
        )
    ),
    actor_type TEXT NOT NULL CHECK (
        actor_type IN ('client', 'system', 'manager', 'integration')
    ),
    actor_id TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX rental_status_history_request_created_idx
    ON rental_status_history(rental_request_id, created_at, id);

CREATE TABLE integration_outbox (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    event_type TEXT NOT NULL,
    aggregate_type TEXT NOT NULL,
    aggregate_id UUID NOT NULL,
    payload JSONB NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending' CHECK (
        status IN ('pending', 'processing', 'completed', 'failed')
    ),
    attempts INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    available_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_error TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    processed_at TIMESTAMPTZ,
    UNIQUE (event_type, aggregate_id)
);

CREATE INDEX integration_outbox_dispatch_idx
    ON integration_outbox(status, available_at, created_at)
    WHERE status IN ('pending', 'failed');
