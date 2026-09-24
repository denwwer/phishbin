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
    jobId TEXT NOT NULL,
    provider TEXT NOT NULL,
    pId INTEGER NOT NULL,
    fetchAt TEXT NOT NULL,
    records INTEGER,
    lastError TEXT
);
CREATE INDEX IF NOT EXISTS idx_feed_history_jobId_last_error ON feed_history(jobId, lastError);
