-- User preferences stored as validated key/value pairs.
CREATE TABLE preferences (
    key        TEXT    PRIMARY KEY CHECK (length(key) BETWEEN 1 AND 64),
    value      TEXT    NOT NULL CHECK (length(value) <= 256),
    updated_at INTEGER NOT NULL
);
