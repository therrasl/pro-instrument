CREATE TABLE client_documents (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    client_id UUID NOT NULL REFERENCES clients(id) ON DELETE CASCADE,
    document_type TEXT NOT NULL CHECK (
        document_type IN (
            'passport_main',
            'passport_registration',
            'selfie_with_passport'
        )
    ),
    storage_key TEXT NOT NULL UNIQUE,
    mime_type TEXT NOT NULL,
    size_bytes BIGINT NOT NULL CHECK (size_bytes > 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (client_id, document_type)
);

CREATE INDEX client_documents_client_id_idx ON client_documents(client_id);

CREATE TABLE verification_reviews (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    client_id UUID NOT NULL REFERENCES clients(id) ON DELETE CASCADE,
    reviewer_id TEXT NOT NULL,
    decision TEXT NOT NULL CHECK (decision IN ('approved', 'rejected')),
    reason TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (
        (decision = 'approved' AND reason IS NULL)
        OR (decision = 'rejected' AND NULLIF(BTRIM(reason), '') IS NOT NULL)
    )
);

CREATE INDEX verification_reviews_client_id_created_idx
    ON verification_reviews(client_id, created_at DESC);

CREATE TABLE verification_audit_logs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    client_id UUID NOT NULL REFERENCES clients(id) ON DELETE CASCADE,
    document_id UUID,
    review_id UUID,
    actor_id TEXT NOT NULL,
    action TEXT NOT NULL CHECK (
        action IN (
            'document_uploaded',
            'document_deleted',
            'verification_approved',
            'verification_rejected'
        )
    ),
    details JSONB NOT NULL DEFAULT '{}'::JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX verification_audit_logs_client_id_created_idx
    ON verification_audit_logs(client_id, created_at DESC);
