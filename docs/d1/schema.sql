CREATE TABLE IF NOT EXISTS abuse_feeds (
    h BLOB PRIMARY KEY,
    pId INTEGER NOT NULL
) WITHOUT ROWID;

CREATE TABLE IF NOT EXISTS abuse_feeds_status (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    lastSyncAt INTEGER,
    rows INTEGER,
    providersFailed TEXT,
    durationMs INTEGER
);
