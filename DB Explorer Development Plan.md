# DB Explorer: Development Plan

*Draft v0.1 · October 25, 2026 · companion to the Project Plan*

## 1. Current state

A first skeleton exists and has **not been compiled or tested yet**. Treat it as a design draft.

| File | Purpose |
| --- | --- |
| `explorer.go` | `Config`, `New`, `Handler`, `ListenAndServe`, `Flags`, `Run`, basic auth, loopback check |
| `dialect.go` | `Dialect` interface, Postgres / MySQL / SQLite implementations, dialect detection, metadata queries |
| `api.go` | HTTP routes, JSON handlers, row scanning, read-only guard |
| `ui/index.html` | Embedded vanilla JS UI: table list, data grid, SQL console |

**First task:** run `go vet ./...` and fix compile errors.

## 2. Target architecture

### Repository layout

```
/                      library module (stdlib only)
  explorer.go  api.go  dialect.go
  ui/                  embedded static UI
  internal/            helpers not part of the public API
  examples/            runnable examples (net/http, chi, cobra)
/cmd/<name>/           standalone CLI, its OWN go.mod
  main.go              DSN parsing, driver imports
/npm/                  npm wrapper packages and publish scripts
/.github/workflows/    CI and release
/.goreleaser.yaml
```

The CLI lives in a separate module so people who import the library do not inherit driver dependencies in their `go.mod`.

### Public API (keep it small, stabilize at v1.0)

- `New(db *sql.DB, cfg Config) (*Explorer, error)`
- `(*Explorer).Handler() http.Handler`
- `(*Explorer).ListenAndServe(ctx) error`
- `Flags(*Config) *flag.FlagSet` and `Run(ctx, db, args)`
- `Dialect` interface for custom databases

Everything else stays unexported or under `internal/` until it has proven itself.

### HTTP API

| Method and path | Purpose | Phase |
| --- | --- | --- |
| `GET /api/info` | Dialect, read-only flag, version | 1 |
| `GET /api/tables` | List tables and views | 1 |
| `GET /api/tables/{schema}/{table}` | Column metadata | 1 |
| `GET /api/tables/{schema}/{table}/rows` | Paginated, sortable rows | 1 |
| `POST /api/query` | Run SQL | 1 |
| `GET /api/tables/{schema}/{table}/schema` | Indexes, foreign keys, DDL | 3 |
| `POST/PATCH/DELETE .../rows` | Insert, update, delete by primary key | 3 |
| \`GET .../export?format=csv | json\` | Export |
| `GET /api/connections` | Multiple named connections | 3 |

## 3. Phases

### Phase 0: Foundations (week 1)

- [ ] Decide final name; create GitHub org and repo; set module path
- [ ] Run `go vet ./...` and fix compile errors
- [ ] Add LICENSE (MIT or Apache-2.0), SECURITY.md, CONTRIBUTING.md
- [ ] Add `golangci-lint` config and a basic GitHub Actions workflow (vet, lint, test)
- [ ] Pin the minimum Go version (currently 1.22, required by routing patterns and `FileServerFS`)

**Exit criteria:** CI is green on an empty test suite.

### Phase 1: Reliable core (weeks 2-3)

**Tests**

- [ ] SQLite in-memory tests for every handler (fast, no services)
- [ ] Integration tests against real PostgreSQL and MySQL using testcontainers-go or CI service containers
- [ ] Table-driven tests for `isReadOnlySQL`, `returnsRows`, `isLoopbackAddr`
- [ ] Security tests: unknown table, unknown order column, quote characters in identifiers, stacked statements in read-only mode, wrong content type

**Correctness**

- [ ] Verify metadata queries on PostgreSQL 14-17, MySQL 8, MariaDB 10/11, SQLite 3.35+
- [ ] Handle views, partitioned tables, composite primary keys, tables without primary keys
- [ ] Serialize `int64` values beyond 2^53 as strings so the browser does not lose precision
- [ ] Handle NaN/Inf floats, JSON/JSONB, arrays, UUID, bytea/BLOB, and time zones in `normalize`
- [ ] Pass `context` so cancelled requests stop the query

**Exit criteria:** all three databases pass the same shared test suite; zero known data-corruption or injection issues.

### Phase 2: v0.1 public release (week 4)

- [ ] README with a 5-line integration example, screenshot or GIF, and a safety section
- [ ] `examples/` for plain `net/http`, chi or gin mounting, and cobra
- [ ] Godoc comments on every exported symbol
- [ ] Tag `v0.1.0`, announce on r/golang and Go Forum

**Exit criteria:** a new user can add the explorer to an existing app in under 5 minutes using only the README.

### Phase 3: v1.0 features (weeks 5-12, can run in parallel with Phase 4)

**Priority order**

1. Schema view: columns, indexes, foreign keys, `CREATE TABLE` DDL
2. CSV and JSON export of table and query results
3. Query history stored in browser storage
4. Row editing, insert and delete for tables with a primary key, behind `ReadOnly: false`, with a confirmation step in the UI
5. Multiple named connections
6. Filters on the data grid (column, operator, value) built from whitelisted columns and bound parameters
7. Keyboard shortcuts, dark mode polish, column resizing

**UI decision:** stay with vanilla JS until editing is added. If the UI grows past roughly 1,500 lines, move to a small build-time framework (for example Preact with Vite) and commit the built output so Go users never need Node.

**Exit criteria:** feature set in the Project Plan v1.0 list is complete and tested.

### Phase 4: Standalone CLI and binaries (weeks 5-7)

- [ ] `cmd/<name>/go.mod` with `pgx/v5/stdlib`, `go-sql-driver/mysql`, and `modernc.org/sqlite`
- [ ] DSN parsing: `postgres://`, `mysql://`, `sqlite:///path/to.db`, or a bare file path for SQLite
- [ ] Flags and environment variables (`--addr`, `--readonly`, `--user`, `--password`, `DATABASE_URL`)
- [ ] Build with `CGO_ENABLED=0` for linux/darwin/windows on amd64 and arm64
- [ ] `.goreleaser.yaml`: archives, checksums, SBOM, version injected via ldflags
- [ ] GitHub Actions release workflow triggered by tags
- [x] Optional: Homebrew tap, Scoop bucket, Docker image (`dbadminer/dbadminer` on Docker Hub)
- [ ] Sign macOS binaries or document the Gatekeeper workaround

**Exit criteria:** `curl` or `brew install` followed by `<name> postgres://...` opens the UI on all three operating systems.

### Phase 5: npm distribution (week 7)

**Approach:** platform packages plus a main package, as esbuild does.

- [ ] Main package `@<scope>/cli` with a small JS launcher that resolves the right platform package and spawns the binary
- [ ] Platform packages `@<scope>/cli-linux-x64`, `-linux-arm64`, `-darwin-x64`, `-darwin-arm64`, `-win32-x64`, `-win32-arm64`, each containing one binary with `os` and `cpu` fields so npm installs only the matching one
- [ ] Main package lists platform packages as `optionalDependencies`
- [ ] Generate and publish from the GoReleaser output in CI, either with `goreleaser-npm-publisher` or a small custom script
- [ ] Use npm provenance (`--provenance`) and a scoped automation token
- [ ] Test matrix: `npx <name> --version` on Linux, macOS, Windows; with and without `--ignore-scripts`

**Fallback:** if a platform package is missing, print a clear error that points to the GitHub Releases page.

**Exit criteria:** `npx <name> sqlite:///app.db` works on a clean machine with no Go installed.

### Phase 6: Docs and launch (weeks 8-12)

- [ ] Docs site (GitHub Pages) with install, integration, configuration, security, and dialect guide
- [ ] Demo video or GIF; sample app with Docker Compose for all three databases
- [ ] Post on Hacker News, r/golang, and relevant newsletters
- [ ] Triage feedback; plan v1.1

## 4. Testing strategy

| Layer | Tool | Runs |
| --- | --- | --- |
| Unit tests (helpers, guards) | `go test` | Every push |
| Handler tests on SQLite in memory | `httptest` | Every push |
| PostgreSQL and MySQL integration | CI service containers or testcontainers | Every push (matrix of versions) |
| UI smoke tests | Playwright against the built binary | On release branches |
| Fuzzing for SQL guards and identifier quoting | `go test -fuzz` | Nightly |
| Race detector | `go test -race` | Every push |

Target: 80%+ coverage on library packages, and 100% of the security cases listed in Phase 1.

## 5. Security checklist

- [ ] No user input reaches SQL unless it is a bound parameter or matches a live schema name
- [ ] Basic auth uses constant-time comparison; consider rate limiting failed attempts
- [ ] Check the `Host` and `Origin` headers to defend against DNS rebinding on loopback
- [ ] Set security headers: `Content-Security-Policy`, `X-Content-Type-Options`, `Referrer-Policy`
- [ ] Cap request body size, row count, query time, and response size
- [ ] UI renders all data with `textContent`, never `innerHTML`
- [ ] Read-only mode documented as best effort; recommend a restricted DB user and `?mode=ro` for SQLite
- [ ] Optional audit log of executed statements
- [ ] Dependency scanning with `govulncheck` and Dependabot
- [ ] Review by someone other than the author before v1.0

## 6. Dialect extension guide

To add a database, implement `Dialect`: `Name`, `Quote`, `TablesQuery`, `ColumnsQuery`, `ColumnsArgs`, `Qualify`, `SupportsReadOnlyTx`. Planned additions to the interface for v1.0:

- `IndexesQuery` and `ForeignKeysQuery`
- `DDL(ctx, db, table)` for `CREATE TABLE` output
- `Paginate(limit, offset)` for databases that do not use `LIMIT/OFFSET` (such as MSSQL)
- `Placeholder(n)` for parameterized filters

## 7. Release and versioning

- Semantic versioning; stay on `v0.x` until the public API and HTTP API are stable
- Conventional commit messages and an auto-generated changelog
- Library and CLI tagged together at first; split tags only if release cadence diverges
- Deprecation policy: one minor version of warning before removal after v1.0

## 8. Technical risks

| Risk | Mitigation |
| --- | --- |
| Skeleton has never been compiled | First task in Phase 0 |
| Metadata queries differ across versions and forks (MariaDB, Cockroach, Aurora) | Version matrix in CI; document tested versions; allow dialect override |
| Read-only guard bypass | Layer defenses: keyword check, read-only transaction, restricted DB user in docs |
| Large tables slow down `COUNT(*)` | Make total count optional, or use estimated counts on PostgreSQL |
| Offset pagination is slow on deep pages | Add keyset pagination for tables with a primary key |
| Cross-origin or rebinding attacks on loopback | Host/Origin checks and JSON-only writes |
| npm platform package drift | Publish all packages from one workflow with the same version |

## 9. Backlog (unscheduled)

- ER diagram view
- Saved queries synced to a file
- Bearer token and OIDC authentication
- Prometheus metrics endpoint
- Plugin hooks to hide tables or mask columns (useful for PII)
- Wasm build for in-browser SQLite

## 10. Definition of done (per feature)

1. Works on PostgreSQL, MySQL and SQLite, or is clearly marked as dialect-specific
2. Unit and integration tests added
3. Security checklist items reviewed
4. Documented in README or docs site
5. Included in changelog
