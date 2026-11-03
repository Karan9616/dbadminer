# Demo stack

Small Go app with two commands:

- `seed` — create sample tables and rows in Postgres and MySQL
- `serve` — hello-world HTTP page that reads those databases

Compose starts the databases, runs seed, then runs the app and full-access DBAdminer.

```bash
# from the repo root
docker compose -f examples/demo/compose.yaml up --build
```

| URL | What |
| --- | --- |
| http://127.0.0.1:8090 | Demo hello-world app |
| http://127.0.0.1:8080 | DBAdminer (`shop` = MySQL, `app` = Postgres) |

Each database gets `customers`, `products`, `orders`, `order_items`, and a `customer_order_totals` view.

Re-seed:

```bash
docker compose -f examples/demo/compose.yaml run --rm seed
```

Locally (databases already on localhost):

```bash
cd examples/demo
export DEMO_DATABASES=shop,app
export SHOP_DATABASE_URL='mysql://127.0.0.1:3306/shop'
export SHOP_DB_USER=dbadminer
export SHOP_DB_PASSWORD=dbadminer
export APP_DATABASE_URL='postgres://127.0.0.1:5432/app?sslmode=disable'
export APP_DB_USER=dbadminer
export APP_DB_PASSWORD=dbadminer
go run . seed
go run . serve   # http://127.0.0.1:8090
```
