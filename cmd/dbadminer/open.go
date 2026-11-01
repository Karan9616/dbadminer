package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/dbadminer/dbadminer"
	_ "github.com/go-sql-driver/mysql"
	_ "github.com/jackc/pgx/v5/stdlib"
	_ "modernc.org/sqlite"
)

// Open connects using the process environment. SQLite files open read-only.
func Open(ctx context.Context) (*sql.DB, error) {
	if ctx == nil {
		return nil, errors.New("dbadminer: nil context")
	}
	db, _, err := open(ctx, os.Getenv, "", true)
	return db, err
}

// Run parses explorer flags and --dsn, connects from the environment when
// --dsn is empty, and serves until ctx is cancelled.
func Run(ctx context.Context, args []string) error {
	if ctx == nil {
		return errors.New("dbadminer: nil context")
	}
	cfg := dbadminer.DefaultConfig()
	fs := dbadminer.Flags(&cfg)
	dsn := fs.String("dsn", "", "database URL or DSN; overrides DATABASE_URL and related variables")
	dbHost := fs.String("db-host", "", "database host; overrides the host in --dsn or DATABASE_URL")
	dbPort := fs.String("db-port", "", "database port; overrides the port in --dsn or DATABASE_URL")
	dbUser := fs.String("db-user", "", "database user; overrides the user in --dsn or DATABASE_URL")
	dbPassword := fs.String("db-password", "", "database password; overrides the password in --dsn or DATABASE_URL")
	dbName := fs.String("db-name", "", "database name; overrides the name in --dsn or DATABASE_URL")
	schema := fs.String("schema", "", "schema to show; on PostgreSQL also sets search_path")
	version := fs.Bool("version", false, "print version and exit")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *version {
		fmt.Println(dbadminer.Version)
		return nil
	}
	if err := dbadminer.ApplyAccess(&cfg, fs, os.Getenv("DBADMINER_ACCESS")); err != nil {
		return err
	}
	getenv := overlayGetenv(os.Getenv, *dbHost, *dbPort, *dbUser, *dbPassword, *dbName, *schema)
	items, err := openConfigured(ctx, cfg, getenv, *dsn)
	if err != nil {
		return err
	}
	defer func() {
		for _, item := range items {
			_ = item.db.Close()
		}
	}()
	return dbadminer.Serve(ctx, cfg, handlerFor(items))
}

func open(ctx context.Context, getenv func(string) string, dsn string, readOnly bool) (*sql.DB, string, error) {
	conn, err := ResolveConn(getenv, dsn, readOnly)
	if err != nil {
		return nil, "", err
	}
	db, err := sql.Open(conn.Driver, conn.DSN)
	if err != nil {
		return nil, "", err
	}
	db.SetMaxOpenConns(8)
	db.SetMaxIdleConns(8)
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		msg := err.Error()
		if conn.DSN != "" {
			msg = strings.ReplaceAll(msg, conn.DSN, conn.Source)
		}
		return nil, "", fmt.Errorf("dbadminer: database ping failed: %s", msg)
	}
	return db, conn.Schema, nil
}

func overlayGetenv(base func(string) string, host, port, user, pass, name, schema string) func(string) string {
	over := map[string]string{
		"DB_HOST":     host,
		"DB_PORT":     port,
		"DB_USER":     user,
		"DB_PASSWORD": pass,
		"DB_NAME":     name,
		"DB_SCHEMA":   schema,
	}
	return func(key string) string {
		if v := strings.TrimSpace(over[key]); v != "" {
			return v
		}
		if base == nil {
			return ""
		}
		return base(key)
	}
}
