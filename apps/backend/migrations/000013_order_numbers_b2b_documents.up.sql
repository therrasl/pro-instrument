CREATE SEQUENCE rental_order_number_seq;

ALTER TABLE rental_requests ADD COLUMN order_number TEXT;

UPDATE rental_requests
SET order_number = format(
    'PI-%s-%s',
    to_char(created_at AT TIME ZONE 'UTC', 'YYYY'),
    lpad(nextval('rental_order_number_seq')::text, 6, '0')
);

ALTER TABLE rental_requests
    ALTER COLUMN order_number SET NOT NULL,
    ALTER COLUMN order_number SET DEFAULT format(
        'PI-%s-%s',
        to_char(CURRENT_DATE, 'YYYY'),
        lpad(nextval('rental_order_number_seq')::text, 6, '0')
    ),
    ADD CONSTRAINT rental_requests_order_number_key UNIQUE (order_number);

ALTER TABLE clients
    ADD COLUMN client_type TEXT NOT NULL DEFAULT 'individual'
        CHECK (client_type IN ('individual', 'legal_entity')),
    ADD COLUMN company_name TEXT,
    ADD COLUMN inn TEXT,
    ADD COLUMN kpp TEXT,
    ADD COLUMN ogrn TEXT,
    ADD COLUMN legal_address TEXT,
    ADD COLUMN company_contact TEXT;

CREATE TABLE rental_customer_snapshots (
    rental_request_id UUID PRIMARY KEY REFERENCES rental_requests(id) ON DELETE CASCADE,
    order_number TEXT NOT NULL,
    client_type TEXT NOT NULL CHECK (client_type IN ('individual', 'legal_entity')),
    full_name TEXT,
    email TEXT,
    company_name TEXT,
    inn TEXT,
    kpp TEXT,
    ogrn TEXT,
    legal_address TEXT,
    company_contact TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

INSERT INTO rental_customer_snapshots (
    rental_request_id, order_number, client_type, full_name, email,
    company_name, inn, kpp, ogrn, legal_address, company_contact, created_at
)
SELECT
    rr.id, rr.order_number, c.client_type, c.full_name, c.email,
    c.company_name, c.inn, c.kpp, c.ogrn, c.legal_address, c.company_contact,
    rr.created_at
FROM rental_requests AS rr
JOIN clients AS c ON c.id = rr.client_id;

CREATE TABLE order_documents (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    rental_request_id UUID NOT NULL REFERENCES rental_requests(id) ON DELETE CASCADE,
    order_number TEXT NOT NULL,
    document_type TEXT NOT NULL CHECK (
        document_type IN ('contract', 'receipt', 'invoice', 'act', 'upd', 'waybill')
    ),
    title TEXT NOT NULL CHECK (NULLIF(BTRIM(title), '') IS NOT NULL),
    file_url TEXT NOT NULL CHECK (NULLIF(BTRIM(file_url), '') IS NOT NULL),
    mime_type TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (rental_request_id, document_type)
);

CREATE INDEX order_documents_rental_created_idx
    ON order_documents(rental_request_id, created_at, id);
CREATE INDEX order_documents_order_number_idx ON order_documents(order_number);
