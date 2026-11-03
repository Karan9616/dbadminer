package dbadminer

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
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
	Exercise(t, db)
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

func TestSchemaLimit(t *testing.T) {
	db := openMem(t)
	execSQL(t, db, `CREATE TABLE keep (id INTEGER)`)
	cfg := DefaultConfig()
	cfg.Schema = "main"
	ex, err := New(db, cfg)
	if err != nil {
		t.Fatal(err)
	}
	infoRR := doRequest(ex.Handler(), http.MethodGet, "/api/info", nil, "")
	var info infoPayload
	decodeJSON(t, infoRR, &info)
	if info.Schema != "main" {
		t.Fatalf("%+v", info)
	}
	tablesRR := doRequest(ex.Handler(), http.MethodGet, "/api/tables", nil, "")
	var tables tablesPayload
	decodeJSON(t, tablesRR, &tables)
	if len(tables.Tables) != 1 || tables.Tables[0].Name != "keep" || tables.Tables[0].Schema != "main" {
		t.Fatalf("%+v", tables.Tables)
	}

	hidden := DefaultConfig()
	hidden.Schema = "other"
	exHidden, err := New(db, hidden)
	if err != nil {
		t.Fatal(err)
	}
	tablesRR = doRequest(exHidden.Handler(), http.MethodGet, "/api/tables", nil, "")
	decodeJSON(t, tablesRR, &tables)
	if len(tables.Tables) != 0 {
		t.Fatalf("%+v", tables.Tables)
	}
	missing := doRequest(exHidden.Handler(), http.MethodGet, "/api/tables/main/keep", nil, "")
	if missing.Code != http.StatusNotFound {
		t.Fatalf("hidden table %d %s", missing.Code, missing.Body.String())
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
