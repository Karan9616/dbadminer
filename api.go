package dbadminer

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"strconv"
	"strings"

	"github.com/dbadminer/dbadminer/internal/guard"
	"github.com/dbadminer/dbadminer/internal/normalize"
)

var (
	errNotFound = errors.New("dbadminer: table not found")
	errTooLarge = errors.New("dbadminer: response too large")
)

type httpError struct {
	status int
	msg    string
}

func (e *httpError) Error() string { return e.msg }

type queryer interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

type tableInfo struct {
	Schema string `json:"schema"`
	Name   string `json:"name"`
	Type   string `json:"type"`
}

type columnInfo struct {
	Name       string  `json:"name"`
	Type       string  `json:"type"`
	Nullable   bool    `json:"nullable"`
	PrimaryKey bool    `json:"primaryKey"`
	Position   int     `json:"position"`
	Default    *string `json:"default,omitempty"`
}

type infoPayload struct {
	Name     string `json:"name"`
	Version  string `json:"version"`
	Dialect  string `json:"dialect"`
	Access   string `json:"access"`
	ReadOnly bool   `json:"readOnly"`
}

type tablesPayload struct {
	Tables []tableInfo `json:"tables"`
}

type columnsPayload struct {
	Schema     string       `json:"schema"`
	Name       string       `json:"name"`
	Columns    []columnInfo `json:"columns"`
	PrimaryKey []string     `json:"primaryKey"`
}

type rowsPayload struct {
	Columns      []string `json:"columns"`
	Rows         [][]any  `json:"rows"`
	Limit        int      `json:"limit,omitempty"`
	Offset       int      `json:"offset,omitempty"`
	Total        *int64   `json:"total,omitempty"`
	Truncated    bool     `json:"truncated,omitempty"`
	RowsAffected *int64   `json:"rowsAffected,omitempty"`
}

type apiError struct {
	Error string `json:"error"`
}

type resultSet struct {
	columns   []string
	rows      [][]any
	truncated bool
}

// Handler returns the explorer UI and JSON API. The handler rejects requests
// whose Host header is not loopback.
func (e *Explorer) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/info", e.handleInfo)
	mux.HandleFunc("GET /api/tables", e.handleTables)
	mux.HandleFunc("GET /api/tables/{schema}/{table}/rows", e.handleRows)
	mux.HandleFunc("GET /api/tables/{schema}/{table}", e.handleColumns)
	mux.HandleFunc("POST /api/query", e.handleQuery)
	if sub, err := fs.Sub(uiFiles, "ui"); err != nil {
		mux.Handle("GET /", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "UI unavailable", http.StatusInternalServerError)
		}))
	} else {
		mux.Handle("GET /", http.FileServerFS(sub))
	}
	var handler http.Handler = mux
	handler = e.auth.Wrap(handler)
	handler = e.protect(handler)
	return handler
}

func (e *Explorer) protect(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", "default-src 'self'")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		if err := guard.CheckRequestHost(r.Host, r.Header.Get("Origin")); err != nil {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (e *Explorer) handleInfo(w http.ResponseWriter, r *http.Request) {
	e.writeJSON(w, http.StatusOK, infoPayload{
		Name:     "dbadminer",
		Version:  Version,
		Dialect:  e.dialect.Name(),
		Access:   e.cfg.Access,
		ReadOnly: e.cfg.ReadOnly,
	})
}

func (e *Explorer) handleTables(w http.ResponseWriter, r *http.Request) {
	var tables []tableInfo
	err := e.withQueryer(r.Context(), func(ctx context.Context, q queryer) error {
		var err error
		tables, err = e.listTables(ctx, q)
		return err
	})
	if err != nil {
		e.failErr(w, err)
		return
	}
	if tables == nil {
		tables = []tableInfo{}
	}
	e.writeJSON(w, http.StatusOK, tablesPayload{Tables: tables})
}

func (e *Explorer) handleColumns(w http.ResponseWriter, r *http.Request) {
	schema := r.PathValue("schema")
	table := r.PathValue("table")
	var payload columnsPayload
	err := e.withQueryer(r.Context(), func(ctx context.Context, q queryer) error {
		if err := e.ensureTable(ctx, q, schema, table); err != nil {
			return err
		}
		cols, pk, err := e.loadColumns(ctx, q, schema, table)
		if err != nil {
			return err
		}
		if cols == nil {
			cols = []columnInfo{}
		}
		if pk == nil {
			pk = []string{}
		}
		payload = columnsPayload{Schema: schema, Name: table, Columns: cols, PrimaryKey: pk}
		return nil
	})
	if err != nil {
		e.failErr(w, err)
		return
	}
	e.writeJSON(w, http.StatusOK, payload)
}

func (e *Explorer) handleRows(w http.ResponseWriter, r *http.Request) {
	schema := r.PathValue("schema")
	table := r.PathValue("table")
	limit, offset, order, desc, wantCount, herr := parseRowsQuery(r, e.cfg.MaxRows)
	if herr != nil {
		e.fail(w, herr.status, herr.msg)
		return
	}
	var payload rowsPayload
	err := e.withQueryer(r.Context(), func(ctx context.Context, q queryer) error {
		if err := e.ensureTable(ctx, q, schema, table); err != nil {
			return err
		}
		cols, _, err := e.loadColumns(ctx, q, schema, table)
		if err != nil {
			return err
		}
		if order != "" && !hasColumn(cols, order) {
			return &httpError{status: http.StatusBadRequest, msg: "unknown order column"}
		}
		set, err := e.selectRows(ctx, q, schema, table, order, desc, limit, offset, jsonColumnTypes(cols))
		if err != nil {
			return err
		}
		payload = rowsPayload{
			Columns:   set.columns,
			Rows:      set.rows,
			Limit:     limit,
			Offset:    offset,
			Truncated: set.truncated,
		}
		if payload.Columns == nil {
			payload.Columns = []string{}
		}
		if payload.Rows == nil {
			payload.Rows = [][]any{}
		}
		if wantCount {
			total, err := e.countRows(ctx, q, schema, table)
			if err != nil {
				return err
			}
			payload.Total = &total
		}
		return nil
	})
	if err != nil {
		e.failErr(w, err)
		return
	}
	e.writeJSON(w, http.StatusOK, payload)
}

func (e *Explorer) handleQuery(w http.ResponseWriter, r *http.Request) {
	if !jsonContentType(r) {
		e.fail(w, http.StatusUnsupportedMediaType, "Content-Type must be application/json")
		return
	}
	body := http.MaxBytesReader(w, r.Body, e.cfg.MaxBodyBytes)
	defer func() { _ = r.Body.Close() }()
	dec := json.NewDecoder(body)
	var req struct {
		SQL string `json:"sql"`
	}
	if err := dec.Decode(&req); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) || strings.Contains(err.Error(), "request body too large") {
			e.fail(w, http.StatusRequestEntityTooLarge, "request body too large")
			return
		}
		e.fail(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if e.cfg.ReadOnly {
		ok, err := guard.ReadOnlySQL(req.SQL)
		if !ok {
			status := http.StatusForbidden
			if errors.Is(err, guard.ErrEmpty) || errors.Is(err, guard.ErrMulti) || strings.Contains(err.Error(), "unterminated") || strings.Contains(err.Error(), "too long") || strings.Contains(err.Error(), "NUL") {
				status = http.StatusBadRequest
			}
			msg := "statement is not read-only"
			if err != nil {
				msg = err.Error()
			}
			e.fail(w, status, msg)
			return
		}
	} else if err := guard.Validate(req.SQL); err != nil {
		e.fail(w, http.StatusBadRequest, err.Error())
		return
	}

	var payload rowsPayload
	err := e.withQueryer(r.Context(), func(ctx context.Context, q queryer) error {
		if guard.ReturnsRows(req.SQL) {
			set, err := e.queryRows(ctx, q, req.SQL, nil, e.cfg.MaxRows, nil)
			if err != nil {
				return err
			}
			payload = rowsPayload{Columns: set.columns, Rows: set.rows, Truncated: set.truncated}
			return nil
		}
		res, err := q.ExecContext(ctx, req.SQL)
		e.audit(ctx, req.SQL, err)
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		if err != nil {
			n = 0
		}
		payload = rowsPayload{Columns: []string{}, Rows: [][]any{}, RowsAffected: &n}
		return nil
	})
	if err != nil {
		e.failErr(w, err)
		return
	}
	if payload.Columns == nil {
		payload.Columns = []string{}
	}
	if payload.Rows == nil {
		payload.Rows = [][]any{}
	}
	e.writeJSON(w, http.StatusOK, payload)
}

func jsonContentType(r *http.Request) bool {
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil {
		return false
	}
	return strings.EqualFold(media, "application/json")
}

func parseRowsQuery(r *http.Request, maxRows int) (limit, offset int, order string, desc, count bool, err *httpError) {
	q := r.URL.Query()
	limit = defaultPageLimit
	if limit > maxRows {
		limit = maxRows
	}
	if raw := q.Get("limit"); raw != "" {
		n, convErr := strconv.Atoi(raw)
		if convErr != nil || n <= 0 {
			return 0, 0, "", false, false, &httpError{http.StatusBadRequest, "invalid limit"}
		}
		if n > maxRows {
			n = maxRows
		}
		limit = n
	}
	if raw := q.Get("offset"); raw != "" {
		n, convErr := strconv.Atoi(raw)
		if convErr != nil || n < 0 || n > maxOffset {
			return 0, 0, "", false, false, &httpError{http.StatusBadRequest, "invalid offset"}
		}
		offset = n
	}
	order = q.Get("order")
	if len(order) > 256 || strings.Contains(order, "\x00") {
		return 0, 0, "", false, false, &httpError{http.StatusBadRequest, "invalid order"}
	}
	switch strings.ToLower(q.Get("dir")) {
	case "", "asc":
	case "desc":
		desc = true
	default:
		return 0, 0, "", false, false, &httpError{http.StatusBadRequest, "invalid dir"}
	}
	switch strings.ToLower(q.Get("count")) {
	case "", "0", "false":
	case "1", "true":
		count = true
	default:
		return 0, 0, "", false, false, &httpError{http.StatusBadRequest, "invalid count"}
	}
	return limit, offset, order, desc, count, nil
}

func (e *Explorer) withQueryer(ctx context.Context, fn func(context.Context, queryer) error) error {
	ctx, cancel := context.WithTimeout(ctx, e.cfg.QueryTimeout)
	defer cancel()
	if e.cfg.ReadOnly && e.dialect.SupportsReadOnlyTx() {
		tx, err := e.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
		if err != nil {
			return err
		}
		defer func() { _ = tx.Rollback() }()
		return fn(ctx, tx)
	}
	return fn(ctx, e.db)
}

func (e *Explorer) listTables(ctx context.Context, q queryer) ([]tableInfo, error) {
	rows, err := e.query(ctx, q, e.dialect.TablesQuery())
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var tables []tableInfo
	for rows.Next() {
		var schema, name, kind string
		if err := rows.Scan(&schema, &name, &kind); err != nil {
			return nil, err
		}
		tables = append(tables, tableInfo{Schema: schema, Name: name, Type: normalizeTableType(kind)})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if tables == nil {
		tables = []tableInfo{}
	}
	return tables, nil
}

func (e *Explorer) ensureTable(ctx context.Context, q queryer, schema, table string) error {
	if schema == "" || table == "" || strings.Contains(schema, "\x00") || strings.Contains(table, "\x00") {
		return errNotFound
	}
	query, _, ok := existsQuery(e.dialect, schema)
	if !ok {
		tables, err := e.listTables(ctx, q)
		if err != nil {
			return err
		}
		for _, tbl := range tables {
			if tbl.Schema == schema && tbl.Name == table {
				return nil
			}
		}
		return errNotFound
	}
	args := []any{schema, table}
	if e.dialect.Name() == "sqlite" {
		if schema != "main" && schema != "temp" {
			return errNotFound
		}
		args = []any{table}
	}
	var one int
	err := q.QueryRowContext(ctx, query, args...).Scan(&one)
	e.audit(ctx, query, err)
	if errors.Is(err, sql.ErrNoRows) {
		return errNotFound
	}
	if err != nil {
		return err
	}
	return nil
}

func (e *Explorer) loadColumns(ctx context.Context, q queryer, schema, table string) ([]columnInfo, []string, error) {
	if e.dialect.Name() == "sqlite" {
		return e.sqliteColumns(ctx, q, schema, table)
	}
	rows, err := e.query(ctx, q, e.dialect.ColumnsQuery(), e.dialect.ColumnsArgs(schema, table)...)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = rows.Close() }()
	var cols []columnInfo
	for rows.Next() {
		var name, dataType, nullable string
		var position int
		var def sql.NullString
		if err := rows.Scan(&name, &dataType, &nullable, &position, &def); err != nil {
			return nil, nil, err
		}
		col := columnInfo{
			Name:     name,
			Type:     dataType,
			Nullable: strings.EqualFold(nullable, "YES"),
			Position: position,
		}
		if def.Valid {
			value := def.String
			col.Default = &value
		}
		cols = append(cols, col)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	pkQuery, ok := primaryKeyQuery(e.dialect)
	var pk []string
	if ok {
		pkRows, err := e.query(ctx, q, pkQuery, schema, table)
		if err != nil {
			return nil, nil, err
		}
		defer func() { _ = pkRows.Close() }()
		for pkRows.Next() {
			var name string
			if err := pkRows.Scan(&name); err != nil {
				return nil, nil, err
			}
			pk = append(pk, name)
		}
		if err := pkRows.Err(); err != nil {
			return nil, nil, err
		}
	}
	if pk == nil {
		pk = []string{}
	}
	markPrimaryKey(cols, pk)
	if err := e.markMariaDBJSON(ctx, q, schema, table, cols); err != nil {
		return nil, nil, err
	}
	return cols, pk, nil
}

func (e *Explorer) markMariaDBJSON(ctx context.Context, q queryer, schema, table string, cols []columnInfo) error {
	if e.dialect.Name() != "mariadb" {
		return nil
	}
	rows, err := e.query(ctx, q, `
SELECT cc.CHECK_CLAUSE
FROM information_schema.TABLE_CONSTRAINTS tc
JOIN information_schema.CHECK_CONSTRAINTS cc
  ON cc.CONSTRAINT_SCHEMA = tc.CONSTRAINT_SCHEMA
 AND cc.CONSTRAINT_NAME = tc.CONSTRAINT_NAME
WHERE tc.CONSTRAINT_SCHEMA = ? AND tc.TABLE_NAME = ? AND tc.CONSTRAINT_TYPE = 'CHECK'`, schema, table)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	jsonCols := map[string]struct{}{}
	for rows.Next() {
		var clause string
		if err := rows.Scan(&clause); err != nil {
			return err
		}
		for _, name := range jsonValidColumns(clause) {
			jsonCols[name] = struct{}{}
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for i := range cols {
		if _, ok := jsonCols[cols[i].Name]; ok {
			cols[i].Type = "json"
		}
	}
	return nil
}

func jsonValidColumns(clause string) []string {
	var names []string
	rest := clause
	for {
		idx := strings.Index(strings.ToLower(rest), "json_valid")
		if idx < 0 {
			return names
		}
		after := strings.TrimLeft(rest[idx+len("json_valid"):], " \t\r\n")
		rest = rest[idx+len("json_valid"):]
		if !strings.HasPrefix(after, "(") {
			continue
		}
		after = strings.TrimLeft(after[1:], " \t\r\n")
		after = strings.TrimPrefix(after, "`")
		end := strings.IndexAny(after, "` )`,\t")
		if end < 0 {
			end = len(after)
		}
		if end > 0 {
			names = append(names, after[:end])
		}
	}
}

func (e *Explorer) sqliteColumns(ctx context.Context, q queryer, schema, table string) ([]columnInfo, []string, error) {
	rows, err := e.query(ctx, q, e.dialect.ColumnsQuery(), e.dialect.ColumnsArgs(schema, table)...)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = rows.Close() }()
	var cols []columnInfo
	var pks []pkCol
	for rows.Next() {
		var cid int
		var name, colType string
		var notNull, pk int
		var def any
		if err := rows.Scan(&cid, &name, &colType, &notNull, &def, &pk); err != nil {
			return nil, nil, err
		}
		col := columnInfo{
			Name:       name,
			Type:       colType,
			Nullable:   notNull == 0,
			PrimaryKey: pk > 0,
			Position:   cid + 1,
		}
		if def != nil {
			text := fmt.Sprint(def)
			col.Default = &text
		}
		cols = append(cols, col)
		if pk > 0 {
			pks = append(pks, pkCol{name: name, ord: pk})
		}
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	if len(pks) == 0 {
		pks = e.sqliteAutoPK(ctx, q, table)
	}
	pk := make([]string, len(pks))
	for i := range pks {
		for j := i; j > 0 && pks[j].ord < pks[j-1].ord; j-- {
			pks[j], pks[j-1] = pks[j-1], pks[j]
		}
	}
	for i, col := range pks {
		pk[i] = col.name
	}
	markPrimaryKey(cols, pk)
	return cols, pk, nil
}

type pkCol struct {
	name string
	ord  int
}

func (e *Explorer) sqliteAutoPK(ctx context.Context, q queryer, table string) []pkCol {
	rows, err := e.query(ctx, q, `SELECT name FROM pragma_index_list(?)`, table)
	if err != nil {
		return nil
	}
	defer func() { _ = rows.Close() }()
	var indexes []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil
		}
		if strings.HasPrefix(name, "sqlite_autoindex_") {
			indexes = append(indexes, name)
		}
	}
	if err := rows.Err(); err != nil {
		return nil
	}
	var pks []pkCol
	for _, index := range indexes {
		info, err := e.query(ctx, q, `SELECT name, seqno FROM pragma_index_info(?) ORDER BY seqno`, index)
		if err != nil {
			return pks
		}
		for info.Next() {
			var name string
			var seq int
			if err := info.Scan(&name, &seq); err != nil {
				_ = info.Close()
				return pks
			}
			pks = append(pks, pkCol{name: name, ord: seq + 1})
		}
		err = info.Err()
		_ = info.Close()
		if err != nil {
			return pks
		}
	}
	return pks
}

func markPrimaryKey(cols []columnInfo, pk []string) {
	set := map[string]struct{}{}
	for _, name := range pk {
		set[name] = struct{}{}
	}
	for i := range cols {
		if _, ok := set[cols[i].Name]; ok {
			cols[i].PrimaryKey = true
		}
	}
}

func hasColumn(cols []columnInfo, name string) bool {
	for _, col := range cols {
		if col.Name == name {
			return true
		}
	}
	return false
}

func jsonColumnTypes(cols []columnInfo) map[string]string {
	var types map[string]string
	for _, col := range cols {
		if !normalize.IsJSON(col.Type) {
			continue
		}
		if types == nil {
			types = map[string]string{}
		}
		types[col.Name] = col.Type
	}
	return types
}

func (e *Explorer) selectRows(ctx context.Context, q queryer, schema, table, order string, desc bool, limit, offset int, colTypes map[string]string) (resultSet, error) {
	var b strings.Builder
	b.WriteString("SELECT * FROM ")
	b.WriteString(e.dialect.Qualify(schema, table))
	if order != "" {
		b.WriteString(" ORDER BY ")
		b.WriteString(e.dialect.Quote(order))
		if desc {
			b.WriteString(" DESC")
		} else {
			b.WriteString(" ASC")
		}
	}
	b.WriteByte(' ')
	b.WriteString(limitClause(e.dialect))
	return e.queryRows(ctx, q, b.String(), []any{limit, offset}, limit, colTypes)
}

func (e *Explorer) countRows(ctx context.Context, q queryer, schema, table string) (int64, error) {
	query := "SELECT COUNT(*) FROM " + e.dialect.Qualify(schema, table)
	var n int64
	err := q.QueryRowContext(ctx, query).Scan(&n)
	e.audit(ctx, query, err)
	return n, err
}

func (e *Explorer) queryRows(ctx context.Context, q queryer, query string, args []any, maxRows int, colTypes map[string]string) (resultSet, error) {
	rows, err := e.query(ctx, q, query, args...)
	if err != nil {
		return resultSet{}, err
	}
	defer func() { _ = rows.Close() }()
	return scanRows(rows, maxRows, e.cfg.MaxResponseBytes, colTypes)
}

func (e *Explorer) query(ctx context.Context, q queryer, query string, args ...any) (*sql.Rows, error) {
	rows, err := q.QueryContext(ctx, query, args...)
	e.audit(ctx, query, err)
	return rows, err
}

func (e *Explorer) audit(ctx context.Context, query string, err error) {
	if e.cfg.Audit != nil {
		e.cfg.Audit(ctx, query, err)
	}
}

func scanRows(rows *sql.Rows, maxRows int, maxBytes int64, colTypes map[string]string) (resultSet, error) {
	cols, err := rows.Columns()
	if err != nil {
		return resultSet{}, err
	}
	if len(cols) > maxColumns {
		return resultSet{}, errors.New("dbadminer: too many columns")
	}
	types, err := rows.ColumnTypes()
	if err != nil {
		return resultSet{}, err
	}
	set := resultSet{columns: cols, rows: [][]any{}}
	var used int64
	for rows.Next() {
		if len(set.rows) >= maxRows {
			set.truncated = true
			break
		}
		raw := make([]any, len(cols))
		dest := make([]any, len(cols))
		for i := range raw {
			dest[i] = &raw[i]
		}
		if err := rows.Scan(dest...); err != nil {
			return resultSet{}, err
		}
		row := make([]any, len(cols))
		for i := range raw {
			typeName := ""
			if i < len(types) && types[i] != nil {
				typeName = types[i].DatabaseTypeName()
			}
			if colTypes != nil {
				if over, ok := colTypes[cols[i]]; ok && !normalize.IsJSON(typeName) {
					typeName = over
				}
			}
			row[i] = normalize.Value(raw[i], typeName)
			used += normalize.Estimate(row[i])
		}
		if maxBytes > 0 && used > maxBytes {
			return resultSet{}, errTooLarge
		}
		set.rows = append(set.rows, row)
	}
	if err := rows.Err(); err != nil {
		return resultSet{}, err
	}
	return set, nil
}

func (e *Explorer) writeJSON(w http.ResponseWriter, status int, v any) {
	var buf strings.Builder
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(true)
	if err := enc.Encode(v); err != nil {
		http.Error(w, "encode error", http.StatusInternalServerError)
		return
	}
	payload := buf.String()
	if status < 400 && int64(len(payload)) > e.cfg.MaxResponseBytes {
		e.fail(w, http.StatusRequestEntityTooLarge, "response too large")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if _, err := io.WriteString(w, payload); err != nil {
		return
	}
}

func (e *Explorer) fail(w http.ResponseWriter, status int, msg string) {
	e.writeJSON(w, status, apiError{Error: msg})
}

func (e *Explorer) failErr(w http.ResponseWriter, err error) {
	var he *httpError
	switch {
	case errors.As(err, &he):
		e.fail(w, he.status, he.msg)
	case errors.Is(err, errNotFound):
		e.fail(w, http.StatusNotFound, "table not found")
	case errors.Is(err, errTooLarge):
		e.fail(w, http.StatusRequestEntityTooLarge, "response too large")
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		e.fail(w, http.StatusGatewayTimeout, "query timed out")
	default:
		e.fail(w, http.StatusInternalServerError, err.Error())
	}
}
