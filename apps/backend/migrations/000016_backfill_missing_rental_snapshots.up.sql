INSERT INTO rental_customer_snapshots (
    rental_request_id, order_number, client_type, full_name, email,
    company_name, inn, kpp, ogrn, legal_address, company_contact,
    actual_address, settlement_account, bik, correspondent_account,
    bank_name, organization_phone, contact_position, created_at
)
SELECT
    rental.id, rental.order_number, client.client_type, client.full_name, client.email,
    COALESCE(organization.company_name, client.company_name),
    COALESCE(organization.inn, client.inn),
    COALESCE(organization.kpp, client.kpp),
    COALESCE(organization.ogrn, client.ogrn),
    COALESCE(organization.legal_address, client.legal_address),
    COALESCE(organization.contact_full_name, client.company_contact),
    organization.actual_address, organization.settlement_account,
    organization.bik, organization.correspondent_account, organization.bank_name,
    organization.phone, organization.contact_position, rental.created_at
FROM rental_requests AS rental
JOIN clients AS client ON client.id = rental.client_id
LEFT JOIN client_organizations AS organization ON organization.client_id = client.id
LEFT JOIN rental_customer_snapshots AS snapshot ON snapshot.rental_request_id = rental.id
WHERE snapshot.rental_request_id IS NULL
ON CONFLICT (rental_request_id) DO NOTHING;
