// Package dbopen connects to PostgreSQL, MySQL, or SQLite from the environment
// variables already used by most applications, then serves DBAdminer.
// Importing this package pulls in database drivers. Programs that already
// hold a *sql.DB can import github.com/dbadminer/dbadminer instead.
package dbopen

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
	return open(ctx, os.Getenv, "", true)
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
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := dbadminer.ApplyAccess(&cfg, fs, os.Getenv("DBADMINER_ACCESS")); err != nil {
		return err
	}
	db, err := open(ctx, os.Getenv, *dsn, cfg.ReadOnly)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()
	ex, err := dbadminer.New(db, cfg)
	if err != nil {
		return err
	}
	return ex.ListenAndServe(ctx)
}

func open(ctx context.Context, getenv func(string) string, dsn string, readOnly bool) (*sql.DB, error) {
	conn, err := dbadminer.ResolveConn(getenv, dsn, readOnly)
	if err != nil {
		return nil, err
	}
	db, err := sql.Open(conn.Driver, conn.DSN)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(8)
	db.SetMaxIdleConns(8)
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		msg := err.Error()
		if conn.DSN != "" {
			msg = strings.ReplaceAll(msg, conn.DSN, conn.Source)
		}
		return nil, fmt.Errorf("dbadminer: database ping failed: %s", msg)
	}
	return db, nil
}
