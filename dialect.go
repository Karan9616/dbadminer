package dbadminer

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// Dialect describes one database. Built-in dialects cover PostgreSQL, MySQL,
// MariaDB, and SQLite. A custom dialect must return the SQL shapes documented
// on TablesQuery and ColumnsQuery. Index, foreign-key, and DDL methods are
// left for a later version.
type Dialect interface {
	// Name is a short id such as "postgres", "mysql", "mariadb", or "sqlite".
	Name() string
	// Quote returns ident as a delimited identifier in this dialect.
	Quote(ident string) string
	// TablesQuery returns SQL with no placeholders. Each row is schema, name, type.
	TablesQuery() string
	// ColumnsQuery returns SQL for one table. Custom dialects scan
	// column_name, data_type, is_nullable ("YES"/"NO"), ordinal_position, column_default.
	// The built-in SQLite dialect uses pragma_table_info and is handled separately.
	ColumnsQuery() string
	// ColumnsArgs returns the arguments for ColumnsQuery, in placeholder order.
	ColumnsArgs(schema, table string) []any
	// Qualify returns a quoted schema.table reference.
	Qualify(schema, table string) string
	// SupportsReadOnlyTx reports whether BEGIN READ ONLY is honored.
	SupportsReadOnlyTx() bool
}

type postgresDialect struct{}

func (postgresDialect) Name() string { return "postgres" }

func (postgresDialect) Quote(ident string) string {
	return `"` + strings.ReplaceAll(ident, `"`, `""`) + `"`
}

func (d postgresDialect) Qualify(schema, table string) string {
	return d.Quote(schema) + "." + d.Quote(table)
}

func (postgresDialect) SupportsReadOnlyTx() bool { return true }

func (postgresDialect) TablesQuery() string {
	return `
SELECT n.nspname, c.relname,
       CASE c.relkind
           WHEN 'v' THEN 'VIEW'
           WHEN 'm' THEN 'VIEW'
           ELSE 'BASE TABLE'
       END
FROM pg_catalog.pg_class AS c
JOIN pg_catalog.pg_namespace AS n ON n.oid = c.relnamespace
WHERE c.relkind IN ('r', 'p', 'v', 'm', 'f')
  AND n.nspname <> 'pg_catalog'
  AND n.nspname <> 'information_schema'
  AND n.nspname NOT LIKE 'pg_toast%'
  AND n.nspname NOT LIKE 'pg_temp%'
ORDER BY n.nspname, c.relname`
}

func (postgresDialect) ColumnsQuery() string {
	return `
SELECT column_name, data_type, is_nullable, ordinal_position, column_default
FROM information_schema.columns
WHERE table_schema = $1 AND table_name = $2
ORDER BY ordinal_position`
}

func (postgresDialect) ColumnsArgs(schema, table string) []any {
	return []any{schema, table}
}

type mysqlDialect struct{}

func (mysqlDialect) Name() string { return "mysql" }

func (mysqlDialect) Quote(ident string) string {
	return "`" + strings.ReplaceAll(ident, "`", "``") + "`"
}

func (d mysqlDialect) Qualify(schema, table string) string {
	return d.Quote(schema) + "." + d.Quote(table)
}

func (mysqlDialect) SupportsReadOnlyTx() bool { return true }

func (mysqlDialect) TablesQuery() string {
	return `
SELECT table_schema, table_name, table_type
FROM information_schema.tables
WHERE table_schema NOT IN ('information_schema', 'mysql', 'performance_schema', 'sys')
  AND table_type IN ('BASE TABLE', 'VIEW')
ORDER BY table_schema, table_name`
}

func (mysqlDialect) ColumnsQuery() string {
	return `
SELECT column_name, data_type, is_nullable, ordinal_position, column_default
FROM information_schema.columns
WHERE table_schema = ? AND table_name = ?
ORDER BY ordinal_position`
}

func (mysqlDialect) ColumnsArgs(schema, table string) []any {
	return []any{schema, table}
}

type mariadbDialect struct{ mysqlDialect }

func (mariadbDialect) Name() string { return "mariadb" }

type sqliteDialect struct{}

func (sqliteDialect) Name() string { return "sqlite" }

func (sqliteDialect) Quote(ident string) string {
	return `"` + strings.ReplaceAll(ident, `"`, `""`) + `"`
}

func (d sqliteDialect) Qualify(schema, table string) string {
	return d.Quote(schema) + "." + d.Quote(table)
}

func (sqliteDialect) SupportsReadOnlyTx() bool { return false }

func (sqliteDialect) TablesQuery() string {
	return `
SELECT 'main', name, CASE type WHEN 'view' THEN 'VIEW' ELSE 'BASE TABLE' END
FROM sqlite_master
WHERE type IN ('table', 'view') AND name NOT LIKE 'sqlite_%'
UNION ALL
SELECT 'temp', name, CASE type WHEN 'view' THEN 'VIEW' ELSE 'BASE TABLE' END
FROM sqlite_temp_master
WHERE type IN ('table', 'view') AND name NOT LIKE 'sqlite_%'
ORDER BY 1, 2`
}

func (sqliteDialect) ColumnsQuery() string {
	return `SELECT cid, name, type, "notnull", dflt_value, pk FROM pragma_table_info(?, ?) ORDER BY cid`
}

func (sqliteDialect) ColumnsArgs(schema, table string) []any {
	return []any{table, schema}
}

func detectDialect(ctx context.Context, db *sql.DB) (Dialect, error) {
	var version string
	if err := db.QueryRowContext(ctx, "SELECT sqlite_version()").Scan(&version); err == nil {
		return sqliteDialect{}, nil
	}
	if err := db.QueryRowContext(ctx, "SELECT version()").Scan(&version); err == nil {
		if strings.Contains(strings.ToLower(version), "postgresql") {
			return postgresDialect{}, nil
		}
	}
	var banner string
	if err := db.QueryRowContext(ctx, "SELECT @@version").Scan(&banner); err == nil {
		if strings.Contains(strings.ToLower(banner), "mariadb") {
			return mariadbDialect{}, nil
		}
		return mysqlDialect{}, nil
	}
	return nil, errors.New("dbadminer: could not detect a PostgreSQL, MySQL, MariaDB, or SQLite database")
}

func limitClause(d Dialect) string {
	if strings.Contains(d.ColumnsQuery(), "$1") {
		return "LIMIT $1 OFFSET $2"
	}
	return "LIMIT ? OFFSET ?"
}

func primaryKeyQuery(d Dialect) (string, bool) {
	switch d.Name() {
	case "postgres":
		return `
SELECT kcu.column_name
FROM information_schema.table_constraints AS tc
JOIN information_schema.key_column_usage AS kcu
  ON tc.constraint_schema = kcu.constraint_schema
 AND tc.constraint_name = kcu.constraint_name
 AND tc.table_schema = kcu.table_schema
 AND tc.table_name = kcu.table_name
WHERE tc.constraint_type = 'PRIMARY KEY'
  AND tc.table_schema = $1
  AND tc.table_name = $2
ORDER BY kcu.ordinal_position`, true
	case "mysql", "mariadb":
		return `
SELECT kcu.column_name
FROM information_schema.table_constraints AS tc
JOIN information_schema.key_column_usage AS kcu
  ON tc.constraint_schema = kcu.constraint_schema
 AND tc.constraint_name = kcu.constraint_name
 AND tc.table_schema = kcu.table_schema
 AND tc.table_name = kcu.table_name
WHERE tc.constraint_type = 'PRIMARY KEY'
  AND tc.table_schema = ?
  AND tc.table_name = ?
ORDER BY kcu.ordinal_position`, true
	default:
		return "", false
	}
}

func existsQuery(d Dialect, schema string) (string, []any, bool) {
	switch d.Name() {
	case "postgres":
		return `
SELECT 1
FROM pg_catalog.pg_class AS c
JOIN pg_catalog.pg_namespace AS n ON n.oid = c.relnamespace
WHERE n.nspname = $1 AND c.relname = $2
  AND c.relkind IN ('r', 'p', 'v', 'm', 'f')`, nil, true
	case "mysql", "mariadb":
		return `
SELECT 1
FROM information_schema.tables
WHERE table_schema = ? AND table_name = ?
  AND table_type IN ('BASE TABLE', 'VIEW')`, nil, true
	case "sqlite":
		master := "sqlite_master"
		if schema == "temp" {
			master = "sqlite_temp_master"
		}
		return fmt.Sprintf(`SELECT 1 FROM %s WHERE name = ? AND type IN ('table', 'view') AND name NOT LIKE 'sqlite_%%'`, master), nil, true
	default:
		return "", nil, false
	}
}

func normalizeTableType(raw string) string {
	switch strings.ToUpper(strings.TrimSpace(raw)) {
	case "VIEW", "SYSTEM VIEW":
		return "view"
	default:
		return "table"
	}
}
