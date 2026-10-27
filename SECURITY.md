# Security

DBAdminer is a local, read-only window onto a database you already have. It is not a sandbox and it is not safe to expose on a public network.

## Reporting

Please report vulnerabilities privately to the repository maintainers. Do not open a public issue for an unfixed security bug.

## What the library does

- Serves only on a loopback address, and rejects requests whose `Host` is not loopback. A request `Origin`, when present, must match that host.
- Defaults to read-only access. `Access` (`readonly` or `full`), `--access`, `--readonly`, and `DBADMINER_ACCESS` select the other mode. In read-only mode, write statements are rejected before they reach the driver, and PostgreSQL, MySQL, and MariaDB also run explorer statements inside a read-only transaction. Full access uses the database user's privileges.
- Accepts schema, table, and order names only when they match a live catalog entry, then quotes them. Query values are bound parameters.
- Compares basic-auth credentials in constant time and slows repeated failures.
- Sets `Content-Security-Policy: default-src 'self'`, `X-Content-Type-Options: nosniff`, and `Referrer-Policy: no-referrer`.
- Caps request bodies, row counts, query time, and encoded response size.
- Renders values in the UI with `textContent`.

## What you should do

Read-only mode is best effort. Keyword checks and a read-only transaction do not replace a database role that cannot write. Create a user with `SELECT` only and point DBAdminer at that user.

For SQLite, open the file with `mode=ro` (DBAdminer does this when it connects from environment variables in read-only mode). A pooled `PRAGMA query_only` is intentionally not used, because it would stick to a connection and affect later work.

Database passwords from `DATABASE_URL`, `PGPASSWORD`, `MYSQL_PASSWORD`, and `DB_PASSWORD` are used only to connect. They are not the explorer's basic-auth password. Set `--user` and `--password` if the page itself should require a login.

Dependency review runs in CI with `govulncheck` and Dependabot.
