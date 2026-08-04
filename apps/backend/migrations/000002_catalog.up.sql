CREATE TABLE categories (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name TEXT NOT NULL,
    slug TEXT NOT NULL UNIQUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE tools (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    category_id UUID NOT NULL REFERENCES categories(id) ON DELETE RESTRICT,
    name TEXT NOT NULL,
    slug TEXT NOT NULL UNIQUE,
    short_description TEXT NOT NULL DEFAULT '',
    description TEXT NOT NULL DEFAULT '',
    specifications JSONB NOT NULL DEFAULT '{}'::JSONB,
    equipment JSONB NOT NULL DEFAULT '[]'::JSONB,
    daily_price INTEGER NOT NULL CHECK (daily_price >= 0),
    deposit_amount INTEGER NOT NULL CHECK (deposit_amount >= 0),
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE tool_units (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tool_id UUID NOT NULL REFERENCES tools(id) ON DELETE RESTRICT,
    inventory_number TEXT NOT NULL UNIQUE,
    status TEXT NOT NULL CHECK (
        status IN (
            'available',
            'pending_confirmation',
            'reserved',
            'paid',
            'ready_for_pickup',
            'issued',
            'rented',
            'awaiting_return',
            'returned',
            'inspection',
            'repair',
            'written_off',
            'unavailable'
        )
    ),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX tools_category_id_idx ON tools(category_id);
CREATE INDEX tools_active_name_idx ON tools(is_active, name);
CREATE INDEX tool_units_tool_id_status_idx ON tool_units(tool_id, status);
