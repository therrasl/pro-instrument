ALTER TABLE order_documents DROP CONSTRAINT IF EXISTS order_documents_file_url_check;
ALTER TABLE order_documents ADD CONSTRAINT order_documents_file_url_check CHECK (
    file_url IS NULL OR NULLIF(BTRIM(file_url), '') IS NOT NULL
);
