-- 001_initial.up.sql
-- Baseline schema for the lore SQLite store.
-- Applied by pkg/lore/store/sqlite inside a single transaction.

-- ---------------------------------------------------------------------------
-- Entries: the primary content table.
--
-- tags and metadata are stored as JSON text: tags as a JSON array of strings
-- (e.g. '["adr","architecture"]') and metadata as a JSON object
-- (e.g. '{"component":"auth"}').
-- ---------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS entries (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  project    TEXT    NOT NULL DEFAULT '',
  kind       TEXT    NOT NULL,
  title      TEXT    NOT NULL,
  body       TEXT    NOT NULL DEFAULT '',
  source     TEXT    NOT NULL DEFAULT '',
  tags       TEXT    NOT NULL DEFAULT '[]',
  metadata   TEXT    NOT NULL DEFAULT '{}',
  created_at TEXT    NOT NULL DEFAULT (datetime('now')),
  updated_at TEXT    NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_entries_project   ON entries(project);
CREATE INDEX IF NOT EXISTS idx_entries_kind      ON entries(kind);
CREATE INDEX IF NOT EXISTS idx_entries_source    ON entries(source) WHERE source != '';

-- ---------------------------------------------------------------------------
-- Edges: directed typed links between entries.
-- ---------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS edges (
  from_id    INTEGER NOT NULL,
  to_id      INTEGER NOT NULL,
  relation   TEXT    NOT NULL,
  weight     REAL    NOT NULL DEFAULT 0,
  created_at TEXT    NOT NULL DEFAULT (datetime('now')),
  PRIMARY KEY (from_id, to_id, relation),
  FOREIGN KEY (from_id) REFERENCES entries(id) ON DELETE CASCADE,
  FOREIGN KEY (to_id)   REFERENCES entries(id) ON DELETE CASCADE
);

-- ---------------------------------------------------------------------------
-- FTS5 virtual table for full-text search over title + body.
--
-- content=entries + content_rowid=id makes this a content shadow index:
-- the FTS table follows entries.id as the rowid and delegates content reads
-- back to entries so content is stored once.
-- ---------------------------------------------------------------------------

CREATE VIRTUAL TABLE IF NOT EXISTS entries_fts USING fts5(
  title, body,
  content=entries, content_rowid=id
);

-- FTS5 sync triggers. Fire on title or body changes only so routine
-- metadata updates (source, tags, metadata) do not re-index the row.

CREATE TRIGGER IF NOT EXISTS entries_ai AFTER INSERT ON entries BEGIN
  INSERT INTO entries_fts(rowid, title, body)
  VALUES (new.id, new.title, new.body);
END;

CREATE TRIGGER IF NOT EXISTS entries_ad AFTER DELETE ON entries BEGIN
  INSERT INTO entries_fts(entries_fts, rowid, title, body)
  VALUES ('delete', old.id, old.title, old.body);
END;

CREATE TRIGGER IF NOT EXISTS entries_au AFTER UPDATE OF title, body ON entries BEGIN
  INSERT INTO entries_fts(entries_fts, rowid, title, body)
  VALUES ('delete', old.id, old.title, old.body);
  INSERT INTO entries_fts(rowid, title, body)
  VALUES (new.id, new.title, new.body);
END;

-- ---------------------------------------------------------------------------
-- schema_migrations: tracks applied migrations so Migrate is idempotent.
-- ---------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS schema_migrations (
  version     INTEGER PRIMARY KEY,
  description TEXT    NOT NULL,
  applied_at  TEXT    NOT NULL DEFAULT (datetime('now'))
);
