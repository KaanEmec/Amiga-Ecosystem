-- Initial schema. Timestamps are INTEGER Unix seconds (UTC).
-- JSON columns are TEXT validated with json_valid.

-- Catalogue entries. doc is the entry object exactly as in the seed/export.
-- ord keeps the document order stable across export round-trips.
-- Rows are never hard-deleted: removal sets deleted_at.
CREATE TABLE entries (
    id         TEXT    PRIMARY KEY,
    doc        TEXT    NOT NULL CHECK (json_valid(doc)),
    rev        INTEGER NOT NULL DEFAULT 1 CHECK (rev >= 1),
    ord        INTEGER NOT NULL,
    deleted_at INTEGER,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
) STRICT, WITHOUT ROWID;

CREATE INDEX entries_ord ON entries (ord);

-- meta, domains, tours, _readme.
CREATE TABLE catalog_meta (
    key TEXT PRIMARY KEY CHECK (key IN ('meta', 'domains', 'tours', '_readme')),
    doc TEXT NOT NULL CHECK (json_valid(doc))
) STRICT, WITHOUT ROWID;

-- Public proposals.
CREATE TABLE suggestions (
    id            INTEGER PRIMARY KEY,
    type          TEXT    NOT NULL CHECK (type IN ('new', 'change', 'remove', 'image')),
    entry_id      TEXT    REFERENCES entries (id),
    title         TEXT    NOT NULL,
    summary       TEXT    NOT NULL DEFAULT '',
    payload       TEXT    NOT NULL DEFAULT '{}' CHECK (json_valid(payload)),
    snapshot      TEXT    CHECK (snapshot IS NULL OR json_valid(snapshot)),
    base_rev      INTEGER,
    reason        TEXT    NOT NULL DEFAULT '',
    sources       TEXT    NOT NULL DEFAULT '[]' CHECK (json_valid(sources)),
    contact       TEXT    NOT NULL DEFAULT '',
    status        TEXT    NOT NULL DEFAULT 'pending'
                  CHECK (status IN ('pending', 'accepted', 'accepted_edited', 'rejected', 'duplicate', 'hidden')),
    duplicate_of  INTEGER REFERENCES suggestions (id),
    decision_note TEXT    NOT NULL DEFAULT '',
    internal_note TEXT    NOT NULL DEFAULT '',
    final         TEXT    CHECK (final IS NULL OR json_valid(final)),
    voter_id      TEXT    NOT NULL,
    ip_hash       TEXT    NOT NULL DEFAULT '',
    created_at    INTEGER NOT NULL,
    decided_at    INTEGER
) STRICT;

CREATE INDEX suggestions_status_created ON suggestions (status, created_at);
CREATE INDEX suggestions_entry ON suggestions (entry_id);

-- One row per publication (create, edit, delete, restore, import).
-- before is NULL for a creation, after is NULL for a deletion.
CREATE TABLE entry_revisions (
    id            INTEGER PRIMARY KEY,
    entry_id      TEXT    NOT NULL REFERENCES entries (id),
    rev           INTEGER NOT NULL,
    before        TEXT    CHECK (before IS NULL OR json_valid(before)),
    after         TEXT    CHECK (after IS NULL OR json_valid(after)),
    actor         TEXT    NOT NULL,
    reason        TEXT    NOT NULL DEFAULT '',
    suggestion_id INTEGER REFERENCES suggestions (id),
    created_at    INTEGER NOT NULL,
    UNIQUE (entry_id, rev)
) STRICT;

CREATE INDEX entry_revisions_suggestion ON entry_revisions (suggestion_id);

-- +1 votes; the signed browser-id cookie is voter_id.
CREATE TABLE votes (
    suggestion_id INTEGER NOT NULL REFERENCES suggestions (id) ON DELETE CASCADE,
    voter_id      TEXT    NOT NULL,
    created_at    INTEGER NOT NULL,
    PRIMARY KEY (suggestion_id, voter_id)
) STRICT, WITHOUT ROWID;

CREATE INDEX votes_voter ON votes (voter_id);

CREATE TABLE moderation_log (
    id            INTEGER PRIMARY KEY,
    suggestion_id INTEGER REFERENCES suggestions (id),
    entry_id      TEXT    REFERENCES entries (id),
    action        TEXT    NOT NULL,
    detail        TEXT    NOT NULL DEFAULT '{}' CHECK (json_valid(detail)),
    created_at    INTEGER NOT NULL
) STRICT;

CREATE INDEX moderation_log_suggestion ON moderation_log (suggestion_id);
CREATE INDEX moderation_log_entry ON moderation_log (entry_id);
CREATE INDEX moderation_log_created ON moderation_log (created_at);

-- Provenance for every published image. path is relative to the media dir.
CREATE TABLE images (
    id            INTEGER PRIMARY KEY,
    suggestion_id INTEGER REFERENCES suggestions (id),
    entry_id      TEXT    REFERENCES entries (id),
    source_url    TEXT    NOT NULL,
    source_page   TEXT    NOT NULL DEFAULT '',
    credit        TEXT    NOT NULL DEFAULT '',
    path          TEXT    NOT NULL UNIQUE,
    content_type  TEXT    NOT NULL,
    width         INTEGER NOT NULL,
    height        INTEGER NOT NULL,
    bytes         INTEGER NOT NULL,
    sha256        TEXT    NOT NULL,
    created_at    INTEGER NOT NULL
) STRICT;

CREATE INDEX images_entry ON images (entry_id);
CREATE INDEX images_suggestion ON images (suggestion_id);

-- Single owner account.
CREATE TABLE owner (
    id             INTEGER PRIMARY KEY CHECK (id = 1),
    username       TEXT    NOT NULL,
    password_hash  TEXT    NOT NULL,
    totp_secret    TEXT    NOT NULL,
    recovery_codes TEXT    NOT NULL DEFAULT '[]' CHECK (json_valid(recovery_codes)),
    created_at     INTEGER NOT NULL
) STRICT;

-- Server-side owner sessions; only the token hash is stored.
CREATE TABLE sessions (
    token_hash TEXT    PRIMARY KEY,
    created_at INTEGER NOT NULL,
    expires_at INTEGER NOT NULL,
    last_seen  INTEGER NOT NULL
) STRICT, WITHOUT ROWID;

CREATE INDEX sessions_expires ON sessions (expires_at);

CREATE TABLE login_attempts (
    key TEXT    NOT NULL,
    at  INTEGER NOT NULL
) STRICT;

CREATE INDEX login_attempts_key_at ON login_attempts (key, at);

-- Rate-limit events, pruned on write.
CREATE TABLE rate_events (
    key  TEXT    NOT NULL,
    kind TEXT    NOT NULL,
    at   INTEGER NOT NULL
) STRICT;

CREATE INDEX rate_events_key_at ON rate_events (key, at);
CREATE INDEX rate_events_at ON rate_events (at);

-- Small key/value switches: submissions_paused, voting_paused.
CREATE TABLE settings (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
) STRICT, WITHOUT ROWID;
