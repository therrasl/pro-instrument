DROP TABLE IF EXISTS order_documents;
DROP TABLE IF EXISTS rental_customer_snapshots;

ALTER TABLE clients
    DROP COLUMN IF EXISTS company_contact,
    DROP COLUMN IF EXISTS legal_address,
    DROP COLUMN IF EXISTS ogrn,
    DROP COLUMN IF EXISTS kpp,
    DROP COLUMN IF EXISTS inn,
    DROP COLUMN IF EXISTS company_name,
    DROP COLUMN IF EXISTS client_type;

ALTER TABLE rental_requests DROP COLUMN IF EXISTS order_number;
DROP SEQUENCE IF EXISTS rental_order_number_seq;
