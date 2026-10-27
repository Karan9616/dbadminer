package dbadminer

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
)

// ErrNoDatabase is returned when no supported database variable is set.
var ErrNoDatabase = errors.New("dbadminer: no database configured")

// Conn is a driver name and DSN resolved from flags or environment variables.
// Driver is "pgx", "mysql", or "sqlite".
type Conn struct {
	Driver string
	DSN    string
	Source string
}

// ResolveConn selects a database from dsnFlag or getenv. readOnly adds
// mode=ro to SQLite DSNs. The first match wins: --dsn, DATABASE_URL,
// DATABASE_PRIVATE_URL, a single engine URL, then libpq, Docker Postgres,
// MySQL, generic DB_*, and SQLite path variables.
func ResolveConn(getenv func(string) string, dsnFlag string, readOnly bool) (Conn, error) {
	get := func(key string) (string, error) {
		if getenv == nil {
			return "", nil
		}
		return cleanValue(getenv(key))
	}
	if strings.TrimSpace(dsnFlag) != "" {
		value, err := cleanValue(dsnFlag)
		if err != nil {
			return Conn{}, err
		}
		conn, err := classifyRaw(value, readOnly)
		if err != nil {
			return Conn{}, err
		}
		conn.Source = "--dsn"
		return conn, nil
	}
	if v, err := get("DATABASE_URL"); err != nil {
		return Conn{}, err
	} else if v != "" {
		return fromURL(v, readOnly, "DATABASE_URL")
	}
	if v, err := get("DATABASE_PRIVATE_URL"); err != nil {
		return Conn{}, err
	} else if v != "" {
		return fromURL(v, readOnly, "DATABASE_PRIVATE_URL")
	}

	urlKeys := []string{"POSTGRES_URL", "POSTGRESQL_URL", "MYSQL_URL", "MYSQL_DSN", "SQLITE_URL"}
	var found []string
	vals := map[string]string{}
	for _, key := range urlKeys {
		v, err := get(key)
		if err != nil {
			return Conn{}, err
		}
		if v != "" {
			found = append(found, key)
			vals[key] = v
		}
	}
	if len(found) > 1 {
		return Conn{}, fmt.Errorf("dbadminer: set only one of POSTGRES_URL, POSTGRESQL_URL, MYSQL_URL, MYSQL_DSN, SQLITE_URL (got %s)", strings.Join(found, ", "))
	}
	if len(found) == 1 {
		key := found[0]
		if key == "MYSQL_DSN" && !strings.Contains(vals[key], "://") && !strings.HasPrefix(vals[key], "file:") {
			return Conn{Driver: "mysql", DSN: vals[key], Source: key}, nil
		}
		return fromURL(vals[key], readOnly, key)
	}

	if v, err := get("PGDATABASE"); err != nil {
		return Conn{}, err
	} else if v != "" {
		return fromLibpq(get, v)
	}
	if v, err := get("POSTGRES_DB"); err != nil {
		return Conn{}, err
	} else if v != "" {
		return fromDockerPostgres(get, v)
	}
	if v, err := get("MYSQL_DATABASE"); err != nil {
		return Conn{}, err
	} else if v != "" {
		return fromMySQLEnv(get, v)
	}
	dbName, err := first(get, "DB_NAME", "DB_DATABASE")
	if err != nil {
		return Conn{}, err
	}
	if dbName != "" {
		driverName, err := first(get, "DB_CONNECTION", "DB_DRIVER")
		if err != nil {
			return Conn{}, err
		}
		if driverName == "" {
			return Conn{}, errors.New("dbadminer: DB_NAME or DB_DATABASE is set; set DB_CONNECTION or DB_DRIVER too")
		}
		source := "DB_NAME"
		if v, _ := get("DB_NAME"); v == "" {
			source = "DB_DATABASE"
		}
		return fromGeneric(get, driverName, dbName, source, readOnly)
	}
	path, err := first(get, "SQLITE_PATH", "SQLITE_DATABASE")
	if err != nil {
		return Conn{}, err
	}
	if path != "" {
		source := "SQLITE_PATH"
		if v, _ := get("SQLITE_PATH"); v == "" {
			source = "SQLITE_DATABASE"
		}
		dsn, err := sqliteDSN(path, readOnly)
		if err != nil {
			return Conn{}, err
		}
		return Conn{Driver: "sqlite", DSN: dsn, Source: source}, nil
	}
	return Conn{}, fmt.Errorf("%w; set one of DATABASE_URL, DATABASE_PRIVATE_URL, POSTGRES_URL, POSTGRESQL_URL, MYSQL_URL, MYSQL_DSN, SQLITE_URL, PGDATABASE, POSTGRES_DB, MYSQL_DATABASE, DB_NAME, DB_DATABASE, SQLITE_PATH, SQLITE_DATABASE", ErrNoDatabase)
}

func cleanValue(v string) (string, error) {
	v = strings.TrimSpace(v)
	if strings.ContainsAny(v, "\r\n\x00") {
		return "", errors.New("dbadminer: database value contains a control character")
	}
	return v, nil
}

func first(get func(string) (string, error), keys ...string) (string, error) {
	for _, key := range keys {
		v, err := get(key)
		if err != nil {
			return "", err
		}
		if v != "" {
			return v, nil
		}
	}
	return "", nil
}

func classifyRaw(raw string, readOnly bool) (Conn, error) {
	if strings.Contains(raw, "://") || strings.HasPrefix(raw, "file:") {
		return fromURL(raw, readOnly, "--dsn")
	}
	lower := strings.ToLower(raw)
	switch {
	case strings.Contains(lower, "@tcp(") || strings.HasPrefix(lower, "tcp("):
		return Conn{Driver: "mysql", DSN: raw}, nil
	case strings.Contains(lower, "dbname=") || strings.HasPrefix(lower, "host=") || strings.Contains(lower, "user="):
		return Conn{Driver: "pgx", DSN: raw}, nil
	case strings.HasPrefix(raw, "/") || strings.HasPrefix(raw, "./") || strings.HasPrefix(raw, "../") || strings.HasSuffix(lower, ".db") || strings.HasSuffix(lower, ".sqlite") || strings.HasSuffix(lower, ".sqlite3") || raw == ":memory:":
		dsn, err := sqliteDSN(raw, readOnly)
		if err != nil {
			return Conn{}, err
		}
		return Conn{Driver: "sqlite", DSN: dsn}, nil
	default:
		return Conn{}, errors.New("dbadminer: cannot tell which database driver to use for the given DSN")
	}
}

func fromURL(raw string, readOnly bool, source string) (Conn, error) {
	if strings.HasPrefix(raw, "file:") {
		dsn, err := sqliteDSN(raw, readOnly)
		if err != nil {
			return Conn{}, err
		}
		return Conn{Driver: "sqlite", DSN: dsn, Source: source}, nil
	}
	u, err := url.Parse(raw)
	if err != nil {
		return Conn{}, fmt.Errorf("dbadminer: parse database URL: %w", err)
	}
	switch strings.ToLower(u.Scheme) {
	case "postgres", "postgresql":
		return Conn{Driver: "pgx", DSN: raw, Source: source}, nil
	case "mysql":
		dsn, err := mysqlDSNFromURL(u)
		if err != nil {
			return Conn{}, err
		}
		return Conn{Driver: "mysql", DSN: dsn, Source: source}, nil
	case "sqlite", "sqlite3":
		dsn, err := sqliteDSN(raw, readOnly)
		if err != nil {
			return Conn{}, err
		}
		return Conn{Driver: "sqlite", DSN: dsn, Source: source}, nil
	case "":
		dsn, err := sqliteDSN(raw, readOnly)
		if err != nil {
			return Conn{}, err
		}
		return Conn{Driver: "sqlite", DSN: dsn, Source: source}, nil
	default:
		return Conn{}, fmt.Errorf("dbadminer: unsupported database URL scheme %q", u.Scheme)
	}
}

func mysqlDSNFromURL(u *url.URL) (string, error) {
	user := ""
	pass := ""
	if u.User != nil {
		user = u.User.Username()
		pass, _ = u.User.Password()
	}
	host := u.Hostname()
	port := u.Port()
	dbName := strings.TrimPrefix(u.Path, "/")
	return formatMySQLDSN(user, pass, host, port, dbName, u.Query())
}

func formatMySQLDSN(user, pass, host, port, dbName string, params url.Values) (string, error) {
	if strings.ContainsAny(user, ":@/") || strings.ContainsAny(pass, "@/") {
		return "", errors.New("dbadminer: MySQL user or password contains '@' or '/'; pass a driver DSN via --dsn or MYSQL_DSN")
	}
	if host == "" {
		host = "localhost"
	}
	if port == "" {
		port = "3306"
	}
	if params == nil {
		params = url.Values{}
	} else {
		params = cloneValues(params)
	}
	if params.Get("parseTime") == "" {
		params.Set("parseTime", "true")
	}
	var b strings.Builder
	if user != "" || pass != "" {
		b.WriteString(user)
		if pass != "" {
			b.WriteByte(':')
			b.WriteString(pass)
		}
		b.WriteByte('@')
	}
	b.WriteString("tcp(")
	b.WriteString(net.JoinHostPort(host, port))
	b.WriteString(")/")
	b.WriteString(dbName)
	if len(params) > 0 {
		b.WriteByte('?')
		b.WriteString(params.Encode())
	}
	return b.String(), nil
}

func cloneValues(v url.Values) url.Values {
	out := url.Values{}
	for key, vals := range v {
		out[key] = append([]string(nil), vals...)
	}
	return out
}

func fromLibpq(get func(string) (string, error), dbName string) (Conn, error) {
	host, err := must(get, "PGHOST")
	if err != nil {
		return Conn{}, err
	}
	port, err := must(get, "PGPORT")
	if err != nil {
		return Conn{}, err
	}
	user, err := must(get, "PGUSER")
	if err != nil {
		return Conn{}, err
	}
	pass, err := must(get, "PGPASSWORD")
	if err != nil {
		return Conn{}, err
	}
	ssl, err := must(get, "PGSSLMODE")
	if err != nil {
		return Conn{}, err
	}
	return Conn{
		Driver: "pgx",
		DSN:    postgresURL(user, pass, host, port, dbName, ssl),
		Source: "PGDATABASE",
	}, nil
}

func fromDockerPostgres(get func(string) (string, error), dbName string) (Conn, error) {
	host, err := must(get, "POSTGRES_HOST")
	if err != nil {
		return Conn{}, err
	}
	port, err := must(get, "POSTGRES_PORT")
	if err != nil {
		return Conn{}, err
	}
	user, err := must(get, "POSTGRES_USER")
	if err != nil {
		return Conn{}, err
	}
	pass, err := must(get, "POSTGRES_PASSWORD")
	if err != nil {
		return Conn{}, err
	}
	return Conn{
		Driver: "pgx",
		DSN:    postgresURL(user, pass, host, port, dbName, ""),
		Source: "POSTGRES_DB",
	}, nil
}

func fromMySQLEnv(get func(string) (string, error), dbName string) (Conn, error) {
	host, err := must(get, "MYSQL_HOST")
	if err != nil {
		return Conn{}, err
	}
	port, err := first(get, "MYSQL_PORT", "MYSQL_TCP_PORT")
	if err != nil {
		return Conn{}, err
	}
	user, err := must(get, "MYSQL_USER")
	if err != nil {
		return Conn{}, err
	}
	pass, err := first(get, "MYSQL_PASSWORD", "MYSQL_PWD")
	if err != nil {
		return Conn{}, err
	}
	dsn, err := formatMySQLDSN(user, pass, host, port, dbName, nil)
	if err != nil {
		return Conn{}, err
	}
	return Conn{Driver: "mysql", DSN: dsn, Source: "MYSQL_DATABASE"}, nil
}

func fromGeneric(get func(string) (string, error), driverName, dbName, source string, readOnly bool) (Conn, error) {
	kind, err := normalizeDriverName(driverName)
	if err != nil {
		return Conn{}, err
	}
	host, err := must(get, "DB_HOST")
	if err != nil {
		return Conn{}, err
	}
	port, err := must(get, "DB_PORT")
	if err != nil {
		return Conn{}, err
	}
	user, err := first(get, "DB_USER", "DB_USERNAME")
	if err != nil {
		return Conn{}, err
	}
	pass, err := must(get, "DB_PASSWORD")
	if err != nil {
		return Conn{}, err
	}
	switch kind {
	case "pgx":
		return Conn{Driver: "pgx", DSN: postgresURL(user, pass, host, port, dbName, ""), Source: source}, nil
	case "mysql":
		dsn, err := formatMySQLDSN(user, pass, host, port, dbName, nil)
		if err != nil {
			return Conn{}, err
		}
		return Conn{Driver: "mysql", DSN: dsn, Source: source}, nil
	case "sqlite":
		dsn, err := sqliteDSN(dbName, readOnly)
		if err != nil {
			return Conn{}, err
		}
		return Conn{Driver: "sqlite", DSN: dsn, Source: source}, nil
	default:
		return Conn{}, err
	}
}

func must(get func(string) (string, error), key string) (string, error) {
	return get(key)
}

func normalizeDriverName(name string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "postgres", "postgresql", "pgsql", "pgx":
		return "pgx", nil
	case "mysql", "mariadb":
		return "mysql", nil
	case "sqlite", "sqlite3":
		return "sqlite", nil
	default:
		return "", fmt.Errorf("dbadminer: unsupported DB_CONNECTION %q", name)
	}
}

func postgresURL(user, pass, host, port, dbName, sslmode string) string {
	if host == "" {
		host = "localhost"
	}
	if port == "" {
		port = "5432"
	}
	u := url.URL{
		Scheme: "postgres",
		Host:   net.JoinHostPort(host, port),
		Path:   "/" + dbName,
	}
	if user != "" {
		if pass != "" {
			u.User = url.UserPassword(user, pass)
		} else {
			u.User = url.User(user)
		}
	}
	if sslmode != "" {
		q := url.Values{}
		q.Set("sslmode", sslmode)
		u.RawQuery = q.Encode()
	}
	return u.String()
}

func sqliteDSN(raw string, readOnly bool) (string, error) {
	var dsn string
	switch {
	case strings.HasPrefix(raw, "sqlite://") || strings.HasPrefix(raw, "sqlite3://"):
		u, err := url.Parse(raw)
		if err != nil {
			return "", fmt.Errorf("dbadminer: parse sqlite URL: %w", err)
		}
		path := u.Path
		if path == "" {
			path = u.Opaque
		}
		file := url.URL{Scheme: "file", Path: path}
		if u.RawQuery != "" {
			file.RawQuery = u.RawQuery
		}
		dsn = file.String()
	case strings.HasPrefix(raw, "file:"):
		dsn = raw
	case raw == ":memory:":
		dsn = raw
	default:
		file := url.URL{Scheme: "file", Path: raw}
		dsn = file.String()
	}
	if readOnly {
		dsn = forceSQLiteReadOnly(dsn)
	}
	return dsn, nil
}

func forceSQLiteReadOnly(dsn string) string {
	if dsn == ":memory:" || strings.Contains(dsn, "mode=memory") {
		return dsn
	}
	if strings.HasPrefix(dsn, "file:") {
		u, err := url.Parse(dsn)
		if err != nil {
			return dsn
		}
		q := u.Query()
		if strings.Contains(q.Get("mode"), "memory") {
			return dsn
		}
		q.Set("mode", "ro")
		u.RawQuery = q.Encode()
		return u.String()
	}
	return dsn + "?mode=ro"
}
