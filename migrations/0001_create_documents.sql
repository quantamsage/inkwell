-- Documents: one row per Markdown document.
-- Timestamps are Unix epoch milliseconds (UTC).
-- version implements optimistic concurrency for autosave.
CREATE TABLE documents (
    id         TEXT    PRIMARY KEY CHECK (length(id) = 32 AND id NOT GLOB '*[^0-9a-f]*'),
    title      TEXT    NOT NULL CHECK (length(title) BETWEEN 1 AND 200),
    content    TEXT    NOT NULL DEFAULT '',
    version    INTEGER NOT NULL DEFAULT 1 CHECK (version >= 1),
    created_at INTEGER NOT NULL CHECK (created_at > 0),
    updated_at INTEGER NOT NULL CHECK (updated_at >= created_at)
);

CREATE INDEX idx_documents_updated_at ON documents (updated_at DESC);
CREATE INDEX idx_documents_created_at ON documents (created_at DESC);
CREATE INDEX idx_documents_title      ON documents (title COLLATE NOCASE);
