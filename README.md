# DBAdminer

![DBAdminer](docs/logo.svg)

![DBAdminer browsing a table](docs/screenshot.png)

DBAdminer is a database explorer for Go. It starts read-only. Download a binary and open the UI, embed it next to a `*sql.DB` you already have, or pull the Docker image. The library module imports only the standard library. `cmd/dbadminer` is its own module, so importing the library does not pull in database drivers. Handler tests use SQLite. The module requires Go 1.26 or newer.

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

These fields override the chosen Postgres or MySQL URL, including `--dsn` and `DATABASE_URL`. A flag wins over the matching variable. SQLite ignores the host, port, user, password, and database name.

| Field | Flag | Variable |
| --- | --- | --- |
| Host | `--db-host` | `DB_HOST` |
| Port | `--db-port` | `DB_PORT` |
| User | `--db-user` | `DB_USER`, otherwise `DB_USERNAME` |
| Password | `--db-password` | `DB_PASSWORD` |
| Database | `--db-name` | `DB_NAME`, otherwise `DB_DATABASE` |
| Schema | `--schema` | `DB_SCHEMA` |

Put a password that contains `@` in `DB_PASSWORD` or `--db-password`. It is percent-encoded into the Postgres URL, so `p@ss` does not have to be written as `p%40ss`.

```bash
dbadminer \
  --dsn 'postgres://postgres:5432/app?sslmode=disable' \
  --db-user ada \
  --db-password 'p@ss' \
  --schema public
```

`--schema` limits the table list to that schema. On PostgreSQL it also sets `search_path`, so unqualified SQL uses it. The name must be a letter or underscore followed by letters, digits, or underscores. `--user` and `--password` are still basic auth for the page, not the database.

## Read-only and full access

Read-only mode allows one `SELECT`, `WITH`, `EXPLAIN`, `VALUES`, `TABLE`, or `SHOW`. `INSERT`, `UPDATE`, `DELETE`, `CREATE`, `ALTER`, and `DROP` are rejected before they reach the database. PostgreSQL, MySQL, and MariaDB also run those statements in a read-only transaction. SQLite opens with `mode=ro`.

Full access runs one statement with the database user's privileges. That includes DML and DDL. One switch covers both:

```bash
dbadminer --access=full
dbadminer --readonly=false
DBADMINER_ACCESS=full
```

`--access` takes `readonly` or `full`. The value you type is `readonly`; the README calls that mode read-only. Set only one of `--access` and `--readonly`. Full access still accepts one statement at a time.

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

Flags accepted by `Flags` and `Run`: `--addr`, `--access`, `--readonly`, `--user`, `--password`, `--max-rows`, `--query-timeout`, `--max-body`, `--max-response`. Access is described above. `cmd/dbadminer` also accepts `--dsn`, `--db-host`, `--db-port`, `--db-user`, `--db-password`, `--db-name`, and `--schema`. `--user` and `--password` are basic auth for the page. They are never taken from the database password. `Config.Schema` hides every other schema for a pool you already opened.

`examples/http`, `examples/chi`, and `examples/cobra` show how to mount the handler.

## Run the Docker image

A tag named `v*` publishes `docker.io/<Docker Hub user>/dbadminer` for that version and for `latest`. The Docker Hub user comes from the `DOCKERHUB_USERNAME` Actions secret. You can also build it locally, publish the port only on the host loopback, then open `http://127.0.0.1:8080`:

```bash
docker build -t dbadminer .
docker run --rm -p 127.0.0.1:8080:8080 \
  -e DATABASE_URL='postgres://postgres:5432/app?sslmode=disable' \
  -e DB_USER=ada \
  -e DB_PASSWORD='p@ss' \
  -e DB_SCHEMA=public \
  dbadminer
```

The image starts read-only. Add `-e DBADMINER_ACCESS=full` when the database user should be allowed to run DML and DDL.

## HTTP API

- `GET /api/info`
- `GET /api/tables`
- `GET /api/tables/{schema}/{table}`
- `GET /api/tables/{schema}/{table}/rows?limit&offset&order&dir&count=1`
- `POST /api/query` with `{"sql":"select 1"}` and `Content-Type: application/json`

`count=1` opts into `COUNT(*)`. The page size is capped by `MaxRows`.

## Safety

Read-only mode is best effort. Use a database user that only has `SELECT`, and open SQLite with `mode=ro`. DBAdminer also rejects non-read statements, uses a read-only transaction on PostgreSQL, MySQL, and MariaDB, and quotes identifiers only after a catalog match. Full access adds no privileges of its own: the statement runs as the database user, one statement at a time. Details are in [SECURITY.md](SECURITY.md).

## Tests

```bash
make check
```

`make check` formats, vets, lints, and tests the module. `compose.yaml` starts PostgreSQL 16, MySQL 8, and MariaDB 11 for the integration tag. `make integration` runs those tests. See [CONTRIBUTING.md](CONTRIBUTING.md).
