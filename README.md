# DBAdminer

DBAdminer is a read-only database explorer for Go. Embed it next to a `*sql.DB` you already have, or start it when `DATABASE_URL` (or the usual Postgres and MySQL variables) is already set.

The root module imports only the standard library. Import `github.com/dbadminer/dbadminer/dbopen` when DBAdminer should open the database itself. That package pulls in PostgreSQL, MySQL, and SQLite drivers. The module requires Go 1.26 or newer, which is the minimum those drivers support.

## Run from existing variables

```go
package main

import (
	"context"
	"log"

	"github.com/dbadminer/dbadminer/dbopen"
)

func main() {
	if err := dbopen.Run(context.Background(), nil); err != nil {
		log.Fatal(err)
	}
}
```

`dbopen.Open` and `dbopen.Run` use the first match:

1. `--dsn`
2. `DATABASE_URL`, otherwise `DATABASE_PRIVATE_URL`
3. One of `POSTGRES_URL`, `POSTGRESQL_URL`, `MYSQL_URL`, `MYSQL_DSN`, `SQLITE_URL`
4. libpq `PG*` when `PGDATABASE` is set (`PGHOST`, `PGPORT`, `PGUSER`, `PGPASSWORD`, `PGSSLMODE`)
5. Docker Postgres `POSTGRES_*` when `POSTGRES_DB` is set
6. `MYSQL_*` when `MYSQL_DATABASE` is set (`MYSQL_HOST`, `MYSQL_PORT` or `MYSQL_TCP_PORT`, `MYSQL_USER`, `MYSQL_PASSWORD` or `MYSQL_PWD`)
7. Generic `DB_*` when `DB_NAME` or `DB_DATABASE` is set (`DB_CONNECTION` or `DB_DRIVER`, `DB_HOST`, `DB_PORT`, `DB_USER` or `DB_USERNAME`, `DB_PASSWORD`)
8. `SQLITE_PATH`, otherwise `SQLITE_DATABASE`

SQLite opens with `mode=ro` while the explorer is read-only. A missing host or port becomes `localhost` and `5432` or `3306`.

## Embed an existing pool

```go
cfg := dbadminer.DefaultConfig()
ex, err := dbadminer.New(db, cfg)
if err != nil {
	log.Fatal(err)
}
if err := ex.ListenAndServe(ctx); err != nil {
	log.Fatal(err)
}
```

`DefaultConfig` is read-only and listens on `127.0.0.1:8080`. `New` keeps a zero `Config` read-only too. Choose the mode with `Access`:

```go
cfg := dbadminer.DefaultConfig()
cfg.Access = dbadminer.AccessFull // or dbadminer.AccessReadOnly
```

Setting `ReadOnly` to false on that configured value still selects full access. `ListenAndServe` refuses any address that is not loopback.

Mount `ex.Handler()` on your own server if you want. Requests whose `Host` header is not loopback are rejected, and a present `Origin` must match that host.

Flags accepted by `Flags` and `Run`: `--addr`, `--access`, `--readonly`, `--user`, `--password`, `--max-rows`, `--query-timeout`, `--max-body`, `--max-response`. `--access` is `readonly` or `full`. `--readonly=false` selects full access as well. Set only one of those two flags. `dbopen.Run` also accepts `--dsn`, and `DBADMINER_ACCESS=readonly` or `DBADMINER_ACCESS=full` when neither flag is present. `--user` and `--password` are basic auth for the page. They are never taken from the database password.

## HTTP API

- `GET /api/info`
- `GET /api/tables`
- `GET /api/tables/{schema}/{table}`
- `GET /api/tables/{schema}/{table}/rows?limit&offset&order&dir&count=1`
- `POST /api/query` with `{"sql":"select 1"}` and `Content-Type: application/json`

`count=1` opts into `COUNT(*)`. The page size is capped by `MaxRows`.

## Safety

Read-only mode is best effort. Use a database user that only has `SELECT`, and open SQLite with `mode=ro`. DBAdminer also rejects non-read statements, uses a read-only transaction on PostgreSQL, MySQL, and MariaDB, and quotes identifiers only after a catalog match. Details are in [SECURITY.md](SECURITY.md).

## Tests

```bash
go test ./...
```

`compose.yaml` starts PostgreSQL 16, MySQL 8, and MariaDB 11 for the integration tag. See [CONTRIBUTING.md](CONTRIBUTING.md).
