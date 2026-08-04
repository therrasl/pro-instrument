CREATE TABLE payments (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    rental_request_id UUID NOT NULL
        REFERENCES rental_requests(id) ON DELETE RESTRICT,
    provider_payment_id TEXT,
    idempotency_key TEXT NOT NULL UNIQUE,
    rental_amount BIGINT NOT NULL CHECK (rental_amount >= 0),
    deposit_amount BIGINT NOT NULL CHECK (deposit_amount >= 0),
    delivery_amount BIGINT NOT NULL CHECK (delivery_amount >= 0),
    total_amount BIGINT NOT NULL CHECK (total_amount > 0),
    currency TEXT NOT NULL DEFAULT 'RUB' CHECK (currency = 'RUB'),
    status TEXT NOT NULL CHECK (
        status IN (
            'creating',
            'pending',
            'succeeded',
            'canceled',
            'expired',
            'requires_review',
            'failed'
        )
    ),
    confirmation_url TEXT,
    provider_payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    succeeded_at TIMESTAMPTZ,
    cancelled_at TIMESTAMPTZ,
    CHECK (total_amount = rental_amount + deposit_amount + delivery_amount)
);

CREATE UNIQUE INDEX payments_provider_payment_id_unique_idx
    ON payments(provider_payment_id)
    WHERE provider_payment_id IS NOT NULL;
CREATE UNIQUE INDEX payments_one_active_per_rental_idx
    ON payments(rental_request_id)
    WHERE status IN ('creating', 'pending', 'requires_review');
CREATE INDEX payments_rental_created_idx
    ON payments(rental_request_id, created_at DESC);

CREATE TABLE payment_events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    event_key TEXT NOT NULL UNIQUE,
    provider_payment_id TEXT NOT NULL,
    event_type TEXT NOT NULL,
    raw_payload JSONB NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending' CHECK (
        status IN ('pending', 'processing', 'completed', 'rejected', 'failed')
    ),
    attempts INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    locked_at TIMESTAMPTZ,
    last_error TEXT,
    payment_id UUID REFERENCES payments(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    processed_at TIMESTAMPTZ
);

CREATE INDEX payment_events_dispatch_idx
    ON payment_events(status, next_attempt_at, created_at)
    WHERE status IN ('pending', 'processing', 'failed');
CREATE INDEX payment_events_provider_payment_idx
    ON payment_events(provider_payment_id, created_at DESC);

CREATE TABLE fiscal_receipts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    payment_id UUID NOT NULL UNIQUE REFERENCES payments(id) ON DELETE RESTRICT,
    status TEXT NOT NULL CHECK (
        status IN ('disabled', 'pending', 'succeeded', 'canceled', 'unknown')
    ),
    provider_registration_status TEXT,
    items JSONB NOT NULL DEFAULT '[]'::jsonb,
    provider_payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE deposits (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    rental_request_id UUID NOT NULL UNIQUE
        REFERENCES rental_requests(id) ON DELETE RESTRICT,
    payment_id UUID NOT NULL UNIQUE REFERENCES payments(id) ON DELETE RESTRICT,
    original_amount BIGINT NOT NULL CHECK (original_amount >= 0),
    refundable_amount BIGINT NOT NULL CHECK (refundable_amount >= 0),
    refunded_amount BIGINT NOT NULL DEFAULT 0 CHECK (refunded_amount >= 0),
    withheld_amount BIGINT NOT NULL DEFAULT 0 CHECK (withheld_amount >= 0),
    status TEXT NOT NULL CHECK (
        status IN ('pending', 'paid', 'partially_refunded', 'refunded', 'withheld')
    ),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (refundable_amount + refunded_amount + withheld_amount = original_amount)
);

CREATE TABLE payment_audit_logs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    payment_id UUID REFERENCES payments(id) ON DELETE SET NULL,
    rental_request_id UUID NOT NULL
        REFERENCES rental_requests(id) ON DELETE RESTRICT,
    action TEXT NOT NULL,
    actor_type TEXT NOT NULL CHECK (
        actor_type IN ('client', 'system', 'provider', 'integration')
    ),
    actor_id TEXT,
    details JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX payment_audit_logs_rental_created_idx
    ON payment_audit_logs(rental_request_id, created_at, id);
