package dbadminer

import (
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// Exercise runs the shared handler checks. Integration tests live in the
// cmd/dbadminer module so they can open a database without a driver import here.
func Exercise(t *testing.T, db *sql.DB) {
	t.Helper()
	exercise(t, db)
}

func exercise(t *testing.T, db *sql.DB) {
	t.Helper()
	ex, err := New(db, DefaultConfig())
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	h := ex.Handler()
	id := shortID(t)
	items := "dbadminer_items_" + id
	composite := "dbadminer_comp_" + id
	nopk := "dbadminer_nopk_" + id
	view := "dbadminer_view_" + id
	weird := `dbadminer_we"ird` + id
	part := "dbadminer_part_" + id
	d := ex.dialect.Name()
	q := func(ident string) string { return quoteDDL(d, ident) }

	switch d {
	case "postgres":
		execSQL(t, db, fmt.Sprintf(`CREATE TABLE %s (id BIGINT PRIMARY KEY, name TEXT NOT NULL, qty BIGINT, big BIGINT, price DOUBLE PRECISION, blob BYTEA, meta JSONB, tags TEXT[], uid UUID, score DOUBLE PRECISION)`, q(items)))
		execSQL(t, db, fmt.Sprintf(`INSERT INTO %s (id, name, qty, big, price, blob, meta, tags, uid, score) VALUES
			(1, 'alpha', 2, 9007199254740993, 1.5, decode('deadbeef','hex'), '{"ok":true}'::jsonb, ARRAY['a','b'], '123e4567-e89b-12d3-a456-426614174000'::uuid, 'NaN'::float8),
			(2, 'beta', 3, 4, 2.5, decode('00','hex'), '{}'::jsonb, ARRAY['c']::text[], '123e4567-e89b-12d3-a456-426614174001'::uuid, 1)`, q(items)))
	case "mysql", "mariadb":
		blobCol := q("blob")
		execSQL(t, db, fmt.Sprintf(`CREATE TABLE %s (id BIGINT PRIMARY KEY, name TEXT NOT NULL, qty BIGINT, big BIGINT, price DOUBLE, %s BLOB, meta JSON)`, q(items), blobCol))
		execSQL(t, db, fmt.Sprintf(`INSERT INTO %s (id, name, qty, big, price, %s, meta) VALUES
			(1, 'alpha', 2, 9007199254740993, 1.5, x'deadbeef', '{"ok":true}'),
			(2, 'beta', 3, 4, 2.5, x'00', '{}')`, q(items), blobCol))
	default:
		execSQL(t, db, fmt.Sprintf(`CREATE TABLE %s (id INTEGER PRIMARY KEY, name TEXT NOT NULL, qty INTEGER, big INTEGER, price REAL, blob BLOB)`, q(items)))
		execSQL(t, db, fmt.Sprintf(`INSERT INTO %s (id, name, qty, big, price, blob) VALUES
			(1, 'alpha', 2, 9007199254740993, 1.5, x'deadbeef'),
			(2, 'beta', 3, 4, 2.5, x'00')`, q(items)))
	}
	execSQL(t, db, fmt.Sprintf(`CREATE TABLE %s (a INTEGER, b INTEGER, note TEXT, PRIMARY KEY (a, b))`, q(composite)))
	execSQL(t, db, fmt.Sprintf(`INSERT INTO %s (a, b, note) VALUES (1, 2, 'pair')`, q(composite)))
	execSQL(t, db, fmt.Sprintf(`CREATE TABLE %s (val TEXT)`, q(nopk)))
	execSQL(t, db, fmt.Sprintf(`INSERT INTO %s (val) VALUES ('solo')`, q(nopk)))
	execSQL(t, db, fmt.Sprintf(`CREATE VIEW %s AS SELECT id, name FROM %s`, q(view), q(items)))
	execSQL(t, db, fmt.Sprintf(`CREATE TABLE %s (id INTEGER PRIMARY KEY)`, q(weird)))
	if d == "postgres" {
		execSQL(t, db, fmt.Sprintf(`CREATE TABLE %s (id BIGINT, day DATE NOT NULL, PRIMARY KEY (id, day)) PARTITION BY RANGE (day)`, q(part)))
		execSQL(t, db, fmt.Sprintf(`CREATE TABLE %s PARTITION OF %s FOR VALUES FROM ('2024-01-01') TO ('2025-01-01')`, q(part+"_y"), q(part)))
		execSQL(t, db, fmt.Sprintf(`INSERT INTO %s (id, day) VALUES (1, '2024-06-01')`, q(part)))
	}
	t.Cleanup(func() {
		_, _ = db.Exec(fmt.Sprintf(`DROP VIEW IF EXISTS %s`, q(view)))
		if d == "postgres" {
			_, _ = db.Exec(fmt.Sprintf(`DROP TABLE IF EXISTS %s CASCADE`, q(part)))
		}
		for _, name := range []string{items, composite, nopk, weird} {
			_, _ = db.Exec(fmt.Sprintf(`DROP TABLE IF EXISTS %s`, q(name)))
		}
	})

	infoRR := doRequest(h, http.MethodGet, "/api/info", nil, "")
	if infoRR.Code != http.StatusOK {
		t.Fatalf("info %d %s", infoRR.Code, infoRR.Body.String())
	}
	if infoRR.Header().Get("Content-Security-Policy") != "default-src 'self'" ||
		infoRR.Header().Get("X-Content-Type-Options") != "nosniff" ||
		infoRR.Header().Get("Referrer-Policy") != "no-referrer" {
		t.Fatalf("headers: %#v", infoRR.Header())
	}
	var info infoPayload
	decodeJSON(t, infoRR, &info)
	if info.Name != "dbadminer" || info.Dialect != d || !info.ReadOnly || info.Access != AccessReadOnly || info.Version == "" {
		t.Fatalf("%+v", info)
	}

	page := doRequest(h, http.MethodGet, "/", nil, "")
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), "DBAdminer") || !strings.Contains(page.Body.String(), `src="./assets/`) {
		t.Fatalf("ui %d %s", page.Code, page.Body.String())
	}

	tablesRR := doRequest(h, http.MethodGet, "/api/tables", nil, "")
	if tablesRR.Code != http.StatusOK {
		t.Fatalf("tables %d %s", tablesRR.Code, tablesRR.Body.String())
	}
	var tables tablesPayload
	decodeJSON(t, tablesRR, &tables)
	schema := ""
	for _, tbl := range tables.Tables {
		if tbl.Name == items {
			schema = tbl.Schema
		}
	}
	if schema == "" {
		t.Fatalf("missing %s in %+v", items, tables.Tables)
	}
	for _, name := range []string{composite, nopk, view, weird} {
		if !hasTable(tables.Tables, name) {
			t.Fatalf("missing %s", name)
		}
	}

	cols := getColumns(t, h, schema, composite)
	if len(cols.PrimaryKey) != 2 || cols.PrimaryKey[0] != "a" || cols.PrimaryKey[1] != "b" {
		t.Fatalf("composite pk %+v", cols.PrimaryKey)
	}
	nopkCols := getColumns(t, h, schema, nopk)
	if len(nopkCols.PrimaryKey) != 0 {
		t.Fatalf("expected no pk, got %+v", nopkCols.PrimaryKey)
	}
	if _, err := getColumnsOK(h, schema, weird); err != nil {
		t.Fatal(err)
	}

	rows := getRows(t, h, schema, items, "order=name&dir=asc")
	if len(rows.Rows) != 2 || cellString(rows, 0, "name") != "alpha" || cellString(rows, 1, "name") != "beta" {
		t.Fatalf("order %+v", rows.Rows)
	}
	if cellString(rows, 0, "big") != "9007199254740993" {
		t.Fatalf("big = %#v", cell(rows, 0, "big"))
	}
	if cellString(rows, 0, "blob") != base64.StdEncoding.EncodeToString([]byte{0xde, 0xad, 0xbe, 0xef}) {
		t.Fatalf("blob = %#v", cell(rows, 0, "blob"))
	}
	if d == "postgres" || d == "mysql" || d == "mariadb" {
		meta := cell(rows, 0, "meta")
		obj, ok := meta.(map[string]any)
		if !ok || obj["ok"] != true {
			t.Fatalf("meta = %#v", meta)
		}
	}
	if d == "postgres" {
		if cell(rows, 0, "score") != nil {
			t.Fatalf("nan score = %#v", cell(rows, 0, "score"))
		}
		if cellString(rows, 0, "uid") != "123e4567-e89b-12d3-a456-426614174000" {
			t.Fatalf("uuid = %#v", cell(rows, 0, "uid"))
		}
		tags, ok := cell(rows, 0, "tags").([]any)
		if !ok || len(tags) != 2 {
			t.Fatalf("tags = %#v", cell(rows, 0, "tags"))
		}
	}
	desc := getRows(t, h, schema, items, "order=name&dir=desc&limit=1&offset=0")
	if cellString(desc, 0, "name") != "beta" {
		t.Fatalf("desc %+v", desc.Rows)
	}
	page2 := getRows(t, h, schema, items, "order=name&dir=asc&limit=1&offset=1&count=1")
	if cellString(page2, 0, "name") != "beta" || page2.Total == nil || *page2.Total != 2 {
		t.Fatalf("page %+v total %v", page2.Rows, page2.Total)
	}
	viewRows := getRows(t, h, schema, view, "")
	if len(viewRows.Rows) != 2 {
		t.Fatalf("view %+v", viewRows.Rows)
	}

	badOrder := doRequest(h, http.MethodGet, rowPath(schema, items)+"?order=not_a_column", nil, "")
	if badOrder.Code != http.StatusBadRequest {
		t.Fatalf("bad order %d %s", badOrder.Code, badOrder.Body.String())
	}
	missing := doRequest(h, http.MethodGet, rowPath(schema, "nope';drop table "+items+";--"), nil, "")
	if missing.Code != http.StatusNotFound {
		t.Fatalf("injection status %d %s", missing.Code, missing.Body.String())
	}
	if left := getRows(t, h, schema, items, "count=1"); left.Total == nil || *left.Total != 2 {
		t.Fatal("table changed after rejected identifier")
	}

	stacked := postSQL(h, "select 1; drop table "+q(items))
	if stacked.Code != http.StatusBadRequest {
		t.Fatalf("stacked %d %s", stacked.Code, stacked.Body.String())
	}
	deleted := postSQL(h, "delete from "+q(items))
	if deleted.Code != http.StatusForbidden {
		t.Fatalf("delete %d %s", deleted.Code, deleted.Body.String())
	}
	plain := doRequest(h, http.MethodPost, "/api/query", strings.NewReader(`{"sql":"select 1"}`), "text/plain")
	if plain.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("content type %d", plain.Code)
	}
	queried := postSQL(h, "select name from "+q(items)+" order by name")
	if queried.Code != http.StatusOK {
		t.Fatalf("query %d %s", queried.Code, queried.Body.String())
	}
	var queriedRows rowsPayload
	decodeJSON(t, queried, &queriedRows)
	if len(queriedRows.Rows) != 2 {
		t.Fatalf("query rows %+v", queriedRows.Rows)
	}

	evil := httptest.NewRequest(http.MethodGet, "/api/info", nil)
	evil.Host = "evil.example"
	evilRR := httptest.NewRecorder()
	h.ServeHTTP(evilRR, evil)
	if evilRR.Code != http.StatusForbidden {
		t.Fatalf("host %d", evilRR.Code)
	}
	origin := httptest.NewRequest(http.MethodGet, "/api/info", nil)
	origin.Host = "127.0.0.1:8080"
	origin.Header.Set("Origin", "http://evil.example")
	originRR := httptest.NewRecorder()
	h.ServeHTTP(originRR, origin)
	if originRR.Code != http.StatusForbidden {
		t.Fatalf("origin %d", originRR.Code)
	}

	if d == "postgres" {
		partRows := getRows(t, h, schema, part, "")
		if len(partRows.Rows) != 1 {
			t.Fatalf("partition %+v", partRows.Rows)
		}
	}
}

func shortID(t *testing.T) string {
	t.Helper()
	sum := sha256.Sum256([]byte(t.Name()))
	return hex.EncodeToString(sum[:4])
}

func quoteDDL(dialect, ident string) string {
	if dialect == "mysql" || dialect == "mariadb" {
		return "`" + strings.ReplaceAll(ident, "`", "``") + "`"
	}
	return `"` + strings.ReplaceAll(ident, `"`, `""`) + `"`
}

func execSQL(t *testing.T, db *sql.DB, query string) {
	t.Helper()
	if _, err := db.Exec(query); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
}

func doRequest(h http.Handler, method, path string, body io.Reader, contentType string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, body)
	req.Host = "127.0.0.1:8080"
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return rr
}

func decodeJSON(t *testing.T, rr *httptest.ResponseRecorder, dest any) {
	t.Helper()
	if err := json.Unmarshal(rr.Body.Bytes(), dest); err != nil {
		t.Fatalf("status %d body %s: %v", rr.Code, rr.Body.String(), err)
	}
}

func hasTable(tables []tableInfo, name string) bool {
	for _, tbl := range tables {
		if tbl.Name == name {
			return true
		}
	}
	return false
}

func rowPath(schema, table string) string {
	return "/api/tables/" + url.PathEscape(schema) + "/" + url.PathEscape(table) + "/rows"
}

func columnPath(schema, table string) string {
	return "/api/tables/" + url.PathEscape(schema) + "/" + url.PathEscape(table)
}

func getColumns(t *testing.T, h http.Handler, schema, table string) columnsPayload {
	t.Helper()
	payload, err := getColumnsOK(h, schema, table)
	if err != nil {
		t.Fatal(err)
	}
	return payload
}

func getColumnsOK(h http.Handler, schema, table string) (columnsPayload, error) {
	rr := doRequest(h, http.MethodGet, columnPath(schema, table), nil, "")
	if rr.Code != http.StatusOK {
		return columnsPayload{}, fmt.Errorf("columns %d %s", rr.Code, rr.Body.String())
	}
	var payload columnsPayload
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		return columnsPayload{}, err
	}
	return payload, nil
}

func getRows(t *testing.T, h http.Handler, schema, table, rawQuery string) rowsPayload {
	t.Helper()
	path := rowPath(schema, table)
	if rawQuery != "" {
		path += "?" + rawQuery
	}
	rr := doRequest(h, http.MethodGet, path, nil, "")
	if rr.Code != http.StatusOK {
		t.Fatalf("rows %s %d %s", path, rr.Code, rr.Body.String())
	}
	var payload rowsPayload
	decodeJSON(t, rr, &payload)
	return payload
}

func cell(payload rowsPayload, row int, name string) any {
	idx := -1
	for i, col := range payload.Columns {
		if col == name {
			idx = i
			break
		}
	}
	if idx < 0 || row >= len(payload.Rows) || idx >= len(payload.Rows[row]) {
		return nil
	}
	return payload.Rows[row][idx]
}

func cellString(payload rowsPayload, row int, name string) string {
	v, _ := cell(payload, row, name).(string)
	return v
}

func postSQL(h http.Handler, sql string) *httptest.ResponseRecorder {
	body, _ := json.Marshal(map[string]string{"sql": sql})
	return doRequest(h, http.MethodPost, "/api/query", strings.NewReader(string(body)), "application/json")
}
