CREATE TABLE sync_watermarks (
    key TEXT PRIMARY KEY,
    last_synced_at TIMESTAMP WITH TIME ZONE NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_abuse_created_at ON abuse (created_at);
