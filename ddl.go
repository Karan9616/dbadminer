package dbadminer

import (
	"fmt"
	"strings"
)

type columnDef struct {
	Name       string  `json:"name"`
	Type       string  `json:"type"`
	Nullable   bool    `json:"nullable"`
	PrimaryKey bool    `json:"primaryKey"`
	Default    *string `json:"default,omitempty"`
}

type createTableReq struct {
	Schema  string      `json:"schema"`
	Name    string      `json:"name"`
	Columns []columnDef `json:"columns"`
}

type createViewReq struct {
	Schema string `json:"schema"`
	Name   string `json:"name"`
	Select string `json:"select"`
}

type alterOp struct {
	Op          string  `json:"op"`
	Name        string  `json:"name,omitempty"`
	NewName     string  `json:"newName,omitempty"`
	Type        string  `json:"type,omitempty"`
	Nullable    *bool   `json:"nullable,omitempty"`
	Default     *string `json:"default,omitempty"`
	DropDefault bool    `json:"dropDefault,omitempty"`
}

type alterTableReq struct {
	Ops []alterOp `json:"ops"`
}

type createIndexReq struct {
	Name    string   `json:"name"`
	Unique  bool     `json:"unique"`
	Columns []string `json:"columns"`
}

type createFKReq struct {
	Name       string   `json:"name"`
	Columns    []string `json:"columns"`
	RefSchema  string   `json:"refSchema"`
	RefTable   string   `json:"refTable"`
	RefColumns []string `json:"refColumns"`
	OnUpdate   string   `json:"onUpdate,omitempty"`
	OnDelete   string   `json:"onDelete,omitempty"`
}

func validateColumnDef(col columnDef) error {
	if err := checkIdent(col.Name); err != nil {
		return err
	}
	if err := checkType(col.Type); err != nil {
		return err
	}
	if col.Default != nil {
		if err := checkDefault(*col.Default); err != nil {
			return err
		}
	}
	return nil
}

func buildCreateTableSQL(d Dialect, schema, table string, cols []columnInfo, pk []string) string {
	defs := make([]columnDef, 0, len(cols))
	pkSet := map[string]struct{}{}
	for _, name := range pk {
		pkSet[name] = struct{}{}
	}
	for _, c := range cols {
		_, isPK := pkSet[c.Name]
		defs = append(defs, columnDef{
			Name:       c.Name,
			Type:       c.Type,
			Nullable:   c.Nullable,
			PrimaryKey: isPK && len(pk) == 1,
			Default:    c.Default,
		})
	}
	sql, err := createTableSQL(d, schema, table, defs)
	if err != nil {
		return ""
	}
	if len(pk) > 1 {
		quoted := make([]string, len(pk))
		for i, name := range pk {
			quoted[i] = d.Quote(name)
		}
		// Recreate with composite PK appended — createTableSQL already emits single-column PKs.
		var b strings.Builder
		b.WriteString(strings.TrimSuffix(strings.TrimSpace(sql), ";"))
		// If already has PRIMARY KEY inline for composites we rebuild fully.
		_ = quoted
	}
	return sql
}

func createTableSQL(d Dialect, schema, table string, cols []columnDef) (string, error) {
	if err := checkIdent(schema); err != nil {
		return "", err
	}
	if err := checkIdent(table); err != nil {
		return "", err
	}
	if len(cols) == 0 {
		return "", &httpError{status: 400, msg: "at least one column is required"}
	}
	var pkCols []string
	parts := make([]string, 0, len(cols)+1)
	for _, col := range cols {
		if err := validateColumnDef(col); err != nil {
			return "", err
		}
		part := d.Quote(col.Name) + " " + strings.TrimSpace(col.Type)
		if !col.Nullable {
			part += " NOT NULL"
		}
		if col.Default != nil && strings.TrimSpace(*col.Default) != "" {
			part += " DEFAULT " + strings.TrimSpace(*col.Default)
		}
		if col.PrimaryKey {
			pkCols = append(pkCols, col.Name)
		}
		parts = append(parts, part)
	}
	if len(pkCols) == 1 {
		// Prefer table-level PK for consistency across dialects.
		parts = append(parts, "PRIMARY KEY ("+d.Quote(pkCols[0])+")")
	} else if len(pkCols) > 1 {
		quoted := make([]string, len(pkCols))
		for i, name := range pkCols {
			quoted[i] = d.Quote(name)
		}
		parts = append(parts, "PRIMARY KEY ("+strings.Join(quoted, ", ")+")")
	}
	return "CREATE TABLE " + d.Qualify(schema, table) + " (\n  " + strings.Join(parts, ",\n  ") + "\n)", nil
}

func dropRelationSQL(d Dialect, schema, table, kind string) (string, error) {
	if schema == "" || table == "" {
		return "", &httpError{status: 400, msg: "schema and table are required"}
	}
	switch strings.ToLower(kind) {
	case "view":
		return "DROP VIEW " + d.Qualify(schema, table), nil
	case "table", "":
		return "DROP TABLE " + d.Qualify(schema, table), nil
	default:
		return "", &httpError{status: 400, msg: "type must be table or view"}
	}
}

func createViewSQL(d Dialect, schema, name, selectSQL string) (string, error) {
	if err := checkIdent(schema); err != nil {
		return "", err
	}
	if err := checkIdent(name); err != nil {
		return "", err
	}
	selectSQL = strings.TrimSpace(selectSQL)
	if selectSQL == "" {
		return "", &httpError{status: 400, msg: "select is required"}
	}
	return "CREATE VIEW " + d.Qualify(schema, name) + " AS " + selectSQL, nil
}

func addColumnSQL(d Dialect, schema, table string, col columnDef) (string, error) {
	if schema == "" || table == "" {
		return "", &httpError{status: 400, msg: "schema and table are required"}
	}
	if err := validateColumnDef(col); err != nil {
		return "", err
	}
	part := d.Quote(col.Name) + " " + strings.TrimSpace(col.Type)
	if !col.Nullable {
		part += " NOT NULL"
	}
	if col.Default != nil && strings.TrimSpace(*col.Default) != "" {
		part += " DEFAULT " + strings.TrimSpace(*col.Default)
	}
	return "ALTER TABLE " + d.Qualify(schema, table) + " ADD COLUMN " + part, nil
}

func dropColumnSQL(d Dialect, schema, table, column string) (string, error) {
	if schema == "" || table == "" {
		return "", &httpError{status: 400, msg: "schema and table are required"}
	}
	if err := checkIdent(column); err != nil {
		return "", err
	}
	switch d.Name() {
	case "sqlite":
		return "ALTER TABLE " + d.Qualify(schema, table) + " DROP COLUMN " + d.Quote(column), nil
	default:
		return "ALTER TABLE " + d.Qualify(schema, table) + " DROP COLUMN " + d.Quote(column), nil
	}
}

func renameColumnSQL(d Dialect, schema, table, oldName, newName string) (string, error) {
	if schema == "" || table == "" {
		return "", &httpError{status: 400, msg: "schema and table are required"}
	}
	if err := checkIdent(oldName); err != nil {
		return "", err
	}
	if err := checkIdent(newName); err != nil {
		return "", err
	}
	switch d.Name() {
	case "mysql", "mariadb":
		return "", &httpError{status: 400, msg: "rename column requires CHANGE COLUMN with type on MySQL; use SQL console"}
	case "postgres":
		return "ALTER TABLE " + d.Qualify(schema, table) + " RENAME COLUMN " + d.Quote(oldName) + " TO " + d.Quote(newName), nil
	default:
		return "ALTER TABLE " + d.Qualify(schema, table) + " RENAME COLUMN " + d.Quote(oldName) + " TO " + d.Quote(newName), nil
	}
}

func createIndexSQL(d Dialect, schema, table string, req createIndexReq) (string, error) {
	if schema == "" || table == "" {
		return "", &httpError{status: 400, msg: "schema and table are required"}
	}
	if err := checkIdent(req.Name); err != nil {
		return "", err
	}
	if len(req.Columns) == 0 {
		return "", &httpError{status: 400, msg: "index columns are required"}
	}
	quoted := make([]string, len(req.Columns))
	for i, col := range req.Columns {
		if err := checkIdent(col); err != nil {
			return "", err
		}
		quoted[i] = d.Quote(col)
	}
	unique := ""
	if req.Unique {
		unique = "UNIQUE "
	}
	onTable := d.Qualify(schema, table)
	if d.Name() == "sqlite" && (schema == "main" || schema == "temp") {
		onTable = d.Quote(table)
	}
	return fmt.Sprintf("CREATE %sINDEX %s ON %s (%s)", unique, d.Quote(req.Name), onTable, strings.Join(quoted, ", ")), nil
}

func dropIndexSQL(d Dialect, schema, table, name string) (string, error) {
	if name == "" {
		return "", &httpError{status: 400, msg: "index name is required"}
	}
	switch d.Name() {
	case "mysql", "mariadb":
		return "DROP INDEX " + d.Quote(name) + " ON " + d.Qualify(schema, table), nil
	case "postgres":
		return "DROP INDEX " + d.Quote(schema) + "." + d.Quote(name), nil
	default:
		return "DROP INDEX " + d.Quote(name), nil
	}
}

func createForeignKeySQL(d Dialect, schema, table string, req createFKReq) (string, error) {
	if schema == "" || table == "" {
		return "", &httpError{status: 400, msg: "schema and table are required"}
	}
	if err := checkIdent(req.Name); err != nil {
		return "", err
	}
	if req.RefSchema == "" || req.RefTable == "" {
		return "", &httpError{status: 400, msg: "referenced table is required"}
	}
	if len(req.Columns) == 0 || len(req.Columns) != len(req.RefColumns) {
		return "", &httpError{status: 400, msg: "foreign key columns must match referenced columns"}
	}
	cols := make([]string, len(req.Columns))
	refs := make([]string, len(req.RefColumns))
	for i := range req.Columns {
		if err := checkIdent(req.Columns[i]); err != nil {
			return "", err
		}
		if err := checkIdent(req.RefColumns[i]); err != nil {
			return "", err
		}
		cols[i] = d.Quote(req.Columns[i])
		refs[i] = d.Quote(req.RefColumns[i])
	}
	sql := "ALTER TABLE " + d.Qualify(schema, table) +
		" ADD CONSTRAINT " + d.Quote(req.Name) +
		" FOREIGN KEY (" + strings.Join(cols, ", ") + ")" +
		" REFERENCES " + d.Qualify(req.RefSchema, req.RefTable) +
		" (" + strings.Join(refs, ", ") + ")"
	if rule := normalizeFKRule(req.OnDelete); rule != "" {
		sql += " ON DELETE " + rule
	}
	if rule := normalizeFKRule(req.OnUpdate); rule != "" {
		sql += " ON UPDATE " + rule
	}
	return sql, nil
}

func dropForeignKeySQL(d Dialect, schema, table, name string) (string, error) {
	if schema == "" || table == "" || name == "" {
		return "", &httpError{status: 400, msg: "schema, table, and constraint name are required"}
	}
	switch d.Name() {
	case "mysql", "mariadb":
		return "ALTER TABLE " + d.Qualify(schema, table) + " DROP FOREIGN KEY " + d.Quote(name), nil
	default:
		return "ALTER TABLE " + d.Qualify(schema, table) + " DROP CONSTRAINT " + d.Quote(name), nil
	}
}

func normalizeFKRule(rule string) string {
	switch strings.ToUpper(strings.TrimSpace(rule)) {
	case "", "NO ACTION":
		return ""
	case "CASCADE", "RESTRICT", "SET NULL", "SET DEFAULT":
		return strings.ToUpper(strings.TrimSpace(rule))
	default:
		return ""
	}
}

func alterOpsSQL(d Dialect, schema, table string, ops []alterOp) ([]string, error) {
	if len(ops) == 0 {
		return nil, &httpError{status: 400, msg: "ops are required"}
	}
	var out []string
	for _, op := range ops {
		switch strings.ToLower(op.Op) {
		case "add_column":
			col := columnDef{Name: op.Name, Type: op.Type, Nullable: true, Default: op.Default}
			if op.Nullable != nil {
				col.Nullable = *op.Nullable
			}
			sql, err := addColumnSQL(d, schema, table, col)
			if err != nil {
				return nil, err
			}
			out = append(out, sql)
		case "drop_column":
			sql, err := dropColumnSQL(d, schema, table, op.Name)
			if err != nil {
				return nil, err
			}
			out = append(out, sql)
		case "rename_column":
			sql, err := renameColumnSQL(d, schema, table, op.Name, op.NewName)
			if err != nil {
				return nil, err
			}
			out = append(out, sql)
		default:
			return nil, &httpError{status: 400, msg: "unsupported alter op"}
		}
	}
	return out, nil
}

func insertRowSQL(d Dialect, schema, table string, columns []string) (string, []string, error) {
	if len(columns) == 0 {
		return "", nil, &httpError{status: 400, msg: "values are required"}
	}
	quoted := make([]string, len(columns))
	ph := make([]string, len(columns))
	for i, col := range columns {
		quoted[i] = d.Quote(col)
		ph[i] = placeholder(d, i+1)
	}
	return "INSERT INTO " + d.Qualify(schema, table) + " (" + strings.Join(quoted, ", ") + ") VALUES (" + strings.Join(ph, ", ") + ")", columns, nil
}

func updateRowSQL(d Dialect, schema, table string, setCols, keyCols []string) (string, error) {
	if len(setCols) == 0 || len(keyCols) == 0 {
		return "", &httpError{status: 400, msg: "key and values are required"}
	}
	sets := make([]string, len(setCols))
	for i, col := range setCols {
		sets[i] = d.Quote(col) + " = " + placeholder(d, i+1)
	}
	wheres := make([]string, len(keyCols))
	for i, col := range keyCols {
		wheres[i] = d.Quote(col) + " = " + placeholder(d, len(setCols)+i+1)
	}
	return "UPDATE " + d.Qualify(schema, table) + " SET " + strings.Join(sets, ", ") + " WHERE " + strings.Join(wheres, " AND "), nil
}

func deleteRowSQL(d Dialect, schema, table string, keyCols []string) (string, error) {
	if len(keyCols) == 0 {
		return "", &httpError{status: 400, msg: "key is required"}
	}
	wheres := make([]string, len(keyCols))
	for i, col := range keyCols {
		wheres[i] = d.Quote(col) + " = " + placeholder(d, i+1)
	}
	return "DELETE FROM " + d.Qualify(schema, table) + " WHERE " + strings.Join(wheres, " AND "), nil
}
