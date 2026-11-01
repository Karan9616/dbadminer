package main

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dbadminer/dbadminer"
)

func TestDatabasesAreIsolated(t *testing.T) {
	dir := t.TempDir()
	alphaPath := filepath.Join(dir, "alpha.db")
	betaPath := filepath.Join(dir, "beta.db")
	seedSQLite(t, alphaPath, "only_alpha")
	seedSQLite(t, betaPath, "only_beta")

	items, err := openConfigured(context.Background(), dbadminer.DefaultConfig(), func(key string) string {
		switch key {
		case "DBADMINER_DATABASES":
			return "alpha,beta"
		case "ALPHA_SQLITE_PATH":
			return alphaPath
		case "BETA_SQLITE_PATH":
			return betaPath
		case "ALPHA_DB_PASSWORD":
			return "super-secret-value"
		default:
			return ""
		}
	}, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for _, item := range items {
			_ = item.db.Close()
		}
	})
	h := handlerFor(items)

	list := getOK(t, h, "/api/databases")
	if strings.Contains(list, "super-secret-value") || strings.Contains(list, alphaPath) || strings.Contains(list, "password") {
		t.Fatalf("list leaked credentials: %s", list)
	}
	if !strings.Contains(list, `"name":"alpha"`) || !strings.Contains(list, `"name":"beta"`) || !strings.Contains(list, `"dialect":"sqlite"`) {
		t.Fatalf("list: %s", list)
	}

	alpha := getOK(t, h, "/api/databases/alpha/tables")
	beta := getOK(t, h, "/api/databases/beta/tables")
	if !strings.Contains(alpha, "only_alpha") || strings.Contains(alpha, "only_beta") {
		t.Fatalf("alpha tables: %s", alpha)
	}
	if !strings.Contains(beta, "only_beta") || strings.Contains(beta, "only_alpha") {
		t.Fatalf("beta tables: %s", beta)
	}
	first := getOK(t, h, "/api/tables")
	if !strings.Contains(first, "only_alpha") || strings.Contains(first, "only_beta") {
		t.Fatalf("first database: %s", first)
	}
	missing := request(h, http.MethodGet, "/api/databases/other/tables")
	if missing.Code != http.StatusNotFound || !strings.Contains(missing.Body.String(), "database not found") {
		t.Fatalf("missing %d %s", missing.Code, missing.Body.String())
	}
}

func seedSQLite(t *testing.T, path, table string) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if _, err := db.Exec(`CREATE TABLE ` + table + ` (id INTEGER)`); err != nil {
		t.Fatal(err)
	}
}

func getOK(t *testing.T, h http.Handler, path string) string {
	t.Helper()
	rr := request(h, http.MethodGet, path)
	if rr.Code != http.StatusOK {
		t.Fatalf("%s %d %s", path, rr.Code, rr.Body.String())
	}
	return rr.Body.String()
}

func request(h http.Handler, method, path string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	req.Host = "127.0.0.1:8080"
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return rr
}
