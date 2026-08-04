CREATE TABLE tool_images (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tool_id UUID NOT NULL REFERENCES tools(id) ON DELETE CASCADE,
    image_url TEXT NOT NULL CHECK (BTRIM(image_url) <> ''),
    position INTEGER NOT NULL DEFAULT 0 CHECK (position >= 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tool_id, image_url)
);

CREATE INDEX tool_images_tool_id_position_idx ON tool_images(tool_id, position, id);

INSERT INTO tool_images (tool_id, image_url, position)
VALUES
    (
        '20000000-0000-4000-8000-000000000001',
        '/static/tools/rotary-hammer.jpg',
        0
    ),
    (
        '20000000-0000-4000-8000-000000000002',
        '/static/tools/circular-saw.jpg',
        0
    ),
    (
        '20000000-0000-4000-8000-000000000003',
        '/static/tools/lawn-mower.jpg',
        0
    );
