package main

import (
	"net/url"
	"strings"
	"testing"
)

func envOf(values map[string]string) func(string) string {
	return func(key string) string {
		return values[key]
	}
}

func TestResolveConnPrecedence(t *testing.T) {
	conn, err := ResolveConn(envOf(map[string]string{
		"DATABASE_URL": "postgres://db.example/app",
		"PGDATABASE":   "other",
		"PGHOST":       "ignored",
	}), "", true)
	if err != nil {
		t.Fatal(err)
	}
	if conn.Source != "DATABASE_URL" || conn.Driver != "pgx" || conn.DSN != "postgres://db.example/app" {
		t.Fatalf("%+v", conn)
	}
}

func TestResolveConnFamilies(t *testing.T) {
	cases := []struct {
		name   string
		env    map[string]string
		flag   string
		ro     bool
		driver string
		source string
		dsnHas string
	}{
		{
			name:   "private url",
			env:    map[string]string{"DATABASE_PRIVATE_URL": "postgres://db.example/app"},
			driver: "pgx", source: "DATABASE_PRIVATE_URL", dsnHas: "postgres://db.example/app", ro: true,
		},
		{
			name:   "libpq",
			env:    map[string]string{"PGHOST": "db.example", "PGUSER": "ada", "PGPASSWORD": "p@ss", "PGDATABASE": "app", "PGSSLMODE": "require"},
			driver: "pgx", source: "PGDATABASE", dsnHas: "sslmode=require", ro: true,
		},
		{
			name:   "docker postgres",
			env:    map[string]string{"POSTGRES_DB": "app", "POSTGRES_USER": "ada", "POSTGRES_PASSWORD": "secret"},
			driver: "pgx", source: "POSTGRES_DB", dsnHas: "postgres://ada:secret@localhost:5432/app", ro: true,
		},
		{
			name:   "mysql url",
			env:    map[string]string{"DATABASE_URL": "mysql://alice:secret@127.0.0.1:3306/app?charset=utf8mb4"},
			driver: "mysql", source: "DATABASE_URL", dsnHas: "alice:secret@tcp(127.0.0.1:3306)/app?", ro: true,
		},
		{
			name:   "mysql ipv6",
			env:    map[string]string{"MYSQL_URL": "mysql://alice:secret@[::1]:3306/app"},
			driver: "mysql", source: "MYSQL_URL", dsnHas: "tcp([::1]:3306)/app", ro: true,
		},
		{
			name:   "mysql env pwd",
			env:    map[string]string{"MYSQL_DATABASE": "app", "MYSQL_USER": "ada", "MYSQL_PWD": "secret", "MYSQL_TCP_PORT": "3307"},
			driver: "mysql", source: "MYSQL_DATABASE", dsnHas: "ada:secret@tcp(localhost:3307)/app?parseTime=true", ro: true,
		},
		{
			name:   "mysql dsn",
			env:    map[string]string{"MYSQL_DSN": "ada:secret@tcp(localhost:3306)/app"},
			driver: "mysql", source: "MYSQL_DSN", dsnHas: "ada:secret@tcp(localhost:3306)/app", ro: true,
		},
		{
			name:   "generic postgres",
			env:    map[string]string{"DB_CONNECTION": "pgsql", "DB_HOST": "db.example", "DB_NAME": "app", "DB_USERNAME": "ada"},
			driver: "pgx", source: "DB_NAME", dsnHas: "postgres://ada@db.example:5432/app", ro: true,
		},
		{
			name:   "sqlite path",
			env:    map[string]string{"SQLITE_PATH": "/tmp/app.db"},
			driver: "sqlite", source: "SQLITE_PATH", dsnHas: "mode=ro", ro: true,
		},
		{
			name:   "sqlite writable",
			env:    map[string]string{"SQLITE_DATABASE": "/tmp/app.db"},
			driver: "sqlite", source: "SQLITE_DATABASE", dsnHas: "file://", ro: false,
		},
		{
			name:   "dsn flag",
			flag:   "postgres://db.example/override",
			env:    map[string]string{"DATABASE_URL": "postgres://db.example/nope"},
			driver: "pgx", source: "--dsn", dsnHas: "override", ro: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			conn, err := ResolveConn(envOf(tc.env), tc.flag, tc.ro)
			if err != nil {
				t.Fatal(err)
			}
			if conn.Driver != tc.driver || conn.Source != tc.source || !strings.Contains(conn.DSN, tc.dsnHas) {
				t.Fatalf("%+v", conn)
			}
			if tc.ro && conn.Driver == "sqlite" && !strings.Contains(conn.DSN, "mode=ro") {
				t.Fatalf("missing mode=ro: %s", conn.DSN)
			}
			if !tc.ro && conn.Driver == "sqlite" && strings.Contains(conn.DSN, "mode=ro") {
				t.Fatalf("unexpected mode=ro: %s", conn.DSN)
			}
		})
	}
}

func TestResolveConnMySQLRewrite(t *testing.T) {
	conn, err := ResolveConn(envOf(map[string]string{
		"DATABASE_URL": "mysql://alice:secret@127.0.0.1:3306/app?charset=utf8mb4",
	}), "", true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(conn.DSN, "parseTime=true") || !strings.Contains(conn.DSN, "charset=utf8mb4") {
		t.Fatal(conn.DSN)
	}
	if _, err := ResolveConn(envOf(map[string]string{
		"DATABASE_URL": "mysql://alice:p@ss@127.0.0.1:3306/app",
	}), "", true); err == nil {
		t.Fatal("expected password rejection")
	}
}

func TestResolveConnLibpqPassword(t *testing.T) {
	conn, err := ResolveConn(envOf(map[string]string{
		"PGDATABASE": "app",
		"PGUSER":     "ada",
		"PGPASSWORD": "p@ss",
	}), "", true)
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(conn.DSN)
	if err != nil {
		t.Fatal(err)
	}
	pass, _ := u.User.Password()
	if pass != "p@ss" || u.Hostname() != "localhost" || u.Port() != "5432" {
		t.Fatalf("%s", conn.DSN)
	}
}

func TestResolveConnErrors(t *testing.T) {
	_, err := ResolveConn(envOf(nil), "", true)
	if err == nil || !strings.Contains(err.Error(), "DATABASE_URL") {
		t.Fatal(err)
	}
	_, err = ResolveConn(envOf(map[string]string{
		"POSTGRES_URL": "postgres://db.example/a",
		"MYSQL_URL":    "mysql://db.example/b",
	}), "", true)
	if err == nil || !strings.Contains(err.Error(), "POSTGRES_URL") {
		t.Fatal(err)
	}
	_, err = ResolveConn(envOf(map[string]string{"DB_DATABASE": "app"}), "", true)
	if err == nil || !strings.Contains(err.Error(), "DB_CONNECTION") {
		t.Fatal(err)
	}
	_, err = ResolveConn(envOf(map[string]string{"DATABASE_URL": "postgres://db.example/a\nb"}), "", true)
	if err == nil {
		t.Fatal("expected control character rejection")
	}
	conn, err := ResolveConn(envOf(map[string]string{
		"DB_CONNECTION": "sqlite",
		"DB_DATABASE":   "/tmp/app.db",
	}), "", true)
	if err != nil {
		t.Fatal(err)
	}
	if conn.Driver != "sqlite" || !strings.Contains(conn.DSN, "mode=ro") {
		t.Fatalf("%+v", conn)
	}
}

func TestResolveConnSeparateOptions(t *testing.T) {
	conn, err := ResolveConn(envOf(map[string]string{
		"DATABASE_URL": "postgres://postgres:5432/postgres?sslmode=disable",
		"DB_USER":      "root",
		"DB_PASSWORD":  "root@123",
		"DB_SCHEMA":    "public",
	}), "", true)
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(conn.DSN)
	if err != nil {
		t.Fatal(err)
	}
	pass, _ := u.User.Password()
	if u.User.Username() != "root" || pass != "root@123" || u.Hostname() != "postgres" || u.Port() != "5432" || u.Path != "/postgres" {
		t.Fatalf("%s", conn.DSN)
	}
	if u.Query().Get("sslmode") != "disable" || u.Query().Get("search_path") != "public" || conn.Schema != "public" {
		t.Fatalf("%s schema=%s", conn.DSN, conn.Schema)
	}

	overridden, err := ResolveConn(envOf(map[string]string{
		"DATABASE_URL": "postgres://ada:secret@old.example:5432/old?sslmode=require",
		"DB_HOST":      "db.example",
		"DB_PORT":      "5433",
		"DB_NAME":      "app",
	}), "", true)
	if err != nil {
		t.Fatal(err)
	}
	u, err = url.Parse(overridden.DSN)
	if err != nil {
		t.Fatal(err)
	}
	pass, _ = u.User.Password()
	if u.Hostname() != "db.example" || u.Port() != "5433" || u.Path != "/app" || u.User.Username() != "ada" || pass != "secret" || u.Query().Get("sslmode") != "require" {
		t.Fatalf("%s", overridden.DSN)
	}

	keyword, err := ResolveConn(nil, "host=localhost dbname=app user=ada", true)
	if err != nil {
		t.Fatal(err)
	}
	if keyword.Driver != "pgx" || !strings.Contains(keyword.DSN, "host=localhost") {
		t.Fatalf("%+v", keyword)
	}
	keyword, err = ResolveConn(envOf(map[string]string{
		"DB_PASSWORD": "p@ss",
		"DB_SCHEMA":   "app",
	}), "host=localhost dbname=app user=ada", true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(keyword.DSN, "password=p@ss") || !strings.Contains(keyword.DSN, "search_path=app") || keyword.Schema != "app" {
		t.Fatalf("%+v", keyword)
	}

	mysqlConn, err := ResolveConn(envOf(map[string]string{
		"DATABASE_URL": "mysql://alice:secret@127.0.0.1:3306/app",
		"DB_HOST":      "db.example",
		"DB_PORT":      "3307",
		"DB_SCHEMA":    "app",
	}), "", true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(mysqlConn.DSN, "tcp(db.example:3307)/app") || mysqlConn.Schema != "app" {
		t.Fatalf("%+v", mysqlConn)
	}

	if _, err := ResolveConn(envOf(map[string]string{
		"DATABASE_URL": "postgres://db.example/app",
		"DB_SCHEMA":    "public;drop",
	}), "", true); err == nil {
		t.Fatal("expected schema rejection")
	}

	sqliteConn, err := ResolveConn(envOf(map[string]string{
		"SQLITE_PATH": "/tmp/app.db",
		"DB_SCHEMA":   "main",
		"DB_HOST":     "ignored.example",
	}), "", true)
	if err != nil {
		t.Fatal(err)
	}
	if sqliteConn.Schema != "main" || strings.Contains(sqliteConn.DSN, "ignored.example") {
		t.Fatalf("%+v", sqliteConn)
	}
}

func TestSQLiteModeOverride(t *testing.T) {
	conn, err := ResolveConn(envOf(map[string]string{
		"SQLITE_URL": "file:/tmp/app.db?mode=rw",
	}), "", true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(conn.DSN, "mode=ro") || strings.Contains(conn.DSN, "mode=rw") {
		t.Fatal(conn.DSN)
	}
}
