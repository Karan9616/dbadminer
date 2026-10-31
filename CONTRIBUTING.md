# Contributing

Run the checks before opening a pull request:

```bash
make check
```

`make check` formats the tree, runs `go vet`, `golangci-lint`, the race detector, and `govulncheck`. Those tools are pinned in `tools/go.mod`.

Integration tests need a database. `compose.yaml` starts PostgreSQL 16, MySQL 8, and MariaDB 11:

```bash
docker compose up -d
make integration
```

`make integration` sets `DBADMINER_INTEGRATION=1` and points `DATABASE_URL` at the Compose Postgres service. Override `DATABASE_URL` with a `mysql://` URL for MySQL or MariaDB. The integration test creates `dbadminer_*` tables and drops them when it finishes.

Format Go files with `gofmt`. The library module must stay free of database drivers. Drivers belong in `cmd/dbadminer`, which has its own `go.mod`. Helpers that are not part of the public API belong in `internal/`.

## UI

The page in `ui/` is the build output of the Svelte app in `web/`. Go embeds that directory, so a checkout can be built without Node. Change the UI from `web/`, then rebuild and commit `ui/`.

```bash
cd web
npm ci
npm run dev
```

`npm run dev` serves the page with Vite and proxies `api` to `http://127.0.0.1:8080`. Start DBAdminer on that address first. `make ui-check` runs `svelte-check` and the production build. `make ui` only rebuilds `ui/`.
