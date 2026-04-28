package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"log/slog"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// migration describes one numbered SQL file under migrations/. Parsed from its
// filename: NNN_description.up.sql -> version=NNN, description="description".
type migration struct {
	version     int
	description string
	filename    string
}

// fileNameRe matches "NNN_description.up.sql" and captures both halves.
var fileNameRe = regexp.MustCompile(`^(\d+)_([a-z0-9_]+)\.up\.sql$`)

// schemaMigrationsDDL ensures the tracking table exists before any migration
// logic runs.
const schemaMigrationsDDL = `
CREATE TABLE IF NOT EXISTS schema_migrations (
  version     INTEGER PRIMARY KEY,
  description TEXT    NOT NULL,
  applied_at  TEXT    NOT NULL DEFAULT (datetime('now'))
)`

// Migrate applies every pending migration from the embedded migrations/
// directory to db, in ascending version order, inside a transaction per
// migration. Migrations already recorded in schema_migrations are skipped.
//
// logger receives one Info log per applied migration at level Info. Pass
// slog.Default() for standard behavior; pass a discarding logger to silence
// upgrade notices. Migrate is safe to call on every startup: if no migrations
// are pending it is a no-op beyond the schema_migrations CREATE IF NOT EXISTS
// and one SELECT per migration file.
func Migrate(ctx context.Context, db *sql.DB, logger *slog.Logger) error {
	if logger == nil {
		logger = slog.Default()
	}

	if _, err := db.ExecContext(ctx, schemaMigrationsDDL); err != nil {
		return fmt.Errorf("sqlite: migrate: create schema_migrations: %w", err)
	}

	migrations, err := loadMigrations()
	if err != nil {
		return fmt.Errorf("sqlite: migrate: load migrations: %w", err)
	}

	applied, err := appliedVersions(ctx, db)
	if err != nil {
		return fmt.Errorf("sqlite: migrate: read applied versions: %w", err)
	}

	for _, m := range migrations {
		if applied[m.version] {
			continue
		}
		if err := applyOne(ctx, db, m); err != nil {
			return err
		}
		logger.InfoContext(ctx, "applying lore migration",
			"version", m.version,
			"description", m.description,
		)
	}

	return nil
}

// appliedVersions returns the set of migration versions already recorded.
func appliedVersions(ctx context.Context, db *sql.DB) (map[int]bool, error) {
	rows, err := db.QueryContext(ctx, `SELECT version FROM schema_migrations`)
	if err != nil {
		return nil, fmt.Errorf("query schema_migrations: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := map[int]bool{}
	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			return nil, fmt.Errorf("scan schema_migrations: %w", err)
		}
		out[v] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate schema_migrations: %w", err)
	}
	return out, nil
}

// loadMigrations walks migrationFS and returns migrations sorted by version.
func loadMigrations() ([]migration, error) {
	entries, err := fs.ReadDir(migrationFS, "migrations")
	if err != nil {
		return nil, fmt.Errorf("read embedded migrations dir: %w", err)
	}

	out := make([]migration, 0, len(entries))
	seen := map[int]string{}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		match := fileNameRe.FindStringSubmatch(name)
		if match == nil {
			continue
		}
		v, err := strconv.Atoi(match[1])
		if err != nil {
			return nil, fmt.Errorf("parse version in %q: %w", name, err)
		}
		if prior, dup := seen[v]; dup {
			return nil, fmt.Errorf("duplicate migration version %d: %q and %q", v, prior, name)
		}
		seen[v] = name
		out = append(out, migration{
			version:     v,
			description: strings.ReplaceAll(match[2], "_", " "),
			filename:    name,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].version < out[j].version })
	return out, nil
}

// applyOne executes every statement in migration m inside a single transaction
// and records the version in schema_migrations. All statements land or none do.
func applyOne(ctx context.Context, db *sql.DB, m migration) error {
	raw, err := fs.ReadFile(migrationFS, "migrations/"+m.filename)
	if err != nil {
		return fmt.Errorf("sqlite: migrate: read %s: %w", m.filename, err)
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("sqlite: migrate: begin tx (version %d): %w", m.version, err)
	}
	defer func() { _ = tx.Rollback() }()

	for i, stmt := range splitStatements(string(raw)) {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("sqlite: migrate: version %d statement %d: %w", m.version, i+1, err)
		}
	}

	if _, err := tx.ExecContext(ctx,
		`INSERT INTO schema_migrations (version, description, applied_at) VALUES (?, ?, ?)`,
		m.version, m.description, time.Now().UTC().Format(time.RFC3339),
	); err != nil {
		return fmt.Errorf("sqlite: migrate: record version %d: %w", m.version, err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("sqlite: migrate: commit version %d: %w", m.version, err)
	}
	return nil
}

// splitStatements splits a SQL script into individual statements. It handles
// CREATE TRIGGER bodies (which embed semicolons inside BEGIN...END) by tracking
// nesting depth: a statement ends at ";" only when depth is zero.
func splitStatements(script string) []string {
	var cleaned strings.Builder
	for _, line := range strings.Split(script, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "--") {
			cleaned.WriteByte('\n')
			continue
		}
		if idx := strings.Index(line, " --"); idx >= 0 {
			line = line[:idx]
		}
		cleaned.WriteString(line)
		cleaned.WriteByte('\n')
	}
	src := cleaned.String()
	upper := strings.ToUpper(src)

	var (
		stmts []string
		buf   strings.Builder
		depth int
	)

	isBoundary := func(b byte) bool {
		return b == 0 || b == ' ' || b == '\n' || b == '\r' || b == '\t' || b == ';'
	}

	for i := 0; i < len(src); i++ {
		if i+5 <= len(upper) && upper[i:i+5] == "BEGIN" {
			var next byte
			if i+5 < len(upper) {
				next = upper[i+5]
			}
			var prev byte
			if i > 0 {
				prev = upper[i-1]
			}
			if isBoundary(next) && (i == 0 || isBoundary(prev)) {
				depth++
			}
		}
		if i+3 <= len(upper) && upper[i:i+3] == "END" {
			var next byte
			if i+3 < len(upper) {
				next = upper[i+3]
			}
			var prev byte
			if i > 0 {
				prev = upper[i-1]
			}
			if isBoundary(next) && (i == 0 || isBoundary(prev)) {
				if depth > 0 {
					depth--
				}
			}
		}

		if src[i] == ';' && depth == 0 {
			stmt := strings.TrimSpace(buf.String())
			if stmt != "" {
				stmts = append(stmts, stmt)
			}
			buf.Reset()
			continue
		}
		buf.WriteByte(src[i])
	}
	if stmt := strings.TrimSpace(buf.String()); stmt != "" {
		stmts = append(stmts, stmt)
	}
	return stmts
}
