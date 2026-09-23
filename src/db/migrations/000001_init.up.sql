CREATE TABLE prev (
    h BLOB PRIMARY KEY,
    pId INTEGER NOT NULL,
    reasons TEXT NOT NULL DEFAULT 'malware'
) WITHOUT ROWID;

-- sync changes with src/worker/service.go Service.rotate()
CREATE TABLE curr (
    h BLOB PRIMARY KEY,
    pId INTEGER NOT NULL,
    reasons TEXT NOT NULL DEFAULT 'malware'
) WITHOUT ROWID;

CREATE TABLE feed_history (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    provider TEXT,
    pId INTEGER NOT NULL,
    fetchAt TEXT,
    records INTEGER,
    last_error TEXT
);
CREATE INDEX IF NOT EXISTS idx_feed_history_last_error ON feed_history(last_error);

CREATE TABLE settings (
    name TEXT PRIMARY KEY,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
) WITHOUT ROWID;
