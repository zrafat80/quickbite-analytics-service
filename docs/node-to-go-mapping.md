# node-to-go-mapping.md — the teaching doc

Most of what you've already learned in `core-service` and `order-service`
still applies. What changes is the syntax and a few idioms. This doc walks
through every layer of the service with a Node example on the left and the
Go translation on the right.

If you remember one sentence: **classes become structs, decorators become
struct tags, `Promise<T>` becomes `(T, error)`, DI providers become fx
providers.**

---

## 1. Module shape

| TypeScript (Nest) | Go (this repo) |
| --- | --- |
| `@Controller`, `@Injectable`, `@Module` | plain structs + constructors |
| Decorators on class methods | struct tags + chi router middleware |
| `OnModuleInit` / `OnApplicationShutdown` | `fx.Hook{OnStart, OnStop}` |

### Nest version

```ts
@Injectable()
export class OrderService {
  constructor(
    @Inject('KNEX_CONNECTION') private readonly knex: ShardedKnex,
    private readonly orderRepo: OrderRepository,
  ) {}
}
```

### Go version

```go
type AnalyticsService struct {
    dayRepo *repository.AggRestaurantDayRepo
}

func NewAnalyticsService(dayRepo *repository.AggRestaurantDayRepo) *AnalyticsService {
    return &AnalyticsService{dayRepo: dayRepo}
}
```

`fx.Provide(service.NewAnalyticsService)` in `lib/boot/boot.go` is the
analogue of `providers: [OrderService]` in `@Module(...)`.

---

## 2. Config

| Node | Go |
| --- | --- |
| `@nestjs/config` + `appConfig()` factory | `caarlos0/env/v11` struct tags |
| `ConfigService.get<string>('rabbit.url')` | `cfg.Rabbit.URL` |
| Required vars throw at boot | `env:"X,required"` |

### Node

```ts
export default () => ({
  port: parseInt(process.env.PORT || '4000', 10),
  rabbit: {
    url: process.env.RABBITMQ_URL as string,
    exchange: process.env.RABBITMQ_CORE_EVENTS_EXCHANGE || 'core.events',
  },
});
```

### Go

```go
type Env struct {
    Port   int    `env:"PORT" envDefault:"5000"`
    Rabbit struct {
        URL      string `env:"RABBITMQ_URL,required"`
        Exchange string `env:"RABBITMQ_ORDER_EVENTS_EXCHANGE" envDefault:"order.events"`
    }
}
```

One struct, one `Load()`, passed by pointer to every constructor that
needs it. No service-locator pattern.

---

## 3. Logger

| Node | Go |
| --- | --- |
| `class Logger` from `@nestjs/common` | stdlib `log/slog` |
| `this.logger.log(...)` | `slog.Default().Info(...)` or `logger.FromContext(ctx).Info(...)` |
| Auto-injected `Logger` | request-scoped child stored on ctx |

### Node

```ts
private readonly logger = new Logger(OrderService.name);
this.logger.log(`order placed ${id}`);
```

### Go

```go
log := logger.FromContext(ctx)
log.Info("order placed", "id", id)
```

`slog` is structured (key/value), JSON by default. The `correlation_id`
field is set once by middleware; every log line in the request carries it
automatically.

---

## 4. Auth

| Node | Go |
| --- | --- |
| `jsonwebtoken` + `JwtAuthGuard` | `golang-jwt/jwt/v5` + `auth.Require` middleware |
| `request.user = payload` | `appcontext.WithClaims(ctx, claims)` |

### Node

```ts
@UseGuards(JwtAuthGuard)
@Get(':id')
async getOrder(@Param('id') id: string, @Req() req: Request) {
  const userId = req.user.userId;
  ...
}
```

### Go

```go
r.Use(auth.Require(verifier))

func (c *Controller) Get(w http.ResponseWriter, r *http.Request) error {
    claims, _ := appcontext.ClaimsFrom(r.Context())
    userID := claims.UserID
    ...
}
```

Cookie name (`access_token`), token shape (`userId`, `email`, `role`,
`restaurantId?`, `restaurantRole?`, `branchIds?`), and signing algorithm
(HS256) match core/order exactly.

---

## 5. RBAC

| Node | Go |
| --- | --- |
| `PermissionCacheService` + `PermissionsGuard` | `rbac.Cache` + `rbac.Require` |
| `@RequirePermissions('orders', 'create')` | `rbac.Require(cache, "orders:create")` |

### Node

```ts
@UseGuards(JwtAuthGuard, PermissionsGuard)
@RequirePermissions('analytics', 'read')
async getDays(...) {}
```

### Go

```go
r.Use(auth.Require(verifier))
r.Use(rbac.Require(rbacCache, "analytics:read"))
r.Get("/restaurants/{id}/days", apperr.Wrap(c.GetRestaurantDays))
```

Permission strings are dotted `<resource>:<action>` in both languages.

---

## 6. Errors

| Node | Go |
| --- | --- |
| `NotFoundException`, `ForbiddenException`, … | `*AppError{Code, Status, Message}` |
| `MODULE_ERRORS.NOT_FOUND` const | `var ErrNotFound = apperr.New(...)` |
| Global `HttpExceptionFilter` | `apperr.Wrap(handler)` middleware |

### Node

```ts
throw new NotFoundException(ORDER_ERRORS.NOT_FOUND);
```

### Go

```go
return analytics.ErrInvalidDateRange
// or with context
return analytics.ErrValidation.WithCause(err)
```

Wrap is the secret sauce — without it, every handler would need
`apperr.Render(w, err); return` at every failure point. With it,
handlers `return err` and the middleware does the rest.

---

## 7. DTOs

| Node | Go |
| --- | --- |
| `class-validator` decorators | `go-playground/validator/v10` struct tags |
| `@IsString()` `@IsInt({min:1})` | `validate:"required,min=1"` |
| Response factory `.from(entity)` | package-level `FromRow(...)` / `FromRows(...)` |

### Node

```ts
export class CreateOrderRequestDTO {
  @IsInt() @Min(1) branchId!: number;
  @IsArray() @ValidateNested({each: true})
  @Type(() => OrderItemDTO) items!: OrderItemDTO[];
}
```

### Go

```go
type RestaurantDaysQuery struct {
    From string `validate:"required,len=10"`
    To   string `validate:"required,len=10"`
}
```

Validation is invoked explicitly via `validator.New().Struct(q)`. There's
no global pipe in Go — but each controller has exactly one validator
instance.

---

## 8. Repositories

| Node | Go |
| --- | --- |
| Knex injected via `@Inject('KNEX_CONNECTION')` | mongo-driver `*mongo.Collection` injected via constructor |
| Raw SQL with `<MODULE>_COLUMNS` | typed `bson.D` / `bson.M` filters and `bson` tags on entity structs |
| `private toEntity(row)` snake → camel | `bson` tags do it for free |
| Trx threaded as `trx?: Knex.Transaction` | Mongo's single-doc updates are atomic; multi-doc trx exists but isn't needed in the slice |

### Node

```ts
async findById(id: string): Promise<OrderEntity | null> {
  const row = await this.knex(ORDERS_TABLE).select(ORDER_COLUMNS).where({id}).first();
  return row ? this.toEntity(row) : null;
}
```

### Go

```go
func (r *AggRestaurantDayRepo) FindByRestaurantInRange(
    ctx context.Context, restaurantID int64, from, to string,
) ([]entity.AggRestaurantDay, error) {
    filter := bson.M{"restaurant_id": restaurantID,
                     "date": bson.M{"$gte": from, "$lte": to}}
    cur, err := r.coll.Find(ctx, filter, options.Find().SetSort(bson.D{{Key: "date", Value: 1}}))
    if err != nil { return nil, err }
    defer cur.Close(ctx)

    out := make([]entity.AggRestaurantDay, 0)
    for cur.Next(ctx) {
        var row entity.AggRestaurantDay
        if err := cur.Decode(&row); err != nil { return nil, err }
        out = append(out, row)
    }
    return out, cur.Err()
}
```

`bson` tags on the struct are the analogue of column constants + the
`toEntity` mapper.

---

## 9. Services

| Node | Go |
| --- | --- |
| `async method(): Promise<T>` | `func (s *Svc) Method(ctx context.Context, ...) (T, error)` |
| Throws `HttpException` | returns `*AppError` |
| `await` chained | explicit `if err != nil { return ..., err }` |

### Node

```ts
async getOrder(user: AuthenticatedUser, region: string, publicId: string) {
  const order = await this.orderRepo.findByPublicId(region, publicId);
  if (!order) throw new NotFoundException(ORDER_ERRORS.NOT_FOUND);
  return order;
}
```

### Go

```go
func (s *AnalyticsService) QueryRestaurantDays(
    ctx context.Context, q analytics.RestaurantDayQuery,
) ([]analytics.RestaurantDayRow, error) {
    if err := validateRange(q.From, q.To); err != nil { return nil, err }
    rows, err := s.dayRepo.FindByRestaurantInRange(ctx, q.RestaurantID, q.From, q.To)
    if err != nil { return nil, err }
    ...
}
```

---

## 10. Controllers / routes

| Node | Go |
| --- | --- |
| `@Controller('orders')` + `@Get(':id')` | chi `r.Route("/orders", ...) ; r.Get("/{id}", ...)` |
| Method auto-async, returns DTO | handler `func(w, r) error` wrapped by `apperr.Wrap` |
| Validation via global `ValidationPipe` | explicit `validator.Struct(q)` |

### Node

```ts
@Controller('orders')
export class OrderController {
  @Get(':id')
  async getOrder(@Param('id') id: string) {
    return OrderResponseDTO.from(...);
  }
}
```

### Go

```go
func (c *AnalyticsController) MountRoutes(r chi.Router, v *auth.Verifier, cache *rbac.Cache) {
    r.Route("/analytics", func(rt chi.Router) {
        rt.Use(auth.Require(v))
        rt.Use(rbac.Require(cache, analytics.PermAnalyticsRead))
        rt.Method(http.MethodGet, "/restaurants/{restaurantId}/days",
            apperr.Wrap(c.GetRestaurantDays))
    })
}
```

Handlers exposed as `http.Handler` (via `apperr.Wrap`, which returns
`http.HandlerFunc`) so the router stays flexible. You can swap chi for
anything else without rewriting the controller.

---

## 11. Messaging (consumer)

| Node | Go |
| --- | --- |
| `amqp-connection-manager` with channel wrappers | `amqp091-go` with a small wrapper in `pkg/messaging` |
| Consumer module `OnModuleInit` | `Consumer.Start(ctx)` triggered by an fx OnStart hook |
| Redis SETNX dedupe | Mongo unique-index dedupe (see system-design.md for why) |

### Node

```ts
await broker.declareTopology(topology);
await broker.consume(topology, (msg) => this.handle(msg));
```

### Go

```go
if err := c.broker.DeclareTopology(ctx, c.topology); err != nil { return err }
return c.broker.Consume(ctx, c.topology, c.dispatch)
```

---

## 12. DI

| Node | Go |
| --- | --- |
| `@Module({providers: [...], imports: [...], exports: [...]})` | `fx.Provide(NewX, NewY, ...)` |
| `@Inject('TOKEN')` | constructor parameter type |
| `OnModuleInit` / `OnApplicationShutdown` | `fx.Hook{OnStart, OnStop}` |

### Node

```ts
@Module({
  providers: [OrderService, OrderRepository],
  exports: [OrderService],
})
export class OrderModule {}
```

### Go

```go
fx.Provide(
    repository.NewAggRestaurantDayRepo,
    repository.NewEventIDsRepo,
    service.NewAnalyticsService,
    controller.New,
)
```

All providers live in `lib/boot/boot.go`. The "module boundary" in Nest is
the `@Module` declaration; in Go we keep it implicit (the parent module
package).

---

## Common gotchas when porting

1. **`null` vs the zero value.** TypeScript has `null` / `undefined`. Go
   has only the zero value (`0`, `""`, `nil`). When optional, use pointers
   (`*int64`) — see `appcontext.Claims.RestaurantID`. Don't sprinkle
   pointers for non-optional fields.
2. **`Promise.all` vs goroutines.** The slice doesn't need parallelism,
   but when you do: `errgroup.Group` + `eg.Go(...)`. Don't reach for raw
   `go func()` — it leaks errors and you can't `Wait()` cleanly.
3. **Mongo IDs.** Don't fight Mongo on `_id`; either let it auto-generate
   or store your own field (`event_id`) and put the unique index on that.
4. **String dates.** We store `YYYY-MM-DD` as a Mongo string because
   string comparison is lexicographic and the field is human-readable in
   `mongosh`. Trade-off: range queries cast `from`/`to` to strings too.
   If you ever store local-time dates, you'll regret it — keep UTC.
5. **`error` is a value, not an exception.** Wrap with `fmt.Errorf("... :
   %w", err)` to keep the chain. Use `errors.As` to extract the typed
   error you care about (we do this in `errors.As(err, &ae)`).
6. **`defer cur.Close(ctx)` after every Find.** Cursors hold resources;
   leaks show up as connection-pool starvation in production. The Node
   side never had to think about this because Knex returns arrays.
7. **No `class-transformer`.** `bson` tags handle DB ↔ struct;
   `json` tags handle struct ↔ wire; `validate` tags handle wire ↔
   accepted. Three distinct concerns, three distinct tag namespaces.
8. **Don't fight `chi`.** Path params via `chi.URLParam(r, "name")`;
   middleware composes via `r.Use(...)`. No `@Param` magic.

---

## When to break the mapping

The mapping is a guide, not a contract. Break it when:

- **A Node pattern relies on metadata reflection (decorators).** Go
  doesn't have that. The closest analogue is struct tags + a runtime
  validator; if a pattern needs full runtime reflection (`class-validator`
  custom decorators, AOP-style interceptors), you'll need to redesign,
  not translate.
- **A Node pattern uses a global singleton via a service-locator.** In
  Go, prefer passing the dependency explicitly. fx makes this cheap.
- **A Node pattern composes Promises.** Goroutines are cheap, but they
  also share memory. Don't reach for them without an `errgroup` or a
  clear single-owner.
- **A Node pattern stores state on `req`.** Use ctx keys via a typed
  helper (`appcontext.WithClaims` / `appcontext.ClaimsFrom`). Don't
  encode untyped maps onto ctx.
