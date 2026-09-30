-- 000017_rental_extensions_and_inspection_photos.down.sql

DROP TABLE IF EXISTS rental_inspection_photos;
ALTER TABLE payments DROP COLUMN IF EXISTS extension_id;
ALTER TABLE payments DROP COLUMN IF EXISTS payment_type;
DROP TABLE IF EXISTS rental_extensions;
