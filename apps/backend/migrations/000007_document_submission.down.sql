UPDATE clients
SET
    status = 'pending_verification',
    updated_at = NOW()
WHERE
    status = 'documents_uploaded'
    AND (
        SELECT COUNT(DISTINCT document_type)
        FROM client_documents
        WHERE client_id = clients.id
    ) = 3;
