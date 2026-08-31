-- 001_init: Create sessions and turns tables for crtx envelopes.
CREATE TABLE IF NOT EXISTS sessions (
    id            TEXT PRIMARY KEY,
    crtx_version  TEXT NOT NULL DEFAULT '0.1',
    source_kind   TEXT NOT NULL DEFAULT 'stem',
    source_ver    TEXT NOT NULL DEFAULT '',
    source_inst   TEXT NOT NULL DEFAULT '',
    parent_id     TEXT NOT NULL DEFAULT '',
    fork_point    INTEGER,
    metadata      TEXT NOT NULL DEFAULT '{}',
    created_at    TEXT NOT NULL,
    updated_at    TEXT NOT NULL,
    closed_at     TEXT
);

CREATE TABLE IF NOT EXISTS turns (
    id          TEXT NOT NULL,
    session_id  TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    role        TEXT NOT NULL,
    content     TEXT NOT NULL DEFAULT '[]',
    metadata    TEXT NOT NULL DEFAULT '{}',
    created_at  TEXT NOT NULL,
    seq         INTEGER NOT NULL,
    PRIMARY KEY (session_id, seq)
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_turns_session_seq
    ON turns(session_id, seq);

CREATE INDEX IF NOT EXISTS idx_sessions_parent
    ON sessions(parent_id);

CREATE INDEX IF NOT EXISTS idx_sessions_created
    ON sessions(created_at);
