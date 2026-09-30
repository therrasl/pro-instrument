CREATE TABLE client_organizations (
    client_id UUID PRIMARY KEY REFERENCES clients(id) ON DELETE CASCADE,
    company_name TEXT NOT NULL CHECK (NULLIF(BTRIM(company_name), '') IS NOT NULL),
    inn TEXT NOT NULL CHECK (inn ~ '^[0-9]{10}([0-9]{2})?$'),
    kpp TEXT CHECK (kpp IS NULL OR kpp ~ '^[0-9]{9}$'),
    ogrn TEXT NOT NULL CHECK (ogrn ~ '^[0-9]{13}([0-9]{2})?$'),
    legal_address TEXT NOT NULL CHECK (NULLIF(BTRIM(legal_address), '') IS NOT NULL),
    actual_address TEXT,
    settlement_account TEXT CHECK (settlement_account IS NULL OR settlement_account ~ '^[0-9]{20}$'),
    bik TEXT CHECK (bik IS NULL OR bik ~ '^[0-9]{9}$'),
    correspondent_account TEXT CHECK (correspondent_account IS NULL OR correspondent_account ~ '^[0-9]{20}$'),
    bank_name TEXT,
    email TEXT NOT NULL CHECK (NULLIF(BTRIM(email), '') IS NOT NULL),
    phone TEXT NOT NULL CHECK (phone ~ '^\+7[0-9]{10}$'),
    contact_full_name TEXT NOT NULL CHECK (NULLIF(BTRIM(contact_full_name), '') IS NOT NULL),
    contact_position TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

INSERT INTO client_organizations (
    client_id, company_name, inn, kpp, ogrn, legal_address,
    email, phone, contact_full_name, created_at, updated_at
)
SELECT
    id, company_name, inn, NULLIF(kpp, ''), ogrn, legal_address,
    email, phone, COALESCE(NULLIF(company_contact, ''), NULLIF(full_name, ''), company_name),
    created_at, updated_at
FROM clients
WHERE client_type = 'legal_entity'
  AND NULLIF(BTRIM(company_name), '') IS NOT NULL
  AND inn ~ '^[0-9]{10}([0-9]{2})?$'
  AND ogrn ~ '^[0-9]{13}([0-9]{2})?$'
  AND NULLIF(BTRIM(legal_address), '') IS NOT NULL
  AND NULLIF(BTRIM(email), '') IS NOT NULL
ON CONFLICT (client_id) DO NOTHING;

ALTER TABLE rental_customer_snapshots
    ADD COLUMN actual_address TEXT,
    ADD COLUMN settlement_account TEXT,
    ADD COLUMN bik TEXT,
    ADD COLUMN correspondent_account TEXT,
    ADD COLUMN bank_name TEXT,
    ADD COLUMN organization_phone TEXT,
    ADD COLUMN contact_position TEXT;

UPDATE rental_customer_snapshots AS snapshot
SET actual_address = organization.actual_address,
    settlement_account = organization.settlement_account,
    bik = organization.bik,
    correspondent_account = organization.correspondent_account,
    bank_name = organization.bank_name,
    organization_phone = organization.phone,
    contact_position = organization.contact_position
FROM rental_requests AS rental
JOIN client_organizations AS organization ON organization.client_id = rental.client_id
WHERE snapshot.rental_request_id = rental.id;

ALTER TABLE order_documents
    ADD COLUMN storage_key TEXT,
    ALTER COLUMN file_url DROP NOT NULL;
ALTER TABLE order_documents DROP CONSTRAINT order_documents_file_url_check;

ALTER TABLE order_documents DROP CONSTRAINT order_documents_document_type_check;
UPDATE order_documents SET document_type = CASE document_type
    WHEN 'contract' THEN 'rental_contract'
    WHEN 'receipt' THEN 'payment_receipt'
    WHEN 'act' THEN 'transfer_act'
    WHEN 'upd' THEN 'closing_document'
    WHEN 'waybill' THEN 'return_act'
    ELSE document_type
END;
ALTER TABLE order_documents ADD CONSTRAINT order_documents_document_type_check CHECK (
    document_type IN (
        'rental_contract', 'invoice', 'payment_receipt',
        'transfer_act', 'return_act', 'closing_document'
    )
);
ALTER TABLE order_documents ADD CONSTRAINT order_documents_location_check CHECK (
    NULLIF(BTRIM(storage_key), '') IS NOT NULL OR NULLIF(BTRIM(file_url), '') IS NOT NULL
);

CREATE INDEX client_organizations_inn_idx ON client_organizations(inn);
CREATE INDEX order_documents_storage_key_idx ON order_documents(storage_key)
    WHERE storage_key IS NOT NULL;
