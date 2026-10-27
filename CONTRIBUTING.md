# Contributing

Run the unit tests before opening a pull request:

```bash
go test ./...
go vet ./...
```

Integration tests need a database and `DBADMINER_INTEGRATION=1`. `compose.yaml` starts PostgreSQL 16, MySQL 8, and MariaDB 11:

```bash
docker compose up -d
DBADMINER_INTEGRATION=1 \
  DATABASE_URL='postgres://dbadminer:dbadminer@127.0.0.1:5432/dbadminer?sslmode=disable' \
  go test -tags integration ./...
```

Use the same flag with a `mysql://` URL for MySQL or MariaDB. The integration test creates `dbadminer_*` tables and drops them when it finishes.

Format Go files with `gofmt`. The root package must stay free of database drivers; drivers belong in `dbopen`.
