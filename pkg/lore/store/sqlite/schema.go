// Package sqlite provides a SQLite-backed implementation of store.Store using
// modernc.org/sqlite, a pure-Go SQLite driver that requires no CGO.
//
// Callers open a *sql.DB themselves (using sql.Open with driver name "sqlite")
// and pass it to New. The store runs pending migrations on construction and
// owns no connection-pool configuration; the caller sets MaxOpenConns,
// MaxIdleConns, and pragmas via the DSN.
//
// Recommended DSN pragmas for every connection in the pool:
//
//	?_pragma=journal_mode(WAL)
//	&_pragma=busy_timeout(5000)
//	&_pragma=synchronous(NORMAL)
//	&_pragma=foreign_keys(ON)
//
// Pass ":memory:" as the DSN path for ephemeral test databases.
package sqlite

import "embed"

//go:embed migrations/*.up.sql
var migrationFS embed.FS
