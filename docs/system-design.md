# system-design.md

## Platform diagram (zoomed-out)

```
                       ┌──────────────────────────┐
                       │      core-service        │
                       │ Postgres + Redis + Nest  │
                       │  users / restaurants /   │
                       │  products / RBAC         │
                       └─────┬───────────┬────────┘
                  GET roles  │  amqp     │  (no events to analytics)
                  /perms     │  core.    │
                  (sync HTTP)│  events   │
                             ▼           ▼
                          ┌──────────────────────────┐
                          │      order-service       │
                          │ Postgres (sharded) +     │
                          │ Redis + Nest             │
                          │  orders / payments /     │
                          │  deliveries              │
                          └─────────────┬────────────┘
                                        │ outbox drain
                                        │ exchange: order.events
                                        │ routing keys: order.*, payment.*
                                        ▼
            ┌────────────── RabbitMQ ──────────────────┐
            │ exchange: order.events (topic)           │
            │ queue:    analytics-service.order-events │
            │   bind:   order.#, payment.#             │
            │   DLX:    order.events.dlx               │
            │   DLQ:    analytics-service.order-events.dlq │
            └─────────────────────┬────────────────────┘
                                  │ at-least-once delivery
                                  ▼
                       ┌──────────────────────────┐
                       │    analytics-service     │
                       │ Go + MongoDB             │
                       │  consume → dedupe → roll │
                       │  GET /api/v1/analytics/* │
                       └────────────┬─────────────┘
                                    │ unique-key dedupe
                                    │ upsert with $inc
                                    ▼
                              ┌──────────┐
                              │  Mongo   │
                              │  agg_*   │
                              │  event_  │
                              │  ids     │
                              └──────────┘
```

## Sync flow — `GET /restaurants/:id/days`

```
client ──cookie/Bearer──▶ analytics chi router
                             │
                             ▼
        ┌─────── lib/middleware.Correlation ──────┐
        │       lib/middleware.AccessLog          │
        │       lib/auth.Require (JWT)            │
        │       lib/rbac.Require ("analytics:read")│
        └──────────────┬───────────────────────────┘
                       ▼
       controller.GetRestaurantDays
                       │
                       ▼ DTO validation (validator/v10)
       service.QueryRestaurantDays
                       │
                       ▼ (revenue_sum / orders_count) → avgOrderMinor
       repository.FindByRestaurantInRange
                       │
                       ▼
                     Mongo
```

RBAC: cache miss → core-client → core-service `/api/roles/:role/permissions`.
Cached by role for `RBAC_CACHE_TTL_SEC` (default 300s).

## Async flow — `order.placed`

```
order-service placeOrder trx
   │
   ├─ insert into orders                     (same trx)
   ├─ insert into order_items                (same trx)
   ├─ INSERT INTO events_outbox(event_id…)   (same trx)
   └─ COMMIT
                       │
                       ▼
   outbox-drain.service.ts (every 2s, per region)
       claim batch FOR UPDATE SKIP LOCKED → publish to order.events → mark dispatched
                       │
                       ▼
   ┌─── RabbitMQ topic exchange "order.events" ───┐
   │  routing key = event_type ("order.placed")   │
   └─────────────────────┬────────────────────────┘
                         ▼
   analytics-service lib/coreevents.Consumer
       │
       │ JSON decode envelope
       ▼
   repository.EventIDsRepo.MarkSeen(eventId)  ─── dup-key? ack-skip
       │
       │ fresh
       ▼
   eventhandlers.handleOrderPlaced
       │
       ▼
   service.OnOrderPlaced
       │
       ▼
   repository.IncrementOrderRow (UpdateOne upsert):
       filter:        {restaurant_id, date}
       $inc:          {orders_count: 1, revenue_sum: total}
       $setOnInsert:  {currency}
       $set:          {updated_at: now}
       │
       ▼
   d.Ack()
```

## Why Mongo unique-key dedupe, not Redis SETNX

Redis SETNX is what order-service uses for inbound core.events dedupe. We
deliberately differ here. Trade-offs:

| Property | Redis SETNX | Mongo `event_ids` unique insert |
| --- | --- | --- |
| Fast happy path | yes (sub-ms) | a little slower (network + index check) |
| Bounded TTL | yes (24h) | yes (TTL index, 7d) |
| Survives Redis bounce | no — dedupe set is lost on restart | yes — same fate as the aggregate writes |
| Same fate as the data it protects | no | **yes** — both are in Mongo |

For analytics, "did we already process this event" must share fate with
"the aggregate row we'd update if we hadn't." Losing one without the other
is the failure mode that double-counts. Mongo gives us co-location for
free; Redis would require another consistency story.

The 7-day TTL is conservative — RabbitMQ retries cap out long before that.

## Failure-mode table

| Failure | Behaviour |
| --- | --- |
| RabbitMQ down at boot | Boot continues. Consumer.Start logs the error; once broker recovers, you'll need to restart the API to re-arm the consumer. Acceptable for the milestone — production would add a retry-loop. |
| RabbitMQ down mid-run | Order-service outbox accumulates rows. Consumer reconnect is amqp091's default; analytics will need a manual nudge today. |
| Mongo down at boot | Boot fails loud (mongo Connect returns error). Operator's job to recover. |
| Mongo down mid-run | Each `UpdateOne` returns an error. Handler returns it; consumer nacks-no-requeue; event flows to DLQ. We do not retry in-place because RabbitMQ retry semantics + DLQ replay are clearer for the operator. |
| Duplicate event delivery | `EventIDsRepo.MarkSeen` returns `fresh=false`; consumer acks. Aggregate untouched. |
| Out-of-order delivery (delivered before placed) | `agg_restaurant_day` is keyed by `restaurant_id + date`. `$setOnInsert` plus `$inc` make the upsert order-insensitive for now (only `order.placed` is wired; future handlers must preserve this property). |
| Unknown event type | Consumer logs warning, acks (forward-compat). |
| Handler panic | amqp091 channel close on goroutine death is the worst case; today's handlers don't panic, but adding `defer recover()` is an obvious next step. |
| Core-service down (RBAC fetch) | First request after cache expiry: 403 (we treat "can't reach core" as "no perms"). Acceptable for a teaching artifact; production would either fail open (with audit) or surface 503. |

## Why fx (DI)

This service started with the spec's "NO framework — wire in main.go"
guidance, and the user opted in to `go.uber.org/fx` instead. The trade-off:

- **Pro:** providers declare dependencies in their signature; fx resolves
  the graph. Adding a singleton = `fx.Provide(NewX)`, not editing main.go
  + all call sites.
- **Pro:** lifecycle hooks (`OnStart` / `OnStop`) replace the Nest
  `OnModuleInit` / `OnApplicationShutdown` pattern one-for-one.
- **Con:** errors at boot are slightly less direct (you read a fx error
  graph, not a stack trace).
- **Con:** more magic than students used to constructor-wiring may expect.

We mitigate the con by keeping the entire wiring in ONE file
(`lib/boot/boot.go`) — anything you'd "grep main.go for" you grep there.
