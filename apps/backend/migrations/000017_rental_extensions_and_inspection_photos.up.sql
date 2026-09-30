-- 000017_rental_extensions_and_inspection_photos.up.sql

-- 1. Таблица продлений аренды
CREATE TABLE IF NOT EXISTS rental_extensions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    rental_request_id UUID NOT NULL REFERENCES rental_requests(id) ON DELETE RESTRICT,
    previous_end_date DATE NOT NULL,
    new_end_date DATE NOT NULL,
    additional_days INTEGER NOT NULL CHECK (additional_days > 0),
    daily_price BIGINT NOT NULL CHECK (daily_price >= 0),
    amount BIGINT NOT NULL CHECK (amount > 0),
    status TEXT NOT NULL DEFAULT 'pending_payment' CHECK (status IN ('pending_payment', 'paid', 'cancelled', 'expired')),
    payment_id UUID REFERENCES payments(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    paid_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS rental_extensions_rental_idx ON rental_extensions(rental_request_id, created_at DESC);

-- 2. Дополнительные колонки в payments для поддержки платежей продления
ALTER TABLE payments ADD COLUMN IF NOT EXISTS payment_type TEXT NOT NULL DEFAULT 'initial' CHECK (payment_type IN ('initial', 'extension'));
ALTER TABLE payments ADD COLUMN IF NOT EXISTS extension_id UUID REFERENCES rental_extensions(id) ON DELETE SET NULL;

-- 3. Таблица фотофиксации состояния инструмента
CREATE TABLE IF NOT EXISTS rental_inspection_photos (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    rental_request_id UUID NOT NULL REFERENCES rental_requests(id) ON DELETE RESTRICT,
    phase TEXT NOT NULL CHECK (phase IN ('handover', 'return')),
    photo_type TEXT NOT NULL CHECK (photo_type IN ('body', 'equipment', 'battery', 'serial_number', 'cleanliness')),
    storage_key TEXT NOT NULL,
    file_name TEXT NOT NULL,
    mime_type TEXT NOT NULL,
    file_size BIGINT NOT NULL,
    comment TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT rental_inspection_photos_unique_phase_type UNIQUE (rental_request_id, phase, photo_type)
);

CREATE INDEX IF NOT EXISTS rental_inspection_photos_rental_phase_idx ON rental_inspection_photos(rental_request_id, phase, photo_type);
