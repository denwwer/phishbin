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

CREATE TABLE feed_meta (
    source TEXT PRIMARY KEY,
    etag TEXT,
    last_modified TEXT,
    last_count INTEGER,
    last_error TEXT
);

CREATE TABLE settings (
    name TEXT PRIMARY KEY,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);
