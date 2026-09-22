CREATE TABLE IF NOT EXISTS bad_url (
    h BLOB PRIMARY KEY,
    host TEXT NOT NULL,
    src INTEGER NOT NULL
) WITHOUT ROWID;

CREATE INDEX IF NOT EXISTS bad_url_host ON bad_url(host);

CREATE TABLE IF NOT EXISTS sync_status (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    finished_at INTEGER,
    rows INTEGER,
    added INTEGER,
    deleted INTEGER,
    sources_ok TEXT,
    sources_failed TEXT,
    duration_ms INTEGER
);
