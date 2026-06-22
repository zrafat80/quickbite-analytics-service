# ai-prompts.md — prompt templates for the homework

You're not learning Go in a vacuum. You're learning **how to direct an AI
through a non-trivial implementation in a new language by anchoring it to
a codebase you already know.** That skill is what this homework drills.

Three rules for every prompt:

1. **Make the AI prove it understood the existing code before writing new
   code.** "Read X. Tell me what it does in 3 sentences. Then …"
2. **Ask for the Node analogue first, then the Go translation.** When you
   can name the analogue out loud, you stop accepting hallucinated
   patterns.
3. **Specify the file paths, struct names, and lines you expect to
   change.** A prompt that just says "add a handler" pushes the design
   work onto the AI. A prompt that names `app/analytics/eventhandlers/
   handlers.go:42` and the existing `handleOrderPlaced` keeps the AI on
   rails.

---

## The 3-step pattern (use this for every piece of homework)

```
STEP 1 — anchor in the existing code.
Read these files and tell me, in ≤5 bullets, what they do today:
  • <list paths>
Do not write any new code yet.

STEP 2 — find the analogue.
In ≤3 sentences, name the closest Node analogue in
core-service or order-service for what I'm about to ask. Quote the file
path and line range. If there is no analogue, say so and stop.

STEP 3 — translate.
Now implement <task> by mirroring the analogue from step 2 in Go,
following the conventions in CLAUDE.md §<n>. Show me only the diff —
no prose around it.
```

---

## Concrete prompts for each homework piece

### Add `agg_branch_day`

```
STEP 1: Read these and tell me what they do (≤5 bullets, no code):
  • app/analytics/entity/agg_restaurant_day.go
  • app/analytics/repository/indexes.go
  • app/analytics/repository/agg_restaurant_day.repo.go
  • app/analytics/service/analytics.service.go (OnOrderPlaced only)
  • app/analytics/eventhandlers/handlers.go (handleOrderPlaced only)

STEP 2: In ≤3 sentences, name the Node analogue of what I'm about to ask:
"add a second per-day aggregate collection, branch-scoped."

STEP 3: Add agg_branch_day end-to-end. Files to add/edit (do NOT touch
anything else):
  • app/analytics/entity/agg_branch_day.go              [NEW]
  • app/analytics/repository/agg_branch_day.repo.go     [NEW]
  • app/analytics/repository/indexes.go                 [EDIT — add unique
    (branch_id, date) + range (date, branch_id)]
  • app/analytics/service/analytics.service.go         [EDIT — extend
    OnOrderPlaced to write both rows in sequence; no goroutines]
  • lib/boot/boot.go                                    [EDIT — one new
    fx.Provide line]

Conventions: see CLAUDE.md §5 (module conventions), §7 (db rules).
Money fields are int64 minor units. Date is YYYY-MM-DD UTC.

Show me only the diff.
```

### Add the `payment.completed` handler

```
STEP 1: Read order-service's payment service and the existing order.placed
flow, and tell me in ≤5 bullets what payment.completed means in the
domain. Do NOT write code yet.

STEP 2: In ≤3 sentences: what's the difference between updating an
aggregate when an order is placed vs. when a payment completes? Why are
both needed for ONLINE orders?

STEP 3: Wire payment.completed:
  • app/analytics/eventhandlers/handlers.go              [EDIT — add
    handlePaymentCompleted, register under analytics.EventPaymentComplete]
  • app/analytics/service/analytics.service.go          [EDIT — add a
    method that bumps revenue_sum without touching orders_count]
  • app/analytics/types.go                              [EDIT — add a
    new input struct, OnPaymentCompletedInput]

Convention: never inline named types in service/. All shared types live
in app/analytics/types.go.

Show me only the diff.
```

### Add the failure-rate endpoint (derived)

```
STEP 1: Read app/analytics/controller/analytics.controller.go and tell me
in ≤5 bullets how a controller is wired today.

STEP 2: In ≤3 sentences, name a Node controller method that returns a
derived metric (not a stored field). Quote the file.

STEP 3: Add GET /api/v1/analytics/restaurants/{id}/failures?from=&to=.
The metric is rejected_count / orders_count, computed in the service.
You'll need new aggregate fields (rejected_count) — for this prompt,
assume the handler for order.rejected is already written (it's a separate
homework item).

Files to add/edit (and ONLY these):
  • app/analytics/dto/restaurant_failures.response.go   [NEW]
  • app/analytics/service/analytics.service.go         [EDIT — add
    QueryRestaurantFailures method]
  • app/analytics/types.go                              [EDIT — add
    RestaurantFailureRow struct]
  • app/analytics/controller/analytics.controller.go   [EDIT — add
    GetRestaurantFailures + mount route]

Show me only the diff.
```

### Hook up `rbac.permissions_changed`

```
STEP 1: Read order-service/src/lib/core-events/handlers/
rbac-permissions-changed.handler.ts and tell me in ≤5 bullets what it does
and what payload shape it expects.

STEP 2: In ≤3 sentences, where in analytics-service is rbac.Cache.Invalidate
implemented today and what's missing for it to be triggered automatically?

STEP 3: Add the consumer wiring:
  • lib/rbac/eventhandler.go                           [NEW — exports
    a function returning a coreevents.EventHandler that calls
    cache.Invalidate(role) on receipt]
  • app/analytics/eventhandlers/handlers.go            [EDIT — register
    the rbac handler under "rbac.permissions_changed"]
  • lib/boot/boot.go                                    [EDIT — make the
    rbac handler available to the Build map]

The payload contains `role` as a string. Defensive parsing only; if the
role field is missing, log and ack-skip (don't DLQ — RBAC failures
shouldn't poison the queue).

Show me only the diff.
```

### Build `cmd/backfill-aggs/`

```
STEP 1: Read order-service's archival worker and tell me in ≤5 bullets
how it iterates rows in keysets without blowing memory.

STEP 2: In ≤3 sentences, what's the simplest way to talk to order-service's
Postgres from a Go binary? (Hint: pgx, NOT a fresh ORM.)

STEP 3: Add cmd/backfill-aggs/ that:
  • takes --from-date and --to-date flags
  • iterates orders in keyset batches (1000 at a time)
  • calls service.AnalyticsService methods directly (no HTTP)
  • is idempotent (the consumer's $inc semantics ARE associative because
    of how we structured the upsert — backfill replays a placed event as
    if it were live, dedupe via event_ids unique index would block
    re-runs; the backfill should use a SEPARATE eventId namespace,
    something like "backfill:<orderId>", so a second backfill run is
    blocked while a future live event for the same order is NOT)

Files to add (and ONLY these):
  • cmd/backfill-aggs/main.go                           [NEW]
  • cmd/backfill-aggs/orders.go                        [NEW — pgx scan
    helper]

Do not reuse the fx tree — backfill is a CLI binary, wire dependencies by
hand.

Show me only the diff.
```

---

## Anti-patterns — don't use these prompts

> "Build the analytics service."

The AI will guess. You'll get something that compiles but ignores half
the rules.

> "Add agg_branch_day. Look at the existing code for the pattern."

Too vague. The AI will look at five files and decide the most "Goish"
shape, which won't match the codebase conventions.

> "Implement this from scratch, no boilerplate."

The boilerplate IS the architecture. If you skip it, you've changed the
service.

---

## The meta-lesson

The reason this homework is structured around prompts is that — in any
job you take after this course — **most of what you'll be asked to do is
to extend a codebase someone else already designed.** The skill is not
"write Go from scratch." The skill is "ship the next module without
breaking the conventions of the previous twelve." Three-step prompts
force that skill into the foreground.

When you ship a homework PR, ask in your description: "what convention
did I follow that I would have skipped if I'd been left to my own
devices?" If you can't answer, you didn't read enough of the existing
code first.
