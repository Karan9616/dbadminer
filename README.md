# DBAdminer

DBAdminer is a read-only database explorer for Go. Download a binary and open the UI, embed it next to a `*sql.DB` you already have, or pull the Docker image. The library module imports only the standard library. `cmd/dbadminer` is its own module, so importing the library does not pull in database drivers. Handler tests use SQLite. The module requires Go 1.26 or newer.

## Download a binary

GitHub Releases publishes `dbadminer` for Linux, macOS, and Windows on amd64 and arm64. Download the archive for your system, unpack it, and run:

```bash
dbadminer --dsn 'postgres://user:pass@127.0.0.1:5432/app?sslmode=disable'
```

Or leave `--dsn` empty and export `DATABASE_URL` (or one of the variables below). Then open `http://127.0.0.1:8080`. The binary listens on `127.0.0.1:8080`. `dbadminer --version` prints the release version.

From a checkout of this repository:

```bash
go run ./cmd/dbadminer
```

The command uses the first match:

1. `--dsn`
2. `DATABASE_URL`, otherwise `DATABASE_PRIVATE_URL`
3. One of `POSTGRES_URL`, `POSTGRESQL_URL`, `MYSQL_URL`, `MYSQL_DSN`, `SQLITE_URL`
4. libpq `PG*` when `PGDATABASE` is set (`PGHOST`, `PGPORT`, `PGUSER`, `PGPASSWORD`, `PGSSLMODE`)
5. Docker Postgres `POSTGRES_*` when `POSTGRES_DB` is set
6. `MYSQL_*` when `MYSQL_DATABASE` is set (`MYSQL_HOST`, `MYSQL_PORT` or `MYSQL_TCP_PORT`, `MYSQL_USER`, `MYSQL_PASSWORD` or `MYSQL_PWD`)
7. Generic `DB_*` when `DB_NAME` or `DB_DATABASE` is set (`DB_CONNECTION` or `DB_DRIVER`, `DB_HOST`, `DB_PORT`, `DB_USER` or `DB_USERNAME`, `DB_PASSWORD`)
8. `SQLITE_PATH`, otherwise `SQLITE_DATABASE`

SQLite opens with `mode=ro` while the explorer is read-only. A missing host or port becomes `localhost` and `5432` or `3306`.

## Use it as a library

Import `github.com/dbadminer/dbadminer` and pass the pool you already have. That import does not add database drivers to your module.

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

Setting `ReadOnly` to false on that configured value still selects full access. `ListenAndServe` accepts a loopback address, `0.0.0.0`, or `::`. A specific public address is refused.

Mount `ex.Handler()` on your own server if you want. Requests whose `Host` header is not loopback are rejected, and a present `Origin` must match that host.

Flags accepted by `Flags` and `Run`: `--addr`, `--access`, `--readonly`, `--user`, `--password`, `--max-rows`, `--query-timeout`, `--max-body`, `--max-response`. `--access` is `readonly` or `full`. `--readonly=false` selects full access as well. Set only one of those two flags. `cmd/dbadminer` also accepts `--dsn`, and `DBADMINER_ACCESS=readonly` or `DBADMINER_ACCESS=full` when neither flag is present. `--user` and `--password` are basic auth for the page. They are never taken from the database password.

`examples/http`, `examples/chi`, and `examples/cobra` show how to mount the handler.

## Run the Docker image

The image is not published yet. Build it locally, publish the port only on the host loopback, then open `http://127.0.0.1:8080`:

```bash
docker build -t dbadminer .
docker run --rm -p 127.0.0.1:8080:8080 \
  -e DATABASE_URL='postgres://user:pass@host.docker.internal:5432/app?sslmode=disable' \
  dbadminer
```

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
make check
```

`make check` formats, vets, lints, and tests the module. `compose.yaml` starts PostgreSQL 16, MySQL 8, and MariaDB 11 for the integration tag. `make integration` runs those tests. See [CONTRIBUTING.md](CONTRIBUTING.md).
