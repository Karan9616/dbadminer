package dbadminer

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"strings"
)

var (
	safeIdent = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	safeType  = regexp.MustCompile(`(?i)^[A-Za-z][A-Za-z0-9 ]*(\([0-9]+(,[0-9]+)?\))?$`)
	numLit    = regexp.MustCompile(`^[+-]?([0-9]+(\.[0-9]*)?|\.[0-9]+)([eE][+-]?[0-9]+)?$`)
)

type indexInfo struct {
	Name    string   `json:"name"`
	Unique  bool     `json:"unique"`
	Columns []string `json:"columns"`
	Primary bool     `json:"primary,omitempty"`
}

type foreignKeyInfo struct {
	Name       string   `json:"name"`
	Columns    []string `json:"columns"`
	RefSchema  string   `json:"refSchema,omitempty"`
	RefTable   string   `json:"refTable"`
	RefColumns []string `json:"refColumns"`
	OnUpdate   string   `json:"onUpdate,omitempty"`
	OnDelete   string   `json:"onDelete,omitempty"`
}

type schemaPayload struct {
	Schema      string           `json:"schema"`
	Name        string           `json:"name"`
	Columns     []columnInfo     `json:"columns"`
	PrimaryKey  []string         `json:"primaryKey"`
	Indexes     []indexInfo      `json:"indexes"`
	ForeignKeys []foreignKeyInfo `json:"foreignKeys"`
	DDL         string           `json:"ddl,omitempty"`
}

func checkIdent(name string) error {
	if name == "" || len(name) > 63 || !safeIdent.MatchString(name) {
		return &httpError{status: 400, msg: "invalid identifier"}
	}
	return nil
}

func checkType(typ string) error {
	typ = strings.TrimSpace(typ)
	if typ == "" || len(typ) > 128 || !safeType.MatchString(typ) {
		return &httpError{status: 400, msg: "invalid column type"}
	}
	return nil
}

func checkDefault(def string) error {
	def = strings.TrimSpace(def)
	if def == "" {
		return nil
	}
	upper := strings.ToUpper(def)
	switch upper {
	case "NULL", "TRUE", "FALSE", "CURRENT_TIMESTAMP", "CURRENT_DATE", "CURRENT_TIME":
		return nil
	}
	if numLit.MatchString(def) {
		return nil
	}
	if isSQLStringLiteral(def) {
		return nil
	}
	return &httpError{status: 400, msg: "invalid column default"}
}

func isSQLStringLiteral(s string) bool {
	if len(s) < 2 || s[0] != '\'' || s[len(s)-1] != '\'' {
		return false
	}
	inner := s[1 : len(s)-1]
	for i := 0; i < len(inner); i++ {
		if inner[i] == '\'' {
			if i+1 >= len(inner) || inner[i+1] != '\'' {
				return false
			}
			i++
		}
	}
	return true
}

func usesDollarPlaceholders(d Dialect) bool {
	return strings.Contains(d.ColumnsQuery(), "$1")
}

func placeholder(d Dialect, n int) string {
	if usesDollarPlaceholders(d) {
		return fmt.Sprintf("$%d", n)
	}
	return "?"
}

func (e *Explorer) loadSchema(ctx context.Context, q queryer, schema, table string) (schemaPayload, error) {
	cols, pk, err := e.loadColumns(ctx, q, schema, table)
	if err != nil {
		return schemaPayload{}, err
	}
	if cols == nil {
		cols = []columnInfo{}
	}
	if pk == nil {
		pk = []string{}
	}
	indexes, err := e.loadIndexes(ctx, q, schema, table)
	if err != nil {
		return schemaPayload{}, err
	}
	fks, err := e.loadForeignKeys(ctx, q, schema, table)
	if err != nil {
		return schemaPayload{}, err
	}
	ddl, err := e.loadDDL(ctx, q, schema, table, cols, pk)
	if err != nil {
		return schemaPayload{}, err
	}
	return schemaPayload{
		Schema:      schema,
		Name:        table,
		Columns:     cols,
		PrimaryKey:  pk,
		Indexes:     indexes,
		ForeignKeys: fks,
		DDL:         ddl,
	}, nil
}

func (e *Explorer) loadIndexes(ctx context.Context, q queryer, schema, table string) ([]indexInfo, error) {
	switch e.dialect.Name() {
	case "sqlite":
		return e.sqliteIndexes(ctx, q, schema, table)
	case "postgres":
		return e.postgresIndexes(ctx, q, schema, table)
	case "mysql", "mariadb":
		return e.mysqlIndexes(ctx, q, schema, table)
	default:
		return []indexInfo{}, nil
	}
}

func (e *Explorer) sqliteIndexes(ctx context.Context, q queryer, schema, table string) ([]indexInfo, error) {
	rows, err := e.query(ctx, q, `SELECT name, "unique", origin FROM pragma_index_list(?)`, table)
	if err != nil {
		return nil, err
	}
	type idxRow struct {
		name   string
		unique int
		origin string
	}
	var list []idxRow
	for rows.Next() {
		var row idxRow
		if err := rows.Scan(&row.name, &row.unique, &row.origin); err != nil {
			_ = rows.Close()
			return nil, err
		}
		list = append(list, row)
	}
	err = rows.Err()
	_ = rows.Close()
	if err != nil {
		return nil, err
	}
	indexes := make([]indexInfo, 0, len(list))
	for _, row := range list {
		cols, err := e.sqliteIndexColumns(ctx, q, row.name)
		if err != nil {
			return nil, err
		}
		indexes = append(indexes, indexInfo{
			Name:    row.name,
			Unique:  row.unique != 0,
			Columns: cols,
			Primary: row.origin == "pk",
		})
	}
	return indexes, nil
}

func (e *Explorer) sqliteIndexColumns(ctx context.Context, q queryer, index string) ([]string, error) {
	rows, err := e.query(ctx, q, `SELECT name FROM pragma_index_info(?) ORDER BY seqno`, index)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var cols []string
	for rows.Next() {
		var name sql.NullString
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		if name.Valid {
			cols = append(cols, name.String)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if cols == nil {
		cols = []string{}
	}
	return cols, nil
}

func (e *Explorer) postgresIndexes(ctx context.Context, q queryer, schema, table string) ([]indexInfo, error) {
	rows, err := e.query(ctx, q, `
SELECT i.relname,
       ix.indisunique,
       ix.indisprimary,
       COALESCE(array_to_string(ARRAY(
         SELECT a.attname
         FROM unnest(ix.indkey) WITH ORDINALITY AS k(attnum, ord)
         JOIN pg_attribute a ON a.attrelid = t.oid AND a.attnum = k.attnum
         ORDER BY k.ord
       ), ','), '')
FROM pg_index ix
JOIN pg_class t ON t.oid = ix.indrelid
JOIN pg_class i ON i.oid = ix.indexrelid
JOIN pg_namespace n ON n.oid = t.relnamespace
WHERE n.nspname = $1 AND t.relname = $2
ORDER BY i.relname`, schema, table)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var indexes []indexInfo
	for rows.Next() {
		var name, cols string
		var unique, primary bool
		if err := rows.Scan(&name, &unique, &primary, &cols); err != nil {
			return nil, err
		}
		var columns []string
		if cols != "" {
			columns = strings.Split(cols, ",")
		} else {
			columns = []string{}
		}
		indexes = append(indexes, indexInfo{Name: name, Unique: unique, Columns: columns, Primary: primary})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if indexes == nil {
		indexes = []indexInfo{}
	}
	return indexes, nil
}

func (e *Explorer) mysqlIndexes(ctx context.Context, q queryer, schema, table string) ([]indexInfo, error) {
	rows, err := e.query(ctx, q, `
SELECT INDEX_NAME, NON_UNIQUE, COLUMN_NAME, SEQ_IN_INDEX
FROM information_schema.STATISTICS
WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ?
ORDER BY INDEX_NAME, SEQ_IN_INDEX`, schema, table)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	order := []string{}
	byName := map[string]*indexInfo{}
	for rows.Next() {
		var name, col string
		var nonUnique, seq int
		if err := rows.Scan(&name, &nonUnique, &col, &seq); err != nil {
			return nil, err
		}
		idx, ok := byName[name]
		if !ok {
			idx = &indexInfo{Name: name, Unique: nonUnique == 0, Columns: []string{}, Primary: name == "PRIMARY"}
			byName[name] = idx
			order = append(order, name)
		}
		idx.Columns = append(idx.Columns, col)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	indexes := make([]indexInfo, 0, len(order))
	for _, name := range order {
		indexes = append(indexes, *byName[name])
	}
	return indexes, nil
}

func (e *Explorer) loadForeignKeys(ctx context.Context, q queryer, schema, table string) ([]foreignKeyInfo, error) {
	switch e.dialect.Name() {
	case "sqlite":
		return e.sqliteForeignKeys(ctx, q, schema, table)
	case "postgres":
		return e.postgresForeignKeys(ctx, q, schema, table)
	case "mysql", "mariadb":
		return e.mysqlForeignKeys(ctx, q, schema, table)
	default:
		return []foreignKeyInfo{}, nil
	}
}

func (e *Explorer) sqliteForeignKeys(ctx context.Context, q queryer, schema, table string) ([]foreignKeyInfo, error) {
	rows, err := e.query(ctx, q, `
SELECT id, "table", "from", "to", on_update, on_delete
FROM pragma_foreign_key_list(?)
ORDER BY id, seq`, table)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	order := []int{}
	byID := map[int]*foreignKeyInfo{}
	for rows.Next() {
		var id int
		var refTable, from, to, onUpdate, onDelete string
		if err := rows.Scan(&id, &refTable, &from, &to, &onUpdate, &onDelete); err != nil {
			return nil, err
		}
		fk, ok := byID[id]
		if !ok {
			fk = &foreignKeyInfo{
				Name:       fmt.Sprintf("fk_%d", id),
				Columns:    []string{},
				RefSchema:  schema,
				RefTable:   refTable,
				RefColumns: []string{},
				OnUpdate:   onUpdate,
				OnDelete:   onDelete,
			}
			byID[id] = fk
			order = append(order, id)
		}
		fk.Columns = append(fk.Columns, from)
		fk.RefColumns = append(fk.RefColumns, to)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	fks := make([]foreignKeyInfo, 0, len(order))
	for _, id := range order {
		fks = append(fks, *byID[id])
	}
	return fks, nil
}

func (e *Explorer) postgresForeignKeys(ctx context.Context, q queryer, schema, table string) ([]foreignKeyInfo, error) {
	rows, err := e.query(ctx, q, `
SELECT tc.constraint_name,
       kcu.column_name,
       ccu.table_schema,
       ccu.table_name,
       ccu.column_name,
       rc.update_rule,
       rc.delete_rule,
       kcu.ordinal_position
FROM information_schema.table_constraints AS tc
JOIN information_schema.key_column_usage AS kcu
  ON tc.constraint_schema = kcu.constraint_schema
 AND tc.constraint_name = kcu.constraint_name
JOIN information_schema.constraint_column_usage AS ccu
  ON ccu.constraint_schema = tc.constraint_schema
 AND ccu.constraint_name = tc.constraint_name
JOIN information_schema.referential_constraints AS rc
  ON rc.constraint_schema = tc.constraint_schema
 AND rc.constraint_name = tc.constraint_name
WHERE tc.constraint_type = 'FOREIGN KEY'
  AND tc.table_schema = $1
  AND tc.table_name = $2
ORDER BY tc.constraint_name, kcu.ordinal_position`, schema, table)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	return collectForeignKeys(rows)
}

func (e *Explorer) mysqlForeignKeys(ctx context.Context, q queryer, schema, table string) ([]foreignKeyInfo, error) {
	rows, err := e.query(ctx, q, `
SELECT tc.CONSTRAINT_NAME,
       kcu.COLUMN_NAME,
       kcu.REFERENCED_TABLE_SCHEMA,
       kcu.REFERENCED_TABLE_NAME,
       kcu.REFERENCED_COLUMN_NAME,
       rc.UPDATE_RULE,
       rc.DELETE_RULE,
       kcu.ORDINAL_POSITION
FROM information_schema.TABLE_CONSTRAINTS AS tc
JOIN information_schema.KEY_COLUMN_USAGE AS kcu
  ON tc.CONSTRAINT_SCHEMA = kcu.CONSTRAINT_SCHEMA
 AND tc.CONSTRAINT_NAME = kcu.CONSTRAINT_NAME
 AND tc.TABLE_SCHEMA = kcu.TABLE_SCHEMA
 AND tc.TABLE_NAME = kcu.TABLE_NAME
JOIN information_schema.REFERENTIAL_CONSTRAINTS AS rc
  ON rc.CONSTRAINT_SCHEMA = tc.CONSTRAINT_SCHEMA
 AND rc.CONSTRAINT_NAME = tc.CONSTRAINT_NAME
WHERE tc.CONSTRAINT_TYPE = 'FOREIGN KEY'
  AND tc.TABLE_SCHEMA = ?
  AND tc.TABLE_NAME = ?
  AND kcu.REFERENCED_TABLE_NAME IS NOT NULL
ORDER BY tc.CONSTRAINT_NAME, kcu.ORDINAL_POSITION`, schema, table)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	return collectForeignKeys(rows)
}

func collectForeignKeys(rows *sql.Rows) ([]foreignKeyInfo, error) {
	order := []string{}
	byName := map[string]*foreignKeyInfo{}
	for rows.Next() {
		var name, col, refSchema, refTable, refCol, onUpdate, onDelete string
		var pos int
		if err := rows.Scan(&name, &col, &refSchema, &refTable, &refCol, &onUpdate, &onDelete, &pos); err != nil {
			return nil, err
		}
		fk, ok := byName[name]
		if !ok {
			fk = &foreignKeyInfo{
				Name:       name,
				Columns:    []string{},
				RefSchema:  refSchema,
				RefTable:   refTable,
				RefColumns: []string{},
				OnUpdate:   onUpdate,
				OnDelete:   onDelete,
			}
			byName[name] = fk
			order = append(order, name)
		}
		fk.Columns = append(fk.Columns, col)
		fk.RefColumns = append(fk.RefColumns, refCol)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	fks := make([]foreignKeyInfo, 0, len(order))
	for _, name := range order {
		fks = append(fks, *byName[name])
	}
	return fks, nil
}

func (e *Explorer) loadDDL(ctx context.Context, q queryer, schema, table string, cols []columnInfo, pk []string) (string, error) {
	switch e.dialect.Name() {
	case "sqlite":
		master := "sqlite_master"
		if schema == "temp" {
			master = "sqlite_temp_master"
		}
		var ddl sql.NullString
		err := q.QueryRowContext(ctx, fmt.Sprintf(`SELECT sql FROM %s WHERE name = ? AND type IN ('table', 'view')`, master), table).Scan(&ddl)
		e.audit(ctx, "SELECT sql FROM "+master, err)
		if err == sql.ErrNoRows {
			return "", nil
		}
		if err != nil {
			return "", err
		}
		if ddl.Valid {
			return ddl.String, nil
		}
		return "", nil
	case "mysql", "mariadb":
		rows, err := e.query(ctx, q, "SHOW CREATE TABLE "+e.dialect.Qualify(schema, table))
		if err != nil {
			return "", err
		}
		defer func() { _ = rows.Close() }()
		if !rows.Next() {
			return "", rows.Err()
		}
		var name, ddl string
		if err := rows.Scan(&name, &ddl); err != nil {
			return "", err
		}
		return ddl, rows.Err()
	default:
		return buildCreateTableSQL(e.dialect, schema, table, cols, pk), nil
	}
}
