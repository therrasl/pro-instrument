ALTER TABLE verification_audit_logs
    DROP CONSTRAINT verification_audit_logs_action_check;

ALTER TABLE verification_audit_logs
    ADD CONSTRAINT verification_audit_logs_action_check
    CHECK (
        action IN (
            'document_uploaded',
            'document_deleted',
            'verification_approved',
            'verification_rejected'
        )
    );
