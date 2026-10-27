package dbadminer

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func openMem(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	return db
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

func TestSQLiteVersion(t *testing.T) {
	db := openMem(t)
	var v string
	if err := db.QueryRow("select sqlite_version()").Scan(&v); err != nil {
		t.Fatal(err)
	}
	t.Logf("sqlite %s", v)
	var major, minor int
	if _, err := fmt.Sscanf(v, "%d.%d", &major, &minor); err != nil {
		t.Fatal(err)
	}
	if major < 3 || (major == 3 && minor < 35) {
		t.Fatalf("sqlite %s is older than 3.35", v)
	}
}

func TestJSONValidColumns(t *testing.T) {
	got := jsonValidColumns("json_valid(`meta`)")
	if len(got) != 1 || got[0] != "meta" {
		t.Fatalf("backtick = %#v", got)
	}
	got = jsonValidColumns("json_valid(meta)")
	if len(got) != 1 || got[0] != "meta" {
		t.Fatalf("bare = %#v", got)
	}
}

func TestQuote(t *testing.T) {
	if got := (postgresDialect{}).Quote(`a"b`); got != `"a""b"` {
		t.Fatal(got)
	}
	if got := (mysqlDialect{}).Quote("a`b"); got != "`a``b`" {
		t.Fatal(got)
	}
	if got := (postgresDialect{}).Qualify("public", "t"); got != `"public"."t"` {
		t.Fatal(got)
	}
	if (mariadbDialect{}).Name() != "mariadb" || !(mariadbDialect{}).SupportsReadOnlyTx() {
		t.Fatal("mariadb dialect")
	}
	if (sqliteDialect{}).SupportsReadOnlyTx() {
		t.Fatal("sqlite should not use a read-only transaction")
	}
}

func TestHandlers(t *testing.T) {
	db := openMem(t)
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
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), "DBAdminer") {
		t.Fatalf("ui %d %s", page.Code, page.Body.String())
	}
	script := doRequest(h, http.MethodGet, "/app.js", nil, "")
	if script.Code != http.StatusOK || !strings.Contains(script.Body.String(), "textContent") {
		t.Fatalf("app.js %d", script.Code)
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

func TestZeroConfigIsReadOnly(t *testing.T) {
	db := openMem(t)
	execSQL(t, db, `CREATE TABLE t (id INTEGER)`)
	execSQL(t, db, `INSERT INTO t VALUES (1)`)
	ex, err := New(db, Config{})
	if err != nil {
		t.Fatal(err)
	}
	if !ex.cfg.ReadOnly || ex.cfg.Access != AccessReadOnly {
		t.Fatal("zero config should be read-only")
	}
	rr := postSQL(ex.Handler(), "delete from t")
	if rr.Code != http.StatusForbidden {
		t.Fatalf("%d %s", rr.Code, rr.Body.String())
	}
	var n int
	if err := db.QueryRow(`select count(*) from t`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("count %d %v", n, err)
	}
}

func TestWritesWhenDisabled(t *testing.T) {
	db := openMem(t)
	execSQL(t, db, `CREATE TABLE t (id INTEGER)`)
	execSQL(t, db, `INSERT INTO t VALUES (1)`)
	cfg := DefaultConfig()
	cfg.ReadOnly = false
	ex, err := New(db, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if ex.cfg.ReadOnly || ex.cfg.Access != AccessFull {
		t.Fatalf("access %+v", ex.cfg.Access)
	}
	rr := postSQL(ex.Handler(), "delete from t")
	if rr.Code != http.StatusOK {
		t.Fatalf("%d %s", rr.Code, rr.Body.String())
	}
	var n int
	if err := db.QueryRow(`select count(*) from t`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("count %d %v", n, err)
	}
	stacked := postSQL(ex.Handler(), "select 1; select 2")
	if stacked.Code != http.StatusBadRequest {
		t.Fatalf("stacked %d %s", stacked.Code, stacked.Body.String())
	}
}

func TestAuthAndLimits(t *testing.T) {
	db := openMem(t)
	cfg := DefaultConfig()
	cfg.User = "ada"
	cfg.Password = "secret"
	cfg.MaxBodyBytes = 8
	cfg.MaxResponseBytes = 32
	ex, err := New(db, cfg)
	if err != nil {
		t.Fatal(err)
	}
	ex.auth.SetFailLimit(2)
	h := ex.Handler()

	unauth := doRequest(h, http.MethodGet, "/api/info", nil, "")
	if unauth.Code != http.StatusUnauthorized || unauth.Header().Get("WWW-Authenticate") == "" {
		t.Fatalf("unauth %d", unauth.Code)
	}
	bad := doRequest(h, http.MethodGet, "/api/info", nil, "")
	bad.Result()
	req := httptest.NewRequest(http.MethodGet, "/api/info", nil)
	req.Host = "127.0.0.1:8080"
	req.SetBasicAuth("ada", "nope")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized && rr.Code != http.StatusTooManyRequests {
		t.Fatalf("bad password %d", rr.Code)
	}
	req2 := httptest.NewRequest(http.MethodGet, "/api/info", nil)
	req2.Host = "127.0.0.1:8080"
	req2.SetBasicAuth("ada", "nope")
	rr2 := httptest.NewRecorder()
	h.ServeHTTP(rr2, req2)
	if rr2.Code != http.StatusTooManyRequests {
		t.Fatalf("rate limit %d %s", rr2.Code, rr2.Body.String())
	}
	req3 := httptest.NewRequest(http.MethodGet, "/api/info", nil)
	req3.Host = "127.0.0.1:8080"
	req3.SetBasicAuth("ada", "secret")
	rr3 := httptest.NewRecorder()
	h.ServeHTTP(rr3, req3)
	if rr3.Code != http.StatusTooManyRequests {
		t.Fatalf("blocked good password %d", rr3.Code)
	}

	cfg2 := DefaultConfig()
	cfg2.User = "ada"
	cfg2.Password = "secret"
	cfg2.MaxBodyBytes = 4
	ex2, err := New(db, cfg2)
	if err != nil {
		t.Fatal(err)
	}
	body := strings.NewReader(`{"sql":"select 1"}`)
	req4 := httptest.NewRequest(http.MethodPost, "/api/query", body)
	req4.Host = "127.0.0.1:8080"
	req4.SetBasicAuth("ada", "secret")
	req4.Header.Set("Content-Type", "application/json")
	rr4 := httptest.NewRecorder()
	ex2.Handler().ServeHTTP(rr4, req4)
	if rr4.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("body cap %d %s", rr4.Code, rr4.Body.String())
	}

	cfg3 := DefaultConfig()
	cfg3.MaxResponseBytes = 8
	ex3, err := New(db, cfg3)
	if err != nil {
		t.Fatal(err)
	}
	small := doRequest(ex3.Handler(), http.MethodGet, "/api/info", nil, "")
	if small.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("response cap %d %s", small.Code, small.Body.String())
	}
}

func TestCancelledQuery(t *testing.T) {
	db := openMem(t)
	ex, err := New(db, DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req := httptest.NewRequest(http.MethodGet, "/api/tables", nil).WithContext(ctx)
	req.Host = "127.0.0.1:8080"
	rr := httptest.NewRecorder()
	ex.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusGatewayTimeout {
		t.Fatalf("%d %s", rr.Code, rr.Body.String())
	}
}

func TestFlagsIgnoreDatabasePassword(t *testing.T) {
	t.Setenv("MYSQL_PASSWORD", "db-secret")
	t.Setenv("DATABASE_URL", "postgres://ada:db-secret@localhost/app")
	var cfg Config
	fs := Flags(&cfg)
	if err := fs.Parse([]string{"--addr", "127.0.0.1:9090", "--readonly=false", "--user", "ada", "--max-rows", "10"}); err != nil {
		t.Fatal(err)
	}
	if err := ApplyAccess(&cfg, fs, "readonly"); err != nil {
		t.Fatal(err)
	}
	if cfg.Password != "" || cfg.ReadOnly || cfg.Access != AccessFull || cfg.Addr != "127.0.0.1:9090" || cfg.User != "ada" || cfg.MaxRows != 10 {
		t.Fatalf("%+v", cfg)
	}
}

func TestAccessConfig(t *testing.T) {
	db := openMem(t)
	execSQL(t, db, `CREATE TABLE t (id INTEGER)`)
	execSQL(t, db, `INSERT INTO t VALUES (1)`)

	full, err := New(db, Config{Access: AccessFull})
	if err != nil {
		t.Fatal(err)
	}
	if full.cfg.ReadOnly || full.cfg.Access != AccessFull {
		t.Fatalf("full %+v", full.cfg)
	}
	if rr := postSQL(full.Handler(), "delete from t"); rr.Code != http.StatusOK {
		t.Fatalf("full delete %d %s", rr.Code, rr.Body.String())
	}

	execSQL(t, db, `INSERT INTO t VALUES (1)`)
	ro, err := New(db, Config{Access: AccessReadOnly})
	if err != nil {
		t.Fatal(err)
	}
	if !ro.cfg.ReadOnly || ro.cfg.Access != AccessReadOnly {
		t.Fatalf("readonly %+v", ro.cfg)
	}
	if rr := postSQL(ro.Handler(), "delete from t"); rr.Code != http.StatusForbidden {
		t.Fatalf("readonly delete %d %s", rr.Code, rr.Body.String())
	}

	if _, err := New(db, Config{Access: "write"}); err == nil {
		t.Fatal("expected invalid access to fail")
	}

	var cfg Config
	fs := Flags(&cfg)
	if err := fs.Parse([]string{"--access", "full"}); err != nil {
		t.Fatal(err)
	}
	if err := ApplyAccess(&cfg, fs, "readonly"); err != nil {
		t.Fatal(err)
	}
	if cfg.Access != AccessFull || cfg.ReadOnly {
		t.Fatalf("flag access %+v readonly %v", cfg.Access, cfg.ReadOnly)
	}

	var both Config
	bothSet := Flags(&both)
	if err := bothSet.Parse([]string{"--access", "full", "--readonly=false"}); err != nil {
		t.Fatal(err)
	}
	if err := ApplyAccess(&both, bothSet, ""); err == nil {
		t.Fatal("expected conflicting access flags to fail")
	}

	var fromEnv Config
	envSet := Flags(&fromEnv)
	if err := envSet.Parse(nil); err != nil {
		t.Fatal(err)
	}
	if err := ApplyAccess(&fromEnv, envSet, "full"); err != nil {
		t.Fatal(err)
	}
	if fromEnv.Access != AccessFull || fromEnv.ReadOnly {
		t.Fatalf("env access %+v", fromEnv)
	}
}

func TestListenAndServe(t *testing.T) {
	db := openMem(t)
	cfg := DefaultConfig()
	cfg.Addr = "192.0.2.1:8080"
	ex, err := New(db, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := ex.ListenAndServe(context.Background()); err == nil {
		t.Fatal("expected public address to be rejected")
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	cfg = DefaultConfig()
	cfg.Addr = addr
	ex, err = New(db, cfg)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- ex.ListenAndServe(ctx) }()
	deadline := time.Now().Add(3 * time.Second)
	var resp *http.Response
	for time.Now().Before(deadline) {
		resp, err = http.Get("http://" + addr + "/api/info")
		if err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatal(resp.Status)
	}
	cancel()
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("shutdown timed out")
	}
}

func TestListenAddr(t *testing.T) {
	for _, addr := range []string{"127.0.0.1:8080", "localhost:8080", "0.0.0.0:8080", "[::]:8080", "[::1]:8080"} {
		if err := requireLoopbackAddr(addr); err != nil {
			t.Errorf("%s: %v", addr, err)
		}
	}
	for _, addr := range []string{"192.0.2.1:8080", "example.com:8080", ":8080", "0.0.0.0"} {
		if err := requireLoopbackAddr(addr); err == nil {
			t.Errorf("%s was accepted", addr)
		}
	}
}

func TestRunRejectsPublicAddr(t *testing.T) {
	db := openMem(t)
	err := Run(context.Background(), db, []string{"--addr", "192.0.2.1:80"})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestAudit(t *testing.T) {
	db := openMem(t)
	execSQL(t, db, `CREATE TABLE t (id INTEGER)`)
	var seen []string
	cfg := DefaultConfig()
	cfg.Audit = func(ctx context.Context, query string, err error) {
		seen = append(seen, query)
	}
	ex, err := New(db, cfg)
	if err != nil {
		t.Fatal(err)
	}
	rr := postSQL(ex.Handler(), "select 1")
	if rr.Code != http.StatusOK {
		t.Fatalf("%d %s", rr.Code, rr.Body.String())
	}
	if len(seen) == 0 || !strings.Contains(strings.ToLower(seen[len(seen)-1]), "select") {
		t.Fatalf("audit %#v", seen)
	}
}
