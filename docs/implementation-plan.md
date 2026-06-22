# implementation-plan.md

Phases 1–6 are the **video slice**. Phases 7+ are **homework** (see
`plan.md` for the gap list and `ai-prompts.md` for prompt templates).

Each phase lists:
- **What to build** (with file paths)
- **Conventions to follow** (CLAUDE.md sections)
- **Acceptance check** (a concrete command + expected output)

---

## Phase 1 — Project skeleton + config + logger

**Build**

- `go.mod` (`go mod init github.com/zrafat80/quickbite/analytics-service`)
- `.env.example`, `.gitignore`
- `cmd/api/main.go` (10 lines, just calls `boot.Run()`)
- `pkg/httpclient/client.go`
- `lib/config/env.go`, `lib/logger/logger.go`
- `lib/appcontext/context.go`
- `lib/errors/{apperror.go, handler.go}`
- `lib/http/response.go`
- `lib/middleware/correlation.go`

**Conventions**

- CLAUDE.md §2 (stack), §3 (layering), §4 (naming).

**Acceptance**

```bash
go build ./...        # clean
go vet ./...          # clean
```

---

## Phase 2 — Mongo client + indexes + boot wiring

**Build**

- `pkg/mongo/client.go` (Connect / Disconnect, NO collection names).
- `app/analytics/repository/indexes.go` (EnsureIndexes — owns collection names).
- `app/analytics/entity/agg_restaurant_day.go`.
- `app/analytics/repository/agg_restaurant_day.repo.go` (upsert + range read).
- `lib/boot/boot.go` — initial fx tree with Mongo + index ensure hook.

**Conventions**

- CLAUDE.md §7 (db rules), §8 (DI: fx).

**Acceptance**

```bash
go run ./cmd/api    # logs "mongo connected", "mongo indexes ensured", "http listening"
curl -s http://localhost:5000/health
# {"success":true,"data":{"status":"ok"}}
```

In `mongosh`:

```js
use analytics
db.agg_restaurant_day.getIndexes()
// shows uq_restaurant_date (unique) + idx_date_restaurant
```

---

## Phase 3 — RabbitMQ wiring + consumer + dedupe

**Build**

- `pkg/messaging/{types.go, amqp.go}` — Broker interface + amqp091 impl.
- `lib/coreevents/{payloads.go, consumer.go}` — generic consumer.
- `app/analytics/repository/event_ids.repo.go` — unique-index dedupe.
- `lib/boot/boot.go` — add Rabbit provider, Consumer.Start fx hook.

**Conventions**

- CLAUDE.md §8 (Messaging), system-design.md (failure-mode table).

**Acceptance**

```bash
# Terminal 1
go run ./cmd/api
# Logs: "rabbit connected", "event consumer started"

# Terminal 2
go run ./play/publish-test -restaurant 42 -total 2500
go run ./play/check-mongo -restaurant 42
# orders_count: 1, revenue_sum: 2500
```

Republish the same event id → mongo is unchanged.

---

## Phase 4 — Order-service publisher (cross-service)

**Build (in order-service, NOT analytics-service)**

- `src/database/migrations/<timestamp>_create_events_outbox.ts`
- `src/lib/events/event-types.ts`, `events.types.ts`,
  `outbox.repository.ts`, `outbox-drain.service.ts`, `order-events.broker.ts`,
  `events.module.ts`.
- New env vars in `src/lib/config/app.config.ts` (RABBITMQ_ORDER_EVENTS_EXCHANGE,
  OUTBOUND_EVENTS_DRAIN_TICK_SEC, OUTBOUND_EVENTS_BATCH_SIZE).
- `src/app/order/order.service.ts` — insert outbox row in `placeOrder`'s trx
  after `bulkInsert`, before `commit`.
- Register `EventsModule` in `app.module.ts`.

**Conventions**

- order-service/CLAUDE.md §8 (Async with core-service section).
- Same exchange name (`order.events`), not `core.events`.
- `FOR UPDATE SKIP LOCKED` so multiple drainers per region are safe.

**Acceptance**

Place a real order through `POST /api/orders` (assumes COD path so payment
auth isn't required). Then:

```bash
go run ./play/check-mongo -restaurant <restaurantId>
# orders_count: 1, revenue_sum matches order total
```

---

## Phase 5 — Auth + RBAC + service + controller

**Build**

- `lib/auth/{jwt.go, middleware.go, apikey.go}`.
- `lib/coreclient/{client.go, types.go}` — fetch role permissions.
- `lib/rbac/{cache.go, middleware.go}`.
- `app/analytics/{types.go, errors.go, enums.go}`.
- `app/analytics/service/analytics.service.go` — `OnOrderPlaced`,
  `QueryRestaurantDays`.
- `app/analytics/dto/restaurant_days.{request,response}.go`.
- `app/analytics/controller/{analytics.controller.go, routes.go}`.
- `app/analytics/eventhandlers/handlers.go`.
- `lib/boot/boot.go` — add controller + router provider.

**Conventions**

- CLAUDE.md §5 (module conventions), §6 (Response DTOs), §8 (Auth + RBAC).

**Acceptance**

```bash
# Terminal 1
go run ./cmd/api

# Terminal 2
go run ./play/mock-core

# Terminal 3
TOK=$(go run ./play/mint-jwt -secret devsecret_replace_me \
        -role restaurant_user -uid 1)
curl -s -H "Authorization: Bearer $TOK" \
  "http://localhost:5000/api/v1/analytics/restaurants/42/days?from=2026-01-01&to=2026-12-31"
# 200 with a row that has avgOrderMinor derived from revenue/count
```

---

## Phase 6 — E2E acceptance (the 11-step gate)

Run **in order**, fix and re-run if any step fails:

1. `go build ./...` and `go vet ./...` clean.
2. Boot prints structured JSON: `mongo connected`, `rabbit connected`,
   `event consumer started`, `http listening`.
3. `GET /health` → 200 envelope.
4. `publish-test -restaurant 42 -total 2500` → `agg_restaurant_day` shows
   `orders_count: 1, revenue_sum: 2500`.
5. Same `-event-id` again → mongo unchanged (dedupe).
6. Second event `-total 1500` → `orders_count: 2, revenue_sum: 4000`.
7. `GET …/days` no auth → 401 `UNAUTHENTICATED`.
8. `GET …/days` garbage cookie → 401.
9. `GET …/days` with valid JWT → 200 with `ordersCount: 2,
   revenueMinor: 4000, avgOrderMinor: 2000`.
10. `from > to` → 400 `ANALYTICS_INVALID_DATE_RANGE`. Missing `from` → 400
    `VALIDATION_ERROR`. Bad path id → 400.
11. Restaurant with no data → 200 `[]`.

---

## Phase 7+ — Homework

See `plan.md` for the full list. Per piece:

- More aggregates (branch/product/platform days).
- More event handlers (payment.completed, order.delivered, order.rejected).
- Derived endpoints (failure-rate, delivery-avg).
- RBAC permissions_changed consumer.
- Backfill command under `cmd/backfill-aggs/`.
- Unit + integration tests.

Use `ai-prompts.md` to drive the AI through each piece in three steps:
**anchor → analogue → translate.**
