CREATE TABLE clients (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    phone TEXT NOT NULL UNIQUE CHECK (phone ~ '^\+[1-9][0-9]{9,14}$'),
    phone_verified_at TIMESTAMPTZ,
    full_name TEXT,
    birth_date DATE,
    email TEXT,
    status TEXT NOT NULL DEFAULT 'registered' CHECK (
        status IN (
            'registered',
            'phone_verified',
            'profile_completed',
            'documents_uploaded',
            'pending_verification',
            'verified',
            'verification_rejected',
            'blocked'
        )
    ),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE phone_verification_codes (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    phone TEXT NOT NULL CHECK (phone ~ '^\+[1-9][0-9]{9,14}$'),
    code_hash BYTEA NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    attempts INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    consumed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX phone_verification_codes_phone_created_idx
    ON phone_verification_codes(phone, created_at DESC);

CREATE TABLE auth_sessions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    client_id UUID NOT NULL REFERENCES clients(id) ON DELETE CASCADE,
    token_hash BYTEA NOT NULL UNIQUE,
    expires_at TIMESTAMPTZ NOT NULL,
    revoked_at TIMESTAMPTZ,
    last_used_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX auth_sessions_client_id_idx ON auth_sessions(client_id);
CREATE INDEX auth_sessions_expires_at_idx ON auth_sessions(expires_at);

CREATE TABLE consent_acceptances (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    client_id UUID NOT NULL REFERENCES clients(id) ON DELETE CASCADE,
    offer_version TEXT NOT NULL,
    privacy_version TEXT NOT NULL,
    offer_accepted BOOLEAN NOT NULL CHECK (offer_accepted),
    privacy_accepted BOOLEAN NOT NULL CHECK (privacy_accepted),
    data_accuracy_confirmed BOOLEAN NOT NULL CHECK (data_accuracy_confirmed),
    rental_rules_accepted BOOLEAN NOT NULL CHECK (rental_rules_accepted),
    accepted_at TIMESTAMPTZ NOT NULL,
    ip_address INET NOT NULL,
    user_agent TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (client_id, offer_version, privacy_version)
);

CREATE INDEX consent_acceptances_client_id_idx ON consent_acceptances(client_id);
