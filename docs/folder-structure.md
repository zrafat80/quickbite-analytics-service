# folder-structure.md

The repo deliberately mirrors `order-service/src/` so a reader familiar
with the Node side can navigate this codebase immediately.

```
analytics-service/
├── cmd/
│   ├── api/main.go              # process entry point — calls lib/boot.Run()
│   └── backfill-aggs/main.go    # thin one-shot CLI
│
├── internal/
│   └── backfill/backfill.go     # testable backfill implementation
│
├── pkg/                         # framework-free, app-agnostic providers
│   ├── mongo/
│   │   └── client.go            # Connect / Disconnect — knows nothing about
│   │                            # specific collections
│   ├── messaging/
│   │   ├── types.go             # Broker interface, Delivery, ConsumerOptions
│   │   └── amqp.go              # amqp091 implementation of Broker
│   └── httpclient/
│       └── client.go            # net/http wrapper: timeout, JSON, retry-on-5xx
│
├── lib/                         # app-aware glue (env, middleware, errors)
│   ├── boot/
│   │   └── boot.go              # fx wiring: Providers + Lifecycle hooks
│   ├── config/
│   │   └── env.go               # struct + Load() — Go analogue of zod schema
│   ├── logger/
│   │   └── logger.go            # slog JSON + FromContext / WithContext
│   ├── appcontext/
│   │   └── context.go           # ctx keys: claims, correlation_id
│   ├── errors/
│   │   ├── apperror.go          # AppError{Code, Status, Message, cause}
│   │   └── handler.go           # HandlerFunc + Wrap middleware
│   ├── http/
│   │   └── response.go          # SendSuccess, SendPaginated, SendError
│   ├── middleware/
│   │   └── correlation.go       # Correlation + AccessLog middleware
│   ├── auth/
│   │   ├── jwt.go               # Verifier (HS256, shape mirrors core)
│   │   ├── middleware.go        # Require(verifier) — chi-compatible
│   │   └── apikey.go            # RequireInternalAPIKey (parity, unused today)
│   ├── rbac/
│   │   ├── cache.go             # read-through cache, TTL, Invalidate
│   │   └── middleware.go        # Require(cache, perm) — chi-compatible
│   ├── coreclient/
│   │   ├── client.go            # GetRolePermissions
│   │   └── types.go             # Envelope + RolePermissionsResponse
│   └── coreevents/
│       ├── consumer.go          # Register + Start, dedupe + DLQ
│       └── payloads.go          # Envelope + EventHandler type + EventDeduper iface
│
├── app/
│   └── analytics/               # the only business module today
│       ├── types.go             # OnOrderPlacedInput, RestaurantDayRow, …
│       ├── errors.go            # var ErrXxx = apperr.New(...)
│       ├── enums.go             # const PermAnalyticsRead, event-type names
│       ├── entity/
│       │   └── agg_restaurant_day.go
│       ├── repository/          # ONLY place mongo-driver appears
│       │   ├── indexes.go
│       │   ├── agg_restaurant_day.repo.go
│       │   └── event_ids.repo.go
│       ├── service/
│       │   └── analytics.service.go
│       ├── controller/
│       │   ├── analytics.controller.go
│       │   └── routes.go        # fx provider façade
│       ├── dto/
│       │   ├── restaurant_days.request.go
│       │   └── restaurant_days.response.go
│       └── eventhandlers/
│           └── handlers.go      # event-type → service method bridge
│
├── play/                        # GITIGNORED — dev aids only
│   ├── mock-core/main.go        # stands in for core-service RBAC endpoint
│   ├── publish-test/main.go     # publishes one synthetic order.placed
│   ├── mint-jwt/main.go         # mints an HS256 token with the same shape
│   └── check-mongo/main.go      # dumps agg_restaurant_day
│
├── test/
│   ├── unit/                    # mirrors app/lib/pkg/internal package paths
│   └── integration/             # aggregate HTTP + repository + event E2E
│
├── docs/                        # see CLAUDE.md §12 for the index
├── go.mod  go.sum
├── .env.example  .gitignore
└── CLAUDE.md  README.md  plan.md
```

## Layering, restated

```
app/  → may import lib, pkg
lib/  → may import pkg + lib/config; may NOT import app/<module>/*
pkg/  → no imports from lib or app, no env, no globals, NO app-specific knowledge
```

Production code may import `app/` only at composition or application-workflow
boundaries:

1. `lib/boot/boot.go` — wires the whole tree (the ONE place that knows
   everyone).
2. `cmd/backfill-aggs/main.go` and `internal/backfill/` — the one-shot
   backfill composition root and its importable workflow.
3. `play/<bin>/main.go` — dev binaries, allowed to reach in.

Black-box tests under `test/` may import any exported package surface needed
to exercise production behavior.

If `lib/<area>` needs something from `app/<module>`, invert with a tiny
interface defined in `lib/`. Today's example: `lib/coreevents.EventDeduper`
is satisfied structurally by `app/analytics/repository.EventIDsRepo`.

## Why `cmd/api/main.go` is 10 lines

The entry point should do one thing: hand control to the bootstrap and
exit. Putting wiring in `main.go` means every new singleton requires
editing the entry point, which gets crowded. Wiring lives in
`lib/boot/boot.go`. Adding a module = edit one fx.Provide.

## Why `play/` instead of `cmd/`

`play/` is gitignored. The four programs in it are dev aids — they make
the slice testable without spinning up Postgres + order-service, but they
are not part of the service. Pollute `cmd/` with them and someone will
assume they're production binaries.

## Why module-level types/errors/enums at `app/<module>/`

The service struct is one concern; the types it consumes and returns are a
different concern. Putting `OnOrderPlacedInput`, `ErrInvalidDateRange`,
`PermAnalyticsRead` in the parent `analytics` package means subpackages
(`service`, `controller`, `eventhandlers`) all import them by the same
short name, and `service/` stays focused on the service implementation
only.
