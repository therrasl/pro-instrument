DROP INDEX IF EXISTS order_documents_storage_key_idx;
DROP INDEX IF EXISTS client_organizations_inn_idx;

ALTER TABLE order_documents DROP CONSTRAINT IF EXISTS order_documents_location_check;
ALTER TABLE order_documents DROP CONSTRAINT IF EXISTS order_documents_document_type_check;
UPDATE order_documents SET document_type = CASE document_type
    WHEN 'rental_contract' THEN 'contract'
    WHEN 'payment_receipt' THEN 'receipt'
    WHEN 'transfer_act' THEN 'act'
    WHEN 'return_act' THEN 'waybill'
    WHEN 'closing_document' THEN 'upd'
    ELSE document_type
END;
ALTER TABLE order_documents ADD CONSTRAINT order_documents_document_type_check CHECK (
    document_type IN ('contract', 'receipt', 'invoice', 'act', 'upd', 'waybill')
);
UPDATE order_documents SET file_url = 'storage://' || storage_key
WHERE file_url IS NULL AND storage_key IS NOT NULL;
ALTER TABLE order_documents ALTER COLUMN file_url SET NOT NULL;
ALTER TABLE order_documents DROP COLUMN storage_key;

ALTER TABLE rental_customer_snapshots
    DROP COLUMN contact_position,
    DROP COLUMN organization_phone,
    DROP COLUMN bank_name,
    DROP COLUMN correspondent_account,
    DROP COLUMN bik,
    DROP COLUMN settlement_account,
    DROP COLUMN actual_address;

DROP TABLE client_organizations;
