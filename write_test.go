package dbadminer

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestSchemaEndpoint(t *testing.T) {
	db := openMem(t)
	execSQL(t, db, `CREATE TABLE parent (id INTEGER PRIMARY KEY)`)
	execSQL(t, db, `CREATE TABLE child (id INTEGER PRIMARY KEY, parent_id INTEGER NOT NULL, name TEXT, FOREIGN KEY (parent_id) REFERENCES parent(id))`)
	execSQL(t, db, `CREATE INDEX child_name_idx ON child (name)`)
	cfg := DefaultConfig()
	ex, err := New(db, cfg)
	if err != nil {
		t.Fatal(err)
	}
	h := ex.Handler()
	rr := doRequest(h, http.MethodGet, "/api/tables/main/child/schema", nil, "")
	if rr.Code != http.StatusOK {
		t.Fatalf("%d %s", rr.Code, rr.Body.String())
	}
	var payload schemaPayload
	decodeJSON(t, rr, &payload)
	if len(payload.Columns) < 2 || payload.DDL == "" {
		t.Fatalf("%+v", payload)
	}
	if len(payload.Indexes) == 0 {
		t.Fatalf("expected indexes: %+v", payload.Indexes)
	}
	if len(payload.ForeignKeys) == 0 {
		t.Fatalf("expected foreign keys: %+v", payload.ForeignKeys)
	}
}

func TestWriteAPIsRequireFullAccess(t *testing.T) {
	db := openMem(t)
	execSQL(t, db, `CREATE TABLE t (id INTEGER PRIMARY KEY, name TEXT)`)
	ex, err := New(db, DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	h := ex.Handler()
	body := strings.NewReader(`{"schema":"main","name":"x","columns":[{"name":"id","type":"INTEGER","primaryKey":true}]}`)
	rr := doRequest(h, http.MethodPost, "/api/tables", body, "application/json")
	if rr.Code != http.StatusForbidden {
		t.Fatalf("create table want 403 got %d %s", rr.Code, rr.Body.String())
	}
	rr = doRequest(h, http.MethodPost, "/api/tables/main/t/rows", strings.NewReader(`{"values":{"id":1,"name":"a"}}`), "application/json")
	if rr.Code != http.StatusForbidden {
		t.Fatalf("insert want 403 got %d %s", rr.Code, rr.Body.String())
	}
	rr = doRequest(h, http.MethodDelete, "/api/tables/main/t?type=table", nil, "")
	if rr.Code != http.StatusForbidden {
		t.Fatalf("drop want 403 got %d %s", rr.Code, rr.Body.String())
	}
}

func TestFullAccessSchemaAndRows(t *testing.T) {
	db := openMem(t)
	cfg := DefaultConfig()
	cfg.Access = AccessFull
	ex, err := New(db, cfg)
	if err != nil {
		t.Fatal(err)
	}
	h := ex.Handler()

	createBody := `{"schema":"main","name":"people","columns":[{"name":"id","type":"INTEGER","nullable":false,"primaryKey":true},{"name":"name","type":"TEXT","nullable":false}]}`
	rr := doRequest(h, http.MethodPost, "/api/tables", strings.NewReader(createBody), "application/json")
	if rr.Code != http.StatusOK {
		t.Fatalf("create %d %s", rr.Code, rr.Body.String())
	}

	rr = doRequest(h, http.MethodPost, "/api/tables/main/people/rows", strings.NewReader(`{"values":{"id":1,"name":"ada"}}`), "application/json")
	if rr.Code != http.StatusOK {
		t.Fatalf("insert %d %s", rr.Code, rr.Body.String())
	}

	rr = doRequest(h, http.MethodPatch, "/api/tables/main/people/rows", strings.NewReader(`{"key":{"id":1},"values":{"name":"Ada"}}`), "application/json")
	if rr.Code != http.StatusOK {
		t.Fatalf("update %d %s", rr.Code, rr.Body.String())
	}

	rows := getRows(t, h, "main", "people", "limit=50&offset=0")
	if len(rows.Rows) != 1 || fmt.Sprint(rows.Rows[0][1]) != "Ada" {
		t.Fatalf("rows %+v", rows)
	}

	rr = doRequest(h, http.MethodPost, "/api/tables/main/people/indexes", strings.NewReader(`{"name":"people_name_idx","unique":false,"columns":["name"]}`), "application/json")
	if rr.Code != http.StatusOK {
		t.Fatalf("index %d %s", rr.Code, rr.Body.String())
	}

	alter := `{"ops":[{"op":"add_column","name":"age","type":"INTEGER","nullable":true}]}`
	rr = doRequest(h, http.MethodPatch, "/api/tables/main/people", strings.NewReader(alter), "application/json")
	if rr.Code != http.StatusOK {
		t.Fatalf("alter %d %s", rr.Code, rr.Body.String())
	}

	rr = doRequest(h, http.MethodPost, "/api/views", strings.NewReader(`{"schema":"main","name":"people_v","select":"SELECT id, name FROM people"}`), "application/json")
	if rr.Code != http.StatusOK {
		t.Fatalf("view %d %s", rr.Code, rr.Body.String())
	}

	rr = doRequest(h, http.MethodDelete, "/api/tables/main/people/rows", strings.NewReader(`{"key":{"id":1}}`), "application/json")
	if rr.Code != http.StatusOK {
		t.Fatalf("delete %d %s", rr.Code, rr.Body.String())
	}

	rr = doRequest(h, http.MethodDelete, "/api/tables/main/people_v?type=view", nil, "")
	if rr.Code != http.StatusOK {
		t.Fatalf("drop view %d %s", rr.Code, rr.Body.String())
	}

	rr = doRequest(h, http.MethodDelete, "/api/tables/main/people/indexes/people_name_idx", nil, "")
	if rr.Code != http.StatusOK {
		t.Fatalf("drop index %d %s", rr.Code, rr.Body.String())
	}

	rr = doRequest(h, http.MethodDelete, "/api/tables/main/people?type=table", nil, "")
	if rr.Code != http.StatusOK {
		t.Fatalf("drop table %d %s", rr.Code, rr.Body.String())
	}
}

func TestRowEditRequiresPrimaryKey(t *testing.T) {
	db := openMem(t)
	execSQL(t, db, `CREATE TABLE nopk (val TEXT)`)
	cfg := DefaultConfig()
	cfg.Access = AccessFull
	ex, err := New(db, cfg)
	if err != nil {
		t.Fatal(err)
	}
	h := ex.Handler()
	rr := doRequest(h, http.MethodPost, "/api/tables/main/nopk/rows", strings.NewReader(`{"values":{"val":"x"}}`), "application/json")
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("want 400 got %d %s", rr.Code, rr.Body.String())
	}
}

func TestCreateTableSQLBuilders(t *testing.T) {
	sql, err := createTableSQL(postgresDialect{}, "public", "t", []columnDef{
		{Name: "id", Type: "BIGINT", Nullable: false, PrimaryKey: true},
		{Name: "name", Type: "TEXT", Nullable: false},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sql, `"public"."t"`) || !strings.Contains(sql, "PRIMARY KEY") {
		t.Fatal(sql)
	}
	_, err = createTableSQL(sqliteDialect{}, "main", "bad;drop", []columnDef{{Name: "id", Type: "INTEGER"}})
	if err == nil {
		t.Fatal("expected invalid identifier")
	}
}

func TestDDLHelpers(t *testing.T) {
	idx, err := createIndexSQL(sqliteDialect{}, "main", "t", createIndexReq{Name: "idx_t_a", Columns: []string{"a"}})
	if err != nil || !strings.Contains(idx, "CREATE") {
		t.Fatalf("%v %s", err, idx)
	}
	fk, err := createForeignKeySQL(postgresDialect{}, "public", "child", createFKReq{
		Name: "child_parent_fk", Columns: []string{"parent_id"}, RefSchema: "public", RefTable: "parent", RefColumns: []string{"id"}, OnDelete: "CASCADE",
	})
	if err != nil || !strings.Contains(fk, "FOREIGN KEY") || !strings.Contains(fk, "ON DELETE CASCADE") {
		t.Fatalf("%v %s", err, fk)
	}
}

func TestMutateResultJSON(t *testing.T) {
	n := int64(1)
	b, err := json.Marshal(mutateResult{RowsAffected: n, SQL: "SELECT 1"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "rowsAffected") {
		t.Fatal(string(b))
	}
}

func TestBindSQLArgJSONAndArray(t *testing.T) {
	obj, err := bindSQLArg("JSONB", map[string]any{"ok": true})
	if err != nil {
		t.Fatal(err)
	}
	raw, ok := obj.(json.RawMessage)
	if !ok || string(raw) != `{"ok":true}` {
		t.Fatalf("json arg = %#v", obj)
	}

	arr, err := bindSQLArg("ARRAY", []any{"a", "b"})
	if err != nil {
		t.Fatal(err)
	}
	strs, ok := arr.([]string)
	if !ok || len(strs) != 2 || strs[0] != "a" || strs[1] != "b" {
		t.Fatalf("array arg = %#v", arr)
	}

	jsonArr, err := bindSQLArg("JSON", []any{1, 2})
	if err != nil {
		t.Fatal(err)
	}
	raw, ok = jsonArr.(json.RawMessage)
	if !ok || string(raw) != `[1,2]` {
		t.Fatalf("json array arg = %#v", jsonArr)
	}
}

func TestUpdateRowJSONObject(t *testing.T) {
	db := openMem(t)
	execSQL(t, db, `CREATE TABLE docs (id INTEGER PRIMARY KEY, meta TEXT NOT NULL)`)
	execSQL(t, db, `INSERT INTO docs (id, meta) VALUES (1, '{}')`)
	cfg := DefaultConfig()
	cfg.Access = AccessFull
	ex, err := New(db, cfg)
	if err != nil {
		t.Fatal(err)
	}
	h := ex.Handler()
	body := `{"key":{"id":1},"values":{"meta":{"kind":"edge","empty":{}}}}`
	rr := doRequest(h, http.MethodPatch, "/api/tables/main/docs/rows", strings.NewReader(body), "application/json")
	if rr.Code != http.StatusOK {
		t.Fatalf("update %d %s", rr.Code, rr.Body.String())
	}
	rows := getRows(t, h, "main", "docs", "limit=10&offset=0")
	if len(rows.Rows) != 1 {
		t.Fatalf("rows %+v", rows)
	}
	meta := fmt.Sprint(rows.Rows[0][1])
	if !strings.Contains(meta, "edge") {
		t.Fatalf("meta = %#v", rows.Rows[0][1])
	}
}
