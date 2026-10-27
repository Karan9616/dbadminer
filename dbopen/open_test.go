package dbopen

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
)

func TestOpenSQLiteReadOnly(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE t (id INTEGER)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO t VALUES (1)`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	getenv := func(key string) string {
		if key == "SQLITE_PATH" {
			return path
		}
		return ""
	}
	ro, err := open(context.Background(), getenv, "", true)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ro.Close() }()
	if _, err := ro.Exec(`INSERT INTO t VALUES (2)`); err == nil {
		t.Fatal("expected read-only insert to fail")
	}
	var id int
	if err := ro.QueryRow(`SELECT id FROM t`).Scan(&id); err != nil || id != 1 {
		t.Fatalf("read id=%d err=%v", id, err)
	}

	rw, err := open(context.Background(), getenv, "", false)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rw.Close() }()
	if _, err := rw.Exec(`INSERT INTO t VALUES (2)`); err != nil {
		t.Fatal(err)
	}
}

func TestOpenMissingEnv(t *testing.T) {
	_, err := open(context.Background(), func(string) string { return "" }, "", true)
	if err == nil {
		t.Fatal("expected error")
	}
}
