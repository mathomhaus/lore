package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	// modernc.org/sqlite is a pure-Go SQLite driver (no CGO). The blank import
	// registers the "sqlite" driver name with database/sql.
	_ "modernc.org/sqlite"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/mathomhaus/lore/pkg/lore"
	"github.com/mathomhaus/lore/pkg/lore/store"
)

const defaultLimit = 50
const tracerName = "github.com/mathomhaus/lore"

// Option configures a Store constructed by New.
type Option func(*sqliteStore)

// WithLogger sets the logger used for migration progress and warnings.
// Defaults to slog.Default() when not provided.
func WithLogger(l *slog.Logger) Option {
	return func(s *sqliteStore) {
		if l != nil {
			s.logger = l
		}
	}
}

// WithTracer sets the OpenTelemetry tracer for span instrumentation.
// Defaults to otel.Tracer(tracerName) when not provided.
func WithTracer(t trace.Tracer) Option {
	return func(s *sqliteStore) {
		if t != nil {
			s.tracer = t
		}
	}
}

// WithDefaultLimit sets the default page size for list operations.
// Defaults to 50 when not provided. Values <= 0 are ignored.
func WithDefaultLimit(n int) Option {
	return func(s *sqliteStore) {
		if n > 0 {
			s.defaultLimit = n
		}
	}
}

// sqliteStore is the SQLite-backed implementation of store.Store.
type sqliteStore struct {
	db           *sql.DB
	logger       *slog.Logger
	tracer       trace.Tracer
	defaultLimit int

	mu     sync.RWMutex
	closed bool
}

// New returns a Store backed by the given *sql.DB. The caller owns the *sql.DB
// and is responsible for connection pool configuration and Close ordering.
// New runs pending migrations on the database; pass a fresh empty DB or one
// already at the latest schema_migrations version.
//
// The driver name for modernc.org/sqlite is "sqlite" (not "sqlite3"). A
// minimal DSN that applies all required pragmas per connection looks like:
//
//	"file:path.db?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=synchronous(NORMAL)&_pragma=foreign_keys(ON)"
func New(db *sql.DB, opts ...Option) (store.Store, error) {
	if db == nil {
		return nil, fmt.Errorf("sqlite: new: db must not be nil")
	}

	s := &sqliteStore{
		db:           db,
		logger:       slog.Default(),
		tracer:       otel.Tracer(tracerName),
		defaultLimit: defaultLimit,
	}
	for _, o := range opts {
		o(s)
	}

	if err := Migrate(context.Background(), db, s.logger); err != nil {
		return nil, fmt.Errorf("sqlite: new: migrate: %w", err)
	}

	return s, nil
}

// checkClosed returns ErrClosed if the store has been closed.
func (s *sqliteStore) checkClosed() error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return fmt.Errorf("sqlite: %w", lore.ErrClosed)
	}
	return nil
}

// resolveLimit applies the caller-supplied limit, falling back to the store
// default when zero.
func (s *sqliteStore) resolveLimit(limit int) int {
	if limit > 0 {
		return limit
	}
	return s.defaultLimit
}

// recordError marks a span as errored and logs the failure.
func (s *sqliteStore) recordError(span trace.Span, op string, err error) {
	span.RecordError(err)
	span.SetStatus(codes.Error, err.Error())
	s.logger.Error("lore store operation failed", "op", op, "error", err)
}

// ---- Inscribe ----

// Inscribe persists a new entry and returns its storage-assigned ID.
func (s *sqliteStore) Inscribe(ctx context.Context, e lore.Entry) (int64, error) {
	ctx, span := s.tracer.Start(ctx, "lore.store.inscribe",
		trace.WithAttributes(
			attribute.String("lore.kind", string(e.Kind)),
			attribute.String("lore.source", e.Source),
		),
	)
	defer span.End()

	if err := s.checkClosed(); err != nil {
		s.recordError(span, "inscribe", err)
		return 0, err
	}
	if err := e.Kind.Validate(); err != nil {
		s.recordError(span, "inscribe", err)
		return 0, fmt.Errorf("sqlite: inscribe: %w", err)
	}
	if strings.TrimSpace(e.Title) == "" {
		err := fmt.Errorf("sqlite: inscribe: %w: title is required", lore.ErrInvalidArgument)
		s.recordError(span, "inscribe", err)
		return 0, err
	}

	tagsJSON, err := marshalTags(e.Tags)
	if err != nil {
		s.recordError(span, "inscribe", err)
		return 0, fmt.Errorf("sqlite: inscribe: marshal tags: %w", err)
	}
	metaJSON, err := marshalMetadata(e.Metadata)
	if err != nil {
		s.recordError(span, "inscribe", err)
		return 0, fmt.Errorf("sqlite: inscribe: marshal metadata: %w", err)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO entries (project, kind, title, body, source, tags, metadata, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		e.Project, string(e.Kind), e.Title, e.Body, e.Source,
		tagsJSON, metaJSON, now, now,
	)
	if err != nil {
		wrappedErr := fmt.Errorf("sqlite: inscribe: insert: %w", err)
		s.recordError(span, "inscribe", wrappedErr)
		return 0, wrappedErr
	}

	id, err := res.LastInsertId()
	if err != nil {
		wrappedErr := fmt.Errorf("sqlite: inscribe: last insert id: %w", err)
		s.recordError(span, "inscribe", wrappedErr)
		return 0, wrappedErr
	}

	span.SetAttributes(attribute.Int64("lore.id", id))
	return id, nil
}

// ---- Update ----

// Update replaces all mutable fields of the entry identified by e.ID.
func (s *sqliteStore) Update(ctx context.Context, e lore.Entry) error {
	ctx, span := s.tracer.Start(ctx, "lore.store.update",
		trace.WithAttributes(
			attribute.Int64("lore.id", e.ID),
			attribute.String("lore.kind", string(e.Kind)),
		),
	)
	defer span.End()

	if err := s.checkClosed(); err != nil {
		s.recordError(span, "update", err)
		return err
	}
	if e.ID <= 0 {
		err := fmt.Errorf("sqlite: update: %w: id must be positive", lore.ErrInvalidArgument)
		s.recordError(span, "update", err)
		return err
	}
	if err := e.Kind.Validate(); err != nil {
		s.recordError(span, "update", err)
		return fmt.Errorf("sqlite: update: %w", err)
	}
	if strings.TrimSpace(e.Title) == "" {
		err := fmt.Errorf("sqlite: update: %w: title is required", lore.ErrInvalidArgument)
		s.recordError(span, "update", err)
		return err
	}

	tagsJSON, err := marshalTags(e.Tags)
	if err != nil {
		s.recordError(span, "update", err)
		return fmt.Errorf("sqlite: update: marshal tags: %w", err)
	}
	metaJSON, err := marshalMetadata(e.Metadata)
	if err != nil {
		s.recordError(span, "update", err)
		return fmt.Errorf("sqlite: update: marshal metadata: %w", err)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	res, err := s.db.ExecContext(ctx,
		`UPDATE entries SET project=?, kind=?, title=?, body=?, source=?, tags=?, metadata=?, updated_at=?
		 WHERE id=?`,
		e.Project, string(e.Kind), e.Title, e.Body, e.Source,
		tagsJSON, metaJSON, now, e.ID,
	)
	if err != nil {
		wrappedErr := fmt.Errorf("sqlite: update: %w", err)
		s.recordError(span, "update", wrappedErr)
		return wrappedErr
	}

	n, err := res.RowsAffected()
	if err != nil {
		wrappedErr := fmt.Errorf("sqlite: update: rows affected: %w", err)
		s.recordError(span, "update", wrappedErr)
		return wrappedErr
	}
	if n == 0 {
		wrappedErr := fmt.Errorf("sqlite: update: id %d: %w", e.ID, lore.ErrNotFound)
		s.recordError(span, "update", wrappedErr)
		return wrappedErr
	}
	return nil
}

// ---- Get ----

// Get returns the entry with the given ID.
func (s *sqliteStore) Get(ctx context.Context, id int64) (lore.Entry, error) {
	ctx, span := s.tracer.Start(ctx, "lore.store.get",
		trace.WithAttributes(attribute.Int64("lore.id", id)),
	)
	defer span.End()

	if err := s.checkClosed(); err != nil {
		s.recordError(span, "get", err)
		return lore.Entry{}, err
	}

	row := s.db.QueryRowContext(ctx,
		`SELECT id, project, kind, title, body, source, tags, metadata, created_at, updated_at
		 FROM entries WHERE id = ?`, id)

	e, err := scanEntry(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			wrappedErr := fmt.Errorf("sqlite: get: id %d: %w", id, lore.ErrNotFound)
			s.recordError(span, "get", wrappedErr)
			return lore.Entry{}, wrappedErr
		}
		wrappedErr := fmt.Errorf("sqlite: get: %w", err)
		s.recordError(span, "get", wrappedErr)
		return lore.Entry{}, wrappedErr
	}
	return e, nil
}

// ---- DeleteBySource ----

// DeleteBySource removes all entries whose Source field exactly matches source.
func (s *sqliteStore) DeleteBySource(ctx context.Context, source string) (int, error) {
	ctx, span := s.tracer.Start(ctx, "lore.store.delete_by_source",
		trace.WithAttributes(attribute.String("lore.source", source)),
	)
	defer span.End()

	if err := s.checkClosed(); err != nil {
		s.recordError(span, "delete_by_source", err)
		return 0, err
	}
	if source == "" {
		err := fmt.Errorf("sqlite: delete_by_source: %w: source must not be empty", lore.ErrInvalidArgument)
		s.recordError(span, "delete_by_source", err)
		return 0, err
	}

	res, err := s.db.ExecContext(ctx, `DELETE FROM entries WHERE source = ?`, source)
	if err != nil {
		wrappedErr := fmt.Errorf("sqlite: delete_by_source: %w", err)
		s.recordError(span, "delete_by_source", wrappedErr)
		return 0, wrappedErr
	}

	n, err := res.RowsAffected()
	if err != nil {
		wrappedErr := fmt.Errorf("sqlite: delete_by_source: rows affected: %w", err)
		s.recordError(span, "delete_by_source", wrappedErr)
		return 0, wrappedErr
	}
	return int(n), nil
}

// ---- ListByTag ----

// ListByTag returns all entries that carry the given tag.
func (s *sqliteStore) ListByTag(ctx context.Context, tag string, opts lore.ListOpts) ([]lore.Entry, error) {
	ctx, span := s.tracer.Start(ctx, "lore.store.list_by_tag",
		trace.WithAttributes(
			attribute.String("lore.tag", tag),
			attribute.Int("lore.limit", s.resolveLimit(opts.Limit)),
		),
	)
	defer span.End()

	if err := s.checkClosed(); err != nil {
		s.recordError(span, "list_by_tag", err)
		return nil, err
	}
	if tag == "" {
		err := fmt.Errorf("sqlite: list_by_tag: %w: tag must not be empty", lore.ErrInvalidArgument)
		s.recordError(span, "list_by_tag", err)
		return nil, err
	}
	if opts.Limit < 0 {
		err := fmt.Errorf("sqlite: list_by_tag: %w: limit must not be negative", lore.ErrInvalidArgument)
		s.recordError(span, "list_by_tag", err)
		return nil, err
	}

	limit := s.resolveLimit(opts.Limit)

	// JSON contains match: the tag appears as a quoted value in the JSON array.
	// e.g. tag="adr" matches tags='["adr","architecture"]' but not tags='["sadr"]'.
	tagPattern := `%"` + strings.ReplaceAll(tag, `"`, `\"`) + `"%`

	rows, err := s.db.QueryContext(ctx,
		`SELECT id, project, kind, title, body, source, tags, metadata, created_at, updated_at
		 FROM entries
		 WHERE tags LIKE ?
		 ORDER BY created_at DESC
		 LIMIT ? OFFSET ?`,
		tagPattern, limit, opts.Offset,
	)
	if err != nil {
		wrappedErr := fmt.Errorf("sqlite: list_by_tag: query: %w", err)
		s.recordError(span, "list_by_tag", wrappedErr)
		return nil, wrappedErr
	}
	defer func() { _ = rows.Close() }()

	return scanEntries(rows)
}

// ---- ListByKind ----

// ListByKind returns all entries of the given kind.
func (s *sqliteStore) ListByKind(ctx context.Context, kind lore.Kind, opts lore.ListOpts) ([]lore.Entry, error) {
	ctx, span := s.tracer.Start(ctx, "lore.store.list_by_kind",
		trace.WithAttributes(
			attribute.String("lore.kind", string(kind)),
			attribute.Int("lore.limit", s.resolveLimit(opts.Limit)),
		),
	)
	defer span.End()

	if err := s.checkClosed(); err != nil {
		s.recordError(span, "list_by_kind", err)
		return nil, err
	}
	if err := kind.Validate(); err != nil {
		s.recordError(span, "list_by_kind", err)
		return nil, fmt.Errorf("sqlite: list_by_kind: %w", err)
	}
	if opts.Limit < 0 {
		err := fmt.Errorf("sqlite: list_by_kind: %w: limit must not be negative", lore.ErrInvalidArgument)
		s.recordError(span, "list_by_kind", err)
		return nil, err
	}

	limit := s.resolveLimit(opts.Limit)

	rows, err := s.db.QueryContext(ctx,
		`SELECT id, project, kind, title, body, source, tags, metadata, created_at, updated_at
		 FROM entries
		 WHERE kind = ?
		 ORDER BY created_at DESC
		 LIMIT ? OFFSET ?`,
		string(kind), limit, opts.Offset,
	)
	if err != nil {
		wrappedErr := fmt.Errorf("sqlite: list_by_kind: query: %w", err)
		s.recordError(span, "list_by_kind", wrappedErr)
		return nil, wrappedErr
	}
	defer func() { _ = rows.Close() }()

	return scanEntries(rows)
}

// ---- SearchText ----

// SearchText runs a BM25 full-text query against Title and Body.
func (s *sqliteStore) SearchText(ctx context.Context, query string, opts lore.SearchOpts) ([]lore.SearchHit, error) {
	ctx, span := s.tracer.Start(ctx, "lore.store.search_text",
		trace.WithAttributes(
			attribute.Int("lore.query.length", len(query)),
			attribute.Int("lore.limit", s.resolveLimit(opts.Limit)),
		),
	)
	defer span.End()

	if err := s.checkClosed(); err != nil {
		s.recordError(span, "search_text", err)
		return nil, err
	}
	if strings.TrimSpace(query) == "" {
		err := fmt.Errorf("sqlite: search_text: %w: query must not be empty", lore.ErrInvalidArgument)
		s.recordError(span, "search_text", err)
		return nil, err
	}
	if opts.Limit < 0 {
		err := fmt.Errorf("sqlite: search_text: %w: limit must not be negative", lore.ErrInvalidArgument)
		s.recordError(span, "search_text", err)
		return nil, err
	}

	limit := s.resolveLimit(opts.Limit)

	var (
		sqlBuf strings.Builder
		args   []any
	)

	sqlBuf.WriteString(`
		SELECT e.id, e.project, e.kind, e.title, e.body, e.source, e.tags, e.metadata,
		       e.created_at, e.updated_at,
		       -bm25(entries_fts) AS score
		FROM entries_fts f
		JOIN entries e ON e.id = f.rowid
		WHERE entries_fts MATCH ?`)
	args = append(args, query)

	if opts.Project != "" {
		sqlBuf.WriteString(` AND e.project = ?`)
		args = append(args, opts.Project)
	}

	if len(opts.Kinds) > 0 {
		placeholders := make([]string, len(opts.Kinds))
		for i, k := range opts.Kinds {
			placeholders[i] = "?"
			args = append(args, string(k))
		}
		sqlBuf.WriteString(` AND e.kind IN (` + strings.Join(placeholders, ",") + `)`)
	}

	sqlBuf.WriteString(` ORDER BY score DESC LIMIT ?`)
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, sqlBuf.String(), args...)
	if err != nil {
		wrappedErr := fmt.Errorf("sqlite: search_text: query: %w", err)
		s.recordError(span, "search_text", wrappedErr)
		return nil, wrappedErr
	}
	defer func() { _ = rows.Close() }()

	var hits []lore.SearchHit
	for rows.Next() {
		var (
			e     lore.Entry
			score float64
		)
		if err := scanEntryWithScore(rows, &e, &score); err != nil {
			wrappedErr := fmt.Errorf("sqlite: search_text: scan: %w", err)
			s.recordError(span, "search_text", wrappedErr)
			return nil, wrappedErr
		}
		hits = append(hits, lore.SearchHit{Entry: e, Score: score})
	}
	if err := rows.Err(); err != nil {
		wrappedErr := fmt.Errorf("sqlite: search_text: iterate: %w", err)
		s.recordError(span, "search_text", wrappedErr)
		return nil, wrappedErr
	}
	return hits, nil
}

// ---- AddEdge ----

// AddEdge persists a directed edge. Re-adding an identical triple is a no-op.
func (s *sqliteStore) AddEdge(ctx context.Context, edge lore.Edge) error {
	ctx, span := s.tracer.Start(ctx, "lore.store.add_edge",
		trace.WithAttributes(
			attribute.Int64("lore.edge.from", edge.FromID),
			attribute.Int64("lore.edge.to", edge.ToID),
		),
	)
	defer span.End()

	if err := s.checkClosed(); err != nil {
		s.recordError(span, "add_edge", err)
		return err
	}
	if edge.Relation == "" {
		err := fmt.Errorf("sqlite: add_edge: %w: relation must not be empty", lore.ErrInvalidArgument)
		s.recordError(span, "add_edge", err)
		return err
	}

	// Verify both entries exist.
	for _, id := range []int64{edge.FromID, edge.ToID} {
		var exists int
		err := s.db.QueryRowContext(ctx, `SELECT 1 FROM entries WHERE id = ?`, id).Scan(&exists)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				wrappedErr := fmt.Errorf("sqlite: add_edge: entry %d: %w", id, lore.ErrNotFound)
				s.recordError(span, "add_edge", wrappedErr)
				return wrappedErr
			}
			wrappedErr := fmt.Errorf("sqlite: add_edge: lookup entry %d: %w", id, err)
			s.recordError(span, "add_edge", wrappedErr)
			return wrappedErr
		}
	}

	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO edges (from_id, to_id, relation, weight, created_at)
		 VALUES (?, ?, ?, ?, ?)`,
		edge.FromID, edge.ToID, edge.Relation, edge.Weight, now,
	)
	if err != nil {
		wrappedErr := fmt.Errorf("sqlite: add_edge: insert: %w", err)
		s.recordError(span, "add_edge", wrappedErr)
		return wrappedErr
	}
	return nil
}

// ---- ListEdges ----

// ListEdges returns all edges whose FromID equals fromID.
func (s *sqliteStore) ListEdges(ctx context.Context, fromID int64) ([]lore.Edge, error) {
	ctx, span := s.tracer.Start(ctx, "lore.store.list_edges",
		trace.WithAttributes(attribute.Int64("lore.id", fromID)),
	)
	defer span.End()

	if err := s.checkClosed(); err != nil {
		s.recordError(span, "list_edges", err)
		return nil, err
	}
	if fromID <= 0 {
		err := fmt.Errorf("sqlite: list_edges: %w: fromID must be positive", lore.ErrInvalidArgument)
		s.recordError(span, "list_edges", err)
		return nil, err
	}

	rows, err := s.db.QueryContext(ctx,
		`SELECT from_id, to_id, relation, weight, created_at
		 FROM edges WHERE from_id = ?
		 ORDER BY created_at ASC`,
		fromID,
	)
	if err != nil {
		wrappedErr := fmt.Errorf("sqlite: list_edges: query: %w", err)
		s.recordError(span, "list_edges", wrappedErr)
		return nil, wrappedErr
	}
	defer func() { _ = rows.Close() }()

	var out []lore.Edge
	for rows.Next() {
		var (
			e         lore.Edge
			createdAt string
		)
		if err := rows.Scan(&e.FromID, &e.ToID, &e.Relation, &e.Weight, &createdAt); err != nil {
			wrappedErr := fmt.Errorf("sqlite: list_edges: scan: %w", err)
			s.recordError(span, "list_edges", wrappedErr)
			return nil, wrappedErr
		}
		e.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		wrappedErr := fmt.Errorf("sqlite: list_edges: iterate: %w", err)
		s.recordError(span, "list_edges", wrappedErr)
		return nil, wrappedErr
	}
	return out, nil
}

// ---- Close ----

// Close is idempotent and returns nil after the first call.
func (s *sqliteStore) Close(_ context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	return nil
}

// ---- scan helpers ----

// rowScanner is satisfied by both *sql.Row and *sql.Rows.
type rowScanner interface {
	Scan(dest ...any) error
}

// scanEntry reads one row of the entries SELECT column list into a lore.Entry.
func scanEntry(row rowScanner) (lore.Entry, error) {
	var (
		e         lore.Entry
		tagsJSON  string
		metaJSON  string
		createdAt string
		updatedAt string
	)
	if err := row.Scan(
		&e.ID, &e.Project, &e.Kind, &e.Title, &e.Body, &e.Source,
		&tagsJSON, &metaJSON, &createdAt, &updatedAt,
	); err != nil {
		return lore.Entry{}, err
	}

	if err := json.Unmarshal([]byte(tagsJSON), &e.Tags); err != nil {
		e.Tags = nil
	}
	if len(e.Tags) == 0 {
		e.Tags = nil
	}

	if metaJSON != "" && metaJSON != "{}" {
		var m map[string]string
		if err := json.Unmarshal([]byte(metaJSON), &m); err == nil {
			e.Metadata = m
		}
	}

	e.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	e.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
	return e, nil
}

// scanEntryWithScore reads a row that includes a trailing score column.
func scanEntryWithScore(row rowScanner, e *lore.Entry, score *float64) error {
	var (
		tagsJSON  string
		metaJSON  string
		createdAt string
		updatedAt string
	)
	if err := row.Scan(
		&e.ID, &e.Project, &e.Kind, &e.Title, &e.Body, &e.Source,
		&tagsJSON, &metaJSON, &createdAt, &updatedAt, score,
	); err != nil {
		return err
	}

	if err := json.Unmarshal([]byte(tagsJSON), &e.Tags); err != nil {
		e.Tags = nil
	}
	if len(e.Tags) == 0 {
		e.Tags = nil
	}

	if metaJSON != "" && metaJSON != "{}" {
		var m map[string]string
		if err := json.Unmarshal([]byte(metaJSON), &m); err == nil {
			e.Metadata = m
		}
	}

	e.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	e.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
	return nil
}

// scanEntries iterates *sql.Rows and returns all scanned entries.
func scanEntries(rows *sql.Rows) ([]lore.Entry, error) {
	var out []lore.Entry
	for rows.Next() {
		e, err := scanEntry(rows)
		if err != nil {
			return nil, fmt.Errorf("scan entry: %w", err)
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate entries: %w", err)
	}
	return out, nil
}

// ---- JSON marshal helpers ----

func marshalTags(tags []string) (string, error) {
	if len(tags) == 0 {
		return "[]", nil
	}
	b, err := json.Marshal(tags)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func marshalMetadata(m map[string]string) (string, error) {
	if len(m) == 0 {
		return "{}", nil
	}
	b, err := json.Marshal(m)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
