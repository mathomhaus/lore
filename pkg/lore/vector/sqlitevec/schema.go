package sqlitevec

// schemaSQL creates the vectors table if it does not already exist.
// DDL is run by New via migrate; callers never invoke this directly.
//
// Table layout:
//
//	entry_id   - PRIMARY KEY; mirrors the lore_entries.id this vector belongs to.
//	dim        - INTEGER recording the dimension of this row's vector. Allows
//	             the migration to detect schema/dimension mismatches at runtime
//	             rather than silently returning garbage similarity scores.
//	vec        - BLOB holding the raw vector as little-endian float32 bytes.
//	             Each float32 occupies 4 bytes (IEEE 754); total BLOB size is
//	             dim*4 bytes. Encoding documented in encode.go.
//	updated_at - TEXT storing an RFC3339 timestamp, updated on every Upsert.
//	             Useful for backfill bookkeeping and incremental sync.
const schemaSQL = `
CREATE TABLE IF NOT EXISTS vectors (
    entry_id   INTEGER PRIMARY KEY,
    dim        INTEGER NOT NULL,
    vec        BLOB    NOT NULL,
    updated_at TEXT    NOT NULL DEFAULT (datetime('now'))
);`
