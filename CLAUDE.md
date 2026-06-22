# CLAUDE.md — analytics-service guidelines

These rules apply to the **`analytics-service`** microservice of the QuickBite
platform. They mirror the architectural conventions of `core-service` and
`order-service`, translated into Go idioms. When in doubt, find the Node
analogue and mirror it. Deviations are documented inline.

---

## 1. Mission

`analytics-service` owns **per-day rollups** of order/payment/delivery activity:

- **Consumes** `order.events` from RabbitMQ (`order.#, payment.#` bindings).
- **Upserts** day-grained aggregates into MongoDB.
- **Serves** read-only HTTP endpoints under `/api/v1/analytics/...`.

It does **not** own users, restaurants, products, orders, payments. It does
**not** write to any operational data store. It does **not** emit events.

---

## 2. Tech stack (locked — do not deviate)

| Concern        | Choice                                                                |
| -------------- | --------------------------------------------------------------------- |
| Runtime        | Go 1.21+                                                              |
| HTTP router    | `github.com/go-chi/chi/v5`                                            |
| DB             | `go.mongodb.org/mongo-driver` (official, v1)                          |
| Messaging      | `github.com/rabbitmq/amqp091-go`                                      |
| Config         | `github.com/caarlos0/env/v11` (struct tags)                           |
| Logger         | stdlib `log/slog`                                                     |
| Validation     | `github.com/go-playground/validator/v10`                              |
| JWT            | `github.com/golang-jwt/jwt/v5`                                        |
| UUID           | `github.com/google/uuid`                                              |
| DI             | `go.uber.org/fx` — providers wired in `lib/boot/boot.go`              |

**Forbidden:** ORMs (GORM, Ent, mgo). The repository layer uses mongo-driver
directly with typed `bson`-tagged structs. This is the Go analogue of
"Knex query builder, not an ORM" in the Node services.

**Forbidden:** schema migrations. Mongo is schemaless. Index definitions live
in `app/<module>/repository/indexes.go` and are applied idempotently on boot
by an `OnStart` fx hook.

---

## 3. Folder structure (mirrors the Node services)

```
analytics-service/
├── cmd/api/main.go              # 10 lines — calls lib/boot.Run()
├── cmd/backfill-aggs/main.go    # thin CLI over internal/backfill
├── internal/backfill/backfill.go
├── pkg/                         # framework-free, app-agnostic
│   ├── mongo/client.go
│   ├── messaging/{types.go, amqp.go}
│   └── httpclient/client.go
├── lib/                         # app-aware glue
│   ├── boot/boot.go             # fx wiring lives here
│   ├── config/env.go
│   ├── logger/logger.go
│   ├── appcontext/context.go
│   ├── errors/{apperror.go, handler.go}
│   ├── http/response.go
│   ├── middleware/correlation.go
│   ├── auth/{jwt.go, middleware.go, apikey.go}
│   ├── rbac/{cache.go, middleware.go}
│   ├── coreclient/{client.go, types.go}
│   └── coreevents/{consumer.go, payloads.go}
├── app/analytics/               # module package — types/errors/enums at root
│   ├── types.go errors.go enums.go
│   ├── entity/agg_restaurant_day.go
│   ├── repository/{indexes.go, agg_restaurant_day.repo.go, event_ids.repo.go}
│   ├── service/analytics.service.go
│   ├── controller/{analytics.controller.go, routes.go}
│   ├── dto/{restaurant_days.request.go, restaurant_days.response.go}
│   └── eventhandlers/handlers.go
├── play/                        # GITIGNORED — dev aids only
│   ├── mock-core/  publish-test/  mint-jwt/  check-mongo/
├── test/
│   ├── unit/                    # mirrored black-box unit-test packages
│   └── integration/             # Mongo/Rabbit aggregate + event E2E tests
├── docs/                        # see doc index below
├── go.mod  .env.example  .gitignore  CLAUDE.md  README.md  plan.md
```

### Layering rules (enforced by review)

```
app/  → may import lib, pkg
lib/  → may import pkg + lib/config; may NOT import app/<module>/*
pkg/  → no imports from lib or app, no env, no globals, NO app-specific knowledge
```

**Strict reading of "no app-specific knowledge in `pkg/`":** `pkg/mongo` knows
how to connect; it does NOT know that this service has an `agg_restaurant_day`
collection. Collection names live in `app/analytics/repository/indexes.go`.

If `lib/*` needs something from `app/*`, invert via interface. `lib/coreevents`
defines `EventDeduper`; `app/analytics/repository.EventIDsRepo` satisfies it
structurally via Go duck typing.

`cmd/api/main.go` is intentionally ~10 lines: hand control to `lib/boot.Run()`.
New singletons = new `fx.Provide`; `main.go` never changes.

### Module-level types live at the parent `app/<module>/` package

Inputs (`OnOrderPlacedInput`), projections (`RestaurantDayRow`), error
catalogues (`Err…`), permission constants (`PermAnalyticsRead`), and event
constants live in `app/analytics/types.go` / `errors.go` / `enums.go` —
**not** in `app/analytics/service/`. Subpackages (`service`, `controller`,
`eventhandlers`) all import them by the same short name.

---

## 4. Naming conventions

| Layer   | Filename                          | Type / Symbol                 |
| ------- | --------------------------------- | ----------------------------- |
| Module  | `analytics.service.go`            | `type AnalyticsService struct` |
|         | `analytics.controller.go`         | `type AnalyticsController struct` |
|         | `agg_restaurant_day.repo.go`      | `type AggRestaurantDayRepo struct` |
|         | `agg_restaurant_day.go` (entity)  | `type AggRestaurantDay struct` |
|         | `*.request.go` / `*.response.go`  | `*Request` / `*Response` |
| Errors  | `errors.go`                       | `var ErrXxx = apperr.New(...)` |
| Enums   | `enums.go`                        | `const PermXxx = "..."` |

- `snake_case` for filenames, dotted role suffixes (`.controller.go`,
  `.service.go`, `.repo.go`, `.request.go`, `.response.go`).
- `PascalCase` exported, `camelCase` unexported.
- Package names are short, lowercase, no underscores (Go convention).
- Constructors are `NewXxx`; fx providers re-export them via `func New(...)` in
  `routes.go`-style provider files when a different signature is needed.

### Mongo

- Collection names are snake_case in code (the `Collection*` constants in
  `repository/indexes.go`).
- BSON tags on entity structs are `snake_case`.
- Money fields are `int64` minor units (CLAUDE.md §6). Never `float64`,
  never `decimal128`.
- Timestamps stored as `time.Time` (BSON → `Date`) UTC.

### Routes

- All routes under `/api/v1`.
- Plural resource nouns: `/restaurants/{id}/days`.
- Path params validated via `strconv.ParseInt`; query params via the
  `validator/v10` `validate:"required,len=10"` tags.

---

## 5. Module conventions

Every module under `app/<module>/` follows this skeleton:

1. **`types.go errors.go enums.go`** at the root — module-shared.
2. **`entity/<n>.go`** — plain struct, `bson` tags, no methods beyond
   simple invariants.
3. **`repository/<n>.repo.go`** — owns ALL `*mongo.Collection` calls for the
   module. Constructor takes `*mongo.Database`. One file per collection.
4. **`repository/indexes.go`** — `EnsureIndexes(ctx, *mongo.Database)`.
   Idempotent.
5. **`service/<module>.service.go`** — business logic. Constructor takes
   repos. Returns `error`; uses `lib/errors.AppError` for stable failure
   modes.
6. **`controller/<module>.controller.go`** — HTTP handlers. Each handler is
   `errors.HandlerFunc` (`func(w, r) error`). `MountRoutes(r chi.Router, …)`
   registers paths + middleware. Always returns Response DTOs.
7. **`controller/routes.go`** — fx-provider façade (`func New(...)`).
8. **`dto/*.request.go` / `dto/*.response.go`** — wire shapes.
9. **`eventhandlers/handlers.go`** — `Build(svc) map[string]EventHandler`.

**Inline named `type X struct {...}` / `interface X` declarations are
forbidden** inside `service/`, `controller/`, `repository/`, `eventhandlers/`
files. Move named shapes to the parent module package (`types.go`) or, for
cross-cutting infra, to `lib/<area>/types.go` (or `payloads.go`). Anonymous
inline shapes used in a single method signature are fine.

---

## 6. Response shape (the rule that differs from core)

In core-service, services return raw entities and a `SuccessInterceptor`
wraps them. **In this service every HTTP response payload MUST be shaped by
a Response DTO** in `dto/*.response.go`.

- Plain Go struct with `json` tags. No mongo `bson` tags on response DTOs.
- Static-style factories `FromRow` / `FromRows` mirror Node's
  `OrderResponseDTO.from(...)`.
- Money fields are integer minor units (`int64`), accompanied by a
  `currency` field.
- Timestamps as ISO-8601 UTC strings.
- Never leak `_id`, internal numeric IDs, or `*_hash` / provider secrets.

The success envelope is:

```json
{"success": true, "data": ...}
```

Errors:

```json
{"success": false, "error": {"code": "...", "message": "..."}}
```

Paginated envelopes additionally surface `meta` at the top level — built by
`libhttp.SendPaginated`.

---

## 7. Database rules

- Indexes are added **only** to support a query that exists in code.
  Document the supporting query in a comment in `indexes.go`.
- Unique indexes are the dedupe primitive (`agg_*` collections: unique
  `(restaurant_id, date)`; `event_ids`: unique `event_id`).
- TTL indexes only on bookkeeping collections (`event_ids.received_at`,
  7d).
- Aggregates are stored as `sum + count`, never `avg`. Averages are derived
  by the service layer at read time.
- **No app-side joins.** A read either targets one collection or runs a
  Mongo aggregation pipeline; never fan-out reads from a service method.
- Replays are expected to occur (at-least-once delivery). Aggregate updates
  must be associative: `$inc orders_count`, `$inc revenue_sum`,
  `$setOnInsert` for static fields. Dedupe at the consumer prevents the
  replay before it reaches the aggregation update.

---

## 8. Cross-cutting infra

### DI: `go.uber.org/fx`

- `cmd/api/main.go` calls `boot.Run()`. `Run` constructs the fx.App with
  `fx.Provide(...)` listing every constructor and `fx.Invoke(...)` for
  lifecycle hooks (index creation, consumer start, http listen).
- Every constructor returns `(T, error)` if it can fail, or `T` if it
  cannot.
- Lifecycle hooks (Mongo Disconnect, Rabbit Close, HTTP Shutdown) are
  attached via `lc.Append(fx.Hook{OnStart, OnStop})` inside the relevant
  provider.

### Auth + RBAC

- JWT verified with `ACCESS_SECRET` (same as core/order). HS256.
- Token from cookie `access_token` first, then `Authorization: Bearer`.
- RBAC: this service has no permissions catalog. Permissions are fetched
  from core's `GET /api/roles/:role/permissions` (with the
  `x-api-key: <internal>` header) and cached in-process by role for
  `RBAC_CACHE_TTL_SEC` (default 300s).
- Middleware order: `auth.Require(verifier)` then `rbac.Require(cache,
  "<perm>")` — enforced by `controller.MountRoutes`.
- `rbac.permissions_changed` consumer is **homework** — `Cache.Invalidate`
  is already implemented.

### Errors

- `lib/errors.AppError{Code, Status, Message, cause}`. Stable cases declared
  as `var ErrXxx = apperr.New(...)` in the module's `errors.go`. Callers
  attach context with `.WithCause(err)`.
- Controllers use `apperr.Wrap(handler)` to convert `HandlerFunc` (returning
  `error`) into `http.Handler`. Unknown errors render as
  `INTERNAL_ERROR 500`.

### Logging

- `log/slog` JSON to stdout. Same shape as Node services so platform log
  infra parses uniformly.
- `lib/middleware.Correlation` attaches a per-request child logger
  (`correlation_id` baked in) to ctx. Handlers / services pull it via
  `logger.FromContext(ctx)`.

### Idempotency

- Inbound: Mongo `event_ids` collection. Unique index on `event_id`;
  `InsertOne` → dup-key on replay → handler returns "fresh=false" and
  acks-skip.
- This service has no idempotent **outbound** writes — it only consumes.

### Messaging

- Topology declared via `messaging.AMQPBroker.DeclareTopology` (idempotent).
- Topic exchange `order.events` (durable, declared by order-service).
- Queue `analytics-service.order-events` (durable, DLX wired).
- Bindings `order.#, payment.#`.
- DLX `order.events.dlx`, DLQ `analytics-service.order-events.dlq`.
- Manual ack. Unknown event types → ack-skip + warn (forward-compat).
  Handler errors → nack-no-requeue → DLQ.

---

## 9. Performance & scale rules

1. **One Mongo round-trip per write.** Order-placed handler = single
   `UpdateOne` upsert. Future bulk inserts (homework) must use
   `BulkWrite`, never per-iteration `InsertOne`.
2. **Every query backed by an index.** Add the supporting comment in
   `indexes.go`. No speculative indexes.
3. **Never `SELECT *` (or full doc Find with no projection)** — keep the
   surface minimal. Today's docs are tiny so this hasn't bitten yet.
4. **Long-running work** (backfill, archival) lives in `cmd/<workername>/`
   (homework), not the API process.
5. **HTTP handlers don't block on RabbitMQ.** Connection failures degrade
   to "events back up in order-service's outbox until broker recovers" —
   the API still serves reads.

---

## 10. Code style — what to avoid

- ❌ ORMs. Mongo-driver directly, with typed structs.
- ❌ Returning entities (or `bson.M`) from controllers — always Response DTOs.
- ❌ Cross-module repository imports. Cross-module access goes through the
  other module's service.
- ❌ Business logic in controllers. Controller = decode + validate + call
  service + render DTO.
- ❌ `_ = err` / silent failures. Either propagate the error or convert it
  to an `*AppError` with a stable code.
- ❌ Plain `errors.New(...)` for cases that have a stable name. Use
  module-level `var ErrXxx = apperr.New(...)`.
- ❌ `interface{}` / `any` in service signatures. Use typed structs.
- ❌ Hidden mutability — repositories take values, return values; pointers
  only when nil signals absence.
- ❌ Inline `type X struct {...}` declarations in service/controller/repo
  files. Move to the parent module package.
- ❌ NestJS-style decorators. We use struct tags (`bson:"..."`,
  `json:"..."`, `validate:"..."`) and fx providers.
- ❌ Adding env vars without listing them in `lib/config/env.go` and
  `.env.example`.

---

## 11. When implementing a new module

1. `types.go errors.go enums.go` at the parent package — declare the shared
   shapes first.
2. Entity struct(s) under `entity/`.
3. Repository under `repository/<n>.repo.go` (one file per collection)
   and update `indexes.go`.
4. Service.
5. DTO request + response.
6. Controller + `routes.go` provider.
7. Event handlers (if any) under `eventhandlers/handlers.go`.
8. Add the new `fx.Provide(...)` lines in `lib/boot/boot.go` —
   `cmd/api/main.go` does NOT change.
9. Smoke-test with the relevant `play/` binary before moving on.

---

## 12. Reference docs (in `docs/`)

- `docs/folder-structure.md` — annotated tree, layer rules.
- `docs/system-design.md` — ASCII platform diagram, sync/async flows,
  failure-mode table.
- `docs/api-contracts.md` — endpoint-by-endpoint request/response shapes
  and every error code.
- `docs/node-to-go-mapping.md` — **the teaching doc.** Side-by-side TS → Go
  translation per layer.
- `docs/ai-prompts.md` — prompt templates for students doing the homework.
- `docs/implementation-plan.md` — phases 1–6 (in the video) and 7+
  (homework), with acceptance checks.

---

## 13. Out of scope (do not build)

- Outbound events — this service does NOT emit. If a future consumer
  requires it, add an outbox here too.
- DevOps / observability stack / read replicas — future, separate effort.
