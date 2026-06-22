# plan.md — what's in the video, what's homework

## In the video (built end-to-end)

| Piece | Where |
| --- | --- |
| `cmd/api/main.go` (10 lines) | `cmd/api/main.go` |
| fx wiring of every singleton | `lib/boot/boot.go` |
| Config (struct + Load) | `lib/config/env.go` |
| slog JSON logger + correlation middleware | `lib/logger`, `lib/middleware/correlation.go` |
| `AppError` + handler middleware | `lib/errors/{apperror,handler}.go` |
| Success/error envelopes | `lib/http/response.go` |
| JWT verifier + auth middleware | `lib/auth/{jwt,middleware}.go` |
| RBAC read-through cache + middleware | `lib/rbac/{cache,middleware}.go` |
| Core-service HTTP client (RBAC perms only) | `lib/coreclient/client.go` |
| Generic event consumer (dedupe + DLQ) | `lib/coreevents/consumer.go` |
| Mongo connection wrapper | `pkg/mongo/client.go` |
| amqp091 broker wrapper | `pkg/messaging/amqp.go` |
| `event_ids` collection + unique-index dedupe | `app/analytics/repository/event_ids.repo.go` |
| `agg_restaurant_day` collection + upsert | `app/analytics/repository/agg_restaurant_day.repo.go` |
| Idempotent index creation | `app/analytics/repository/indexes.go` |
| AnalyticsService (`OnOrderPlaced`, `QueryRestaurantDays`) | `app/analytics/service/analytics.service.go` |
| Controller (GET /restaurants/:id/days) | `app/analytics/controller/analytics.controller.go` |
| Request + Response DTOs | `app/analytics/dto/restaurant_days.{request,response}.go` |
| `order.placed` event handler | `app/analytics/eventhandlers/handlers.go` |
| Order-service publisher (outbox + drainer) | `order-service/src/lib/events/` + migration |
| Dev helpers | `play/{mock-core,publish-test,mint-jwt,check-mongo}` |
| All docs | `docs/` + `CLAUDE.md` + `README.md` |

## Homework — explicitly NOT built

| Piece | Hint |
| --- | --- |
| `agg_branch_day`, `agg_product_day`, `agg_platform_day` | Add a repo per collection, a `Build` entry in eventhandlers, and a new fx.Provide in lib/boot. `agg_product_day` is one upsert per line item — use `BulkWrite`, not a loop. |
| `payment.completed` handler | Mostly the same as `order.placed`; bumps `revenue_sum` after capture for online orders. |
| `order.delivered` handler | Out-of-order delivery is the gotcha — delivered may arrive before placed because they're routed through different queues in some failure modes. Treat as upsert; create the row if missing. |
| `order.rejected` handler | Decrement `orders_count`, decrement `revenue_sum` by the original amount. Or, simpler, maintain a separate `rejected_count` field and never decrement. |
| 7 more endpoints (failures, delivery-avg, active-restaurants, …) | Failure-rate and delivery-avg are **derived** — no new collections needed. |
| `rbac.permissions_changed` consumer | `lib/rbac/cache.go` already exposes `Invalidate(role)`. Add a binding + handler that calls it. |
| `cmd/backfill-aggs/` | One-shot binary that scans `orders` on order-service's shards and replays into the aggregates. Add `--from-date` / `--to-date` flags and run idempotently. |
| Unit + integration tests | Unit tests live under mirrored `test/unit/` package paths. Aggregate HTTP integration tests are split by restaurant, branch, product, and platform under `test/integration/`, with real MongoDB/RabbitMQ event fan-out coverage. |
| Outbound events | This service has no outbox today. If a downstream consumer needs them, mirror order-service's `events_outbox` pattern. |

## Where the teaching weight is

Most of the codebase is one-time wiring. The pieces students will edit most:

1. `app/analytics/repository/*` (one file per new collection).
2. `app/analytics/service/analytics.service.go` (one method per new use case).
3. `app/analytics/eventhandlers/handlers.go` (one entry per new event type).
4. `app/analytics/controller/analytics.controller.go` (one method per new endpoint).
5. `lib/boot/boot.go` (one `fx.Provide` per new singleton).

Everything else (config, logger, errors, middleware, broker, mongo client)
should not need to change.
