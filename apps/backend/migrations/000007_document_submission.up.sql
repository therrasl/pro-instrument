UPDATE clients
SET
    status = 'documents_uploaded',
    updated_at = NOW()
WHERE status = 'pending_verification';
