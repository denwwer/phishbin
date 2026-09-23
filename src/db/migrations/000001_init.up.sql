CREATE TABLE prev (
    h BLOB PRIMARY KEY,
    src INTEGER NOT NULL,
    reasons TEXT NOT NULL DEFAULT 'malware'
) WITHOUT ROWID;

CREATE TABLE curr (
    h BLOB PRIMARY KEY,
    src INTEGER NOT NULL,
    reasons TEXT NOT NULL DEFAULT 'malware'
) WITHOUT ROWID;

CREATE TABLE feed_history (
    provider TEXT PRIMARY KEY,
    fetchAt TEXT,
    records INTEGER,
    last_error TEXT
);

CREATE TABLE settings (
    name TEXT PRIMARY KEY,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);
