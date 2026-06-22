# analytics-service

Per-day rollups of QuickBite order/payment/delivery activity.

- Consumes `order.events` from RabbitMQ (bindings: `order.#, payment.#`).
- Upserts day-grained aggregates into MongoDB.
- Serves read-only HTTP endpoints under `/api/v1/analytics/...`.

The service maintains restaurant, branch, product, and platform daily
aggregates. It consumes placement, payment-completion, rejection, delivery,
and RBAC cache-invalidation events.

## Prerequisites

- Go 1.21+ (developed with 1.26).
- Running MongoDB on `mongodb://localhost:27017`.
- Running RabbitMQ on `amqp://guest:guest@localhost:5672/`.

You do **not** need core-service or order-service running for the demo —
the `play/` binaries stand in.

## First-time setup

```bash
cp .env.example .env
# (review .env — defaults work for a local mongo + rabbit + no auth)
go mod download
```

## Three-terminal demo

### Terminal 1 — API server

```bash
go run ./cmd/api
```

Expected first lines (JSON, one per line):

```json
{"time":"...","level":"INFO","msg":"mongo connected","database":"analytics"}
{"time":"...","level":"INFO","msg":"rabbit connected"}
{"time":"...","level":"INFO","msg":"mongo indexes ensured"}
{"time":"...","level":"INFO","msg":"event consumer started"}
{"time":"...","level":"INFO","msg":"http listening","addr":":5000"}
```

### Terminal 2 — mock-core (so auth/RBAC works)

```bash
go run ./play/mock-core
```

Returns `analytics:read` for every role.

### Terminal 3 — exercise the slice

```bash
# 1. health
curl -s http://localhost:5000/health

# 2. publish an order.placed event (restaurant 42, total 2500 minor)
go run ./play/publish-test -restaurant 42 -total 2500

# 3. verify the aggregate doc was written
go run ./play/check-mongo -restaurant 42

# 4. mint a JWT and query the API
export TOK=$(go run ./play/mint-jwt -secret devsecret_replace_me \
              -role restaurant_user -uid 1)

curl -s "http://localhost:5000/api/v1/analytics/restaurants/42/days?from=2026-01-01&to=2026-12-31" \
  -H "Authorization: Bearer $TOK"
```

You should see one row, with `ordersCount: 1`, `revenueMinor: 2500`,
`avgOrderMinor: 2500`.

### Dedupe check

Re-publish the same `eventId` — the aggregate row must NOT change:

```bash
EVT=$(uuidgen)
go run ./play/publish-test -event-id $EVT -restaurant 42 -total 500
go run ./play/publish-test -event-id $EVT -restaurant 42 -total 500
go run ./play/check-mongo -restaurant 42
# orders_count incremented by exactly 1, not 2
```

### Auth checks

```bash
# no token → 401
curl -s -o /dev/null -w "%{http_code}\n" \
  "http://localhost:5000/api/v1/analytics/restaurants/42/days?from=2026-01-01&to=2026-12-31"

# garbage cookie → 401
curl -s -o /dev/null -w "%{http_code}\n" \
  --cookie "access_token=garbage" \
  "http://localhost:5000/api/v1/analytics/restaurants/42/days?from=2026-01-01&to=2026-12-31"

# from > to → 400 ANALYTICS_INVALID_DATE_RANGE
curl -s -H "Authorization: Bearer $TOK" \
  "http://localhost:5000/api/v1/analytics/restaurants/42/days?from=2026-12-31&to=2026-01-01"
```

## Repository layout

See `docs/folder-structure.md`. Short version:

```
cmd/api/main.go    # 10 lines — hands off to lib/boot.Run()
internal/backfill/ # testable implementation behind cmd/backfill-aggs
pkg/               # framework-free providers (mongo, amqp, http)
lib/               # app-aware glue (config, auth, errors, fx wiring)
app/analytics/     # the one business module
test/unit/         # black-box unit tests mirroring source package paths
test/integration/  # MongoDB/RabbitMQ integration and event E2E tests
play/              # dev helpers (gitignored)
docs/              # architecture + teaching docs
```

## Tests

```bash
go test ./test/unit/...
go test -race ./test/unit/...
go test -tags=integration -count=1 -v ./test/integration/...
go test ./...
go vet ./...
go build ./...
```

Unit tests are centralized under `test/unit/` and mirror production package
paths. They exercise exported behavior without exposing production internals
for testing.

The integration suite uses a dedicated MongoDB database and unique,
automatically cleaned RabbitMQ topology. It exercises the production router,
every analytics endpoint, aggregate repositories, indexes, dedupe, complete
restaurant/branch/product/platform event fan-out, RBAC invalidation, and DLQ
behavior.

See `test/integration/README.md` for prerequisites and safety constraints.
