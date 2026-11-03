// Command demo seeds sample databases and serves a tiny hello-world app.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	_ "github.com/go-sql-driver/mysql"
	_ "github.com/jackc/pgx/v5/stdlib"
)

func main() {
	log.SetFlags(0)
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "seed":
		err = runSeed(os.Args[2:])
	case "serve":
		err = runServe(os.Args[2:])
	case "help", "-h", "--help":
		usage()
		return
	default:
		usage()
		os.Exit(2)
	}
	if err != nil {
		log.Fatal(err)
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, `usage: demo <command>

Commands:
  seed   create sample tables and rows in DEMO_DATABASES
  serve  run a small hello-world HTTP app (DEMO_ADDR, default :8090)

For each name in DEMO_DATABASES, reads <NAME>_DATABASE_URL or <NAME>_DSN,
plus optional <NAME>_DB_USER and <NAME>_DB_PASSWORD overrides.

Example:
  DEMO_DATABASES=shop,app \
  SHOP_DATABASE_URL=mysql://mysql:3306/shop \
  SHOP_DB_USER=dbadminer \
  SHOP_DB_PASSWORD=dbadminer \
  APP_DATABASE_URL=postgres://postgres:5432/app?sslmode=disable \
  APP_DB_USER=dbadminer \
  APP_DB_PASSWORD=dbadminer \
  demo seed
`)
}

func runSeed(args []string) error {
	if len(args) > 0 {
		return errors.New("seed takes no arguments")
	}
	names, err := databaseNames(os.Getenv("DEMO_DATABASES"))
	if err != nil {
		return err
	}
	if len(names) == 0 {
		return errors.New("set DEMO_DATABASES to a comma-separated list of database names")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	for _, name := range names {
		dsn, driver, err := resolveDSN(name)
		if err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		if err := seedOne(ctx, name, driver, dsn); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		log.Printf("seeded %s (%s)", name, driver)
	}
	return nil
}

func databaseNames(raw string) ([]string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	var names []string
	seen := map[string]struct{}{}
	for _, part := range strings.Split(raw, ",") {
		name := strings.TrimSpace(part)
		if name == "" {
			continue
		}
		key := strings.ToUpper(name)
		if _, ok := seen[key]; ok {
			return nil, fmt.Errorf("duplicate database name %q", name)
		}
		seen[key] = struct{}{}
		names = append(names, name)
	}
	return names, nil
}

func resolveDSN(name string) (dsn, driver string, err error) {
	prefix := strings.ToUpper(name) + "_"
	raw := firstNonEmpty(
		os.Getenv(prefix+"DATABASE_URL"),
		os.Getenv(prefix+"DSN"),
		os.Getenv(prefix+"MYSQL_DSN"),
	)
	if raw == "" {
		return "", "", fmt.Errorf("set %sDATABASE_URL or %sDSN", prefix, prefix)
	}
	user := os.Getenv(prefix + "DB_USER")
	pass := os.Getenv(prefix + "DB_PASSWORD")
	driver, dsn, err = normalizeDSN(raw, user, pass)
	return dsn, driver, err
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func normalizeDSN(raw, user, pass string) (driver, dsn string, err error) {
	switch {
	case strings.HasPrefix(raw, "postgres://"), strings.HasPrefix(raw, "postgresql://"):
		driver = "pgx"
		dsn, err = withUserPassPostgres(raw, user, pass)
		return driver, dsn, err
	case strings.HasPrefix(raw, "mysql://"):
		driver = "mysql"
		dsn, err = mysqlDSNFromURL(raw, user, pass)
		return driver, dsn, err
	case strings.Contains(raw, "@tcp(") || strings.HasPrefix(raw, "user:"):
		driver = "mysql"
		dsn = raw
		if user != "" {
			dsn, err = withUserPassMySQL(raw, user, pass)
		}
		return driver, dsn, err
	default:
		return "", "", fmt.Errorf("unsupported DSN %q", raw)
	}
}

func withUserPassPostgres(raw, user, pass string) (string, error) {
	if user == "" && pass == "" {
		return raw, nil
	}
	// postgres://host:port/db → inject userinfo
	rest := strings.TrimPrefix(strings.TrimPrefix(raw, "postgresql://"), "postgres://")
	if user == "" {
		return raw, nil
	}
	auth := user
	if pass != "" {
		auth += ":" + pass
	}
	if at := strings.Index(rest, "@"); at >= 0 {
		rest = rest[at+1:]
	}
	return "postgres://" + auth + "@" + rest, nil
}

func mysqlDSNFromURL(raw, user, pass string) (string, error) {
	u := strings.TrimPrefix(raw, "mysql://")
	hostPort, dbName, ok := strings.Cut(u, "/")
	if !ok {
		return "", fmt.Errorf("mysql URL needs a database path: %s", raw)
	}
	dbName, _, _ = strings.Cut(dbName, "?")
	if user == "" {
		user = "root"
	}
	auth := user
	if pass != "" {
		auth += ":" + pass
	}
	return fmt.Sprintf("%s@tcp(%s)/%s?parseTime=true&multiStatements=true", auth, hostPort, dbName), nil
}

func withUserPassMySQL(raw, user, pass string) (string, error) {
	rest := raw
	if at := strings.LastIndex(raw, "@"); at >= 0 {
		rest = raw[at+1:]
	}
	auth := user
	if pass != "" {
		auth += ":" + pass
	}
	if !strings.Contains(rest, "parseTime=") {
		if strings.Contains(rest, "?") {
			rest += "&parseTime=true&multiStatements=true"
		} else {
			rest += "?parseTime=true&multiStatements=true"
		}
	}
	return auth + "@" + rest, nil
}

func seedOne(ctx context.Context, name, driver, dsn string) error {
	db, err := openReady(ctx, driver, dsn)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	for _, stmt := range seedSQL(driver) {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("exec %q: %w", truncate(stmt, 60), err)
		}
	}
	_ = name
	return nil
}

func truncate(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
