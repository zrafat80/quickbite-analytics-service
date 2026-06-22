// Package boot wires every singleton via uber-go/fx. main.go just calls Run.
//
// Why fx? The user asked for it. The trade-off vs. hand-rolled wiring is
// that providers express their dependencies in their signature and fx
// resolves the DAG; new singletons require only an fx.Provide, not edits to
// main.go. Lifecycle hooks (OnStart/OnStop) replace the OnModuleInit /
// OnApplicationShutdown pattern from Nest one-for-one.
//
// LAYERING: lib/boot is the ONE place allowed to import every layer (pkg,
// lib, app). Modules elsewhere never reach across — they import what they
// need by name.
package boot

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/go-chi/chi/v5"
	"go.mongodb.org/mongo-driver/mongo"
	"go.uber.org/fx"

	"github.com/zrafat80/quickbite/analytics-service/app/analytics"
	"github.com/zrafat80/quickbite/analytics-service/app/analytics/controller"
	"github.com/zrafat80/quickbite/analytics-service/app/analytics/eventhandlers"
	"github.com/zrafat80/quickbite/analytics-service/app/analytics/repository"
	"github.com/zrafat80/quickbite/analytics-service/app/analytics/service"
	"github.com/zrafat80/quickbite/analytics-service/lib/auth"
	"github.com/zrafat80/quickbite/analytics-service/lib/config"
	"github.com/zrafat80/quickbite/analytics-service/lib/coreclient"
	"github.com/zrafat80/quickbite/analytics-service/lib/coreevents"
	"github.com/zrafat80/quickbite/analytics-service/lib/logger"
	"github.com/zrafat80/quickbite/analytics-service/lib/middleware"
	"github.com/zrafat80/quickbite/analytics-service/lib/rbac"
	"github.com/zrafat80/quickbite/analytics-service/pkg/httpclient"
	"github.com/zrafat80/quickbite/analytics-service/pkg/messaging"
	pkgmongo "github.com/zrafat80/quickbite/analytics-service/pkg/mongo"
)

// Run boots the app. Blocks until SIGINT/SIGTERM, then runs OnStop hooks.
func Run() {
	app := fx.New(appOptions()...)

	startCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := app.Start(startCtx); err != nil {
		// fx prints the underlying error; bail cleanly.
		fmt.Fprintf(os.Stderr, "boot failed: %v\n", err)
		os.Exit(1)
	}
	<-app.Done()

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer stopCancel()
	if err := app.Stop(stopCtx); err != nil {
		fmt.Fprintf(os.Stderr, "shutdown error: %v\n", err)
	}
}

func appOptions() []fx.Option {
	return []fx.Option{
		fx.NopLogger,
		fx.Provide(
			// ── config + logger ───────────────────────────────────────
			provideConfig,
			provideLogger,

			// ── pkg/ providers ────────────────────────────────────────
			provideMongo,
			provideMessagingBroker,
			provideHTTPClient,

			// ── lib/ providers ────────────────────────────────────────
			provideJWTVerifier,
			provideCoreClient,
			provideRBACCache,

			// ── app/analytics providers ───────────────────────────────
			fx.Annotate(repository.NewAggRestaurantDayRepo, fx.As(new(analytics.RestaurantDayRepository))),
			fx.Annotate(repository.NewAggBranchDayRepo, fx.As(new(analytics.BranchDayRepository))),
			fx.Annotate(repository.NewAggProductDayRepo, fx.As(new(analytics.ProductDayRepository))),
			fx.Annotate(repository.NewAggPlatformDayRepo, fx.As(new(analytics.PlatformDayRepository))),
			repository.NewEventIDsRepo,
			service.NewAnalyticsService,
			controller.New,
			provideOrderEventHandlers,
			provideRBACEventHandlers,

			// ── coreevents consumers (order.events + core.events) ────
			provideConsumers,

			// ── HTTP wiring ───────────────────────────────────────────
			provideRouter,
			provideHTTPServer,
		),
		fx.Invoke(
			ensureMongoIndexes,
			startCoreEventsConsumer,
			startHTTPServer,
		),
	}
}

// ─── providers ──────────────────────────────────────────────────────────────

func provideConfig() (*config.Env, error) { return config.Load() }

func provideLogger(cfg *config.Env) *slog.Logger {
	l := logger.New(cfg.LogLevel)
	slog.SetDefault(l)
	return l
}

func provideMongo(lc fx.Lifecycle, cfg *config.Env, log *slog.Logger) (*mongo.Database, error) {
	client, db, err := pkgmongo.Connect(context.Background(), pkgmongo.Config{
		URI:      cfg.Mongo.URI,
		Database: cfg.Mongo.Database,
	})
	if err != nil {
		return nil, err
	}
	log.Info("mongo connected", "database", cfg.Mongo.Database)
	lc.Append(fx.Hook{
		OnStop: func(ctx context.Context) error {
			log.Info("mongo disconnecting")
			return pkgmongo.Disconnect(ctx, client)
		},
	})
	return db, nil
}

func provideMessagingBroker(lc fx.Lifecycle, cfg *config.Env, log *slog.Logger) (messaging.Broker, error) {
	b := messaging.NewAMQPBroker(messaging.Config{URL: cfg.Rabbit.URL})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := b.Connect(ctx); err != nil {
		// Best-effort connect: log + continue. Consumer.Start will fail loud
		// if the broker is still down at that point.
		log.Warn("rabbit unreachable at boot", "err", err.Error())
	} else {
		log.Info("rabbit connected")
	}
	lc.Append(fx.Hook{
		OnStop: func(ctx context.Context) error {
			log.Info("rabbit closing")
			return b.Close(ctx)
		},
	})
	return b, nil
}

func provideHTTPClient() *httpclient.Client {
	return httpclient.New(httpclient.Config{Timeout: 5 * time.Second, MaxRetries: 2})
}

func provideJWTVerifier(cfg *config.Env) *auth.Verifier {
	return auth.NewVerifier(cfg.Auth.AccessSecret)
}

func provideCoreClient(cfg *config.Env, h *httpclient.Client) *coreclient.Client {
	return coreclient.New(coreclient.Config{
		BaseURL:        cfg.Core.BaseURL,
		InternalAPIKey: cfg.Core.InternalAPIKey,
	}, h)
}

func provideRBACCache(cfg *config.Env, c *coreclient.Client) *rbac.Cache {
	return rbac.NewCache(c, cfg.Core.RBACCacheTTL)
}

// orderEventHandlers is the map handed to the order.events consumer —
// distinct named type so fx can resolve the dependency unambiguously when
// we add the rbac handler map.
type orderEventHandlers map[string]coreevents.EventHandler

// rbacEventHandlers is the map handed to the core.events consumer (RBAC
// cache invalidation).
type rbacEventHandlers map[string]coreevents.EventHandler

// Consumers is the bundle the startup hook receives. Wrapping the pair in
// one struct avoids needing fx.Annotate with name tags for each consumer.
type Consumers struct {
	OrderEvents *coreevents.Consumer
	CoreEvents  *coreevents.Consumer
}

func provideOrderEventHandlers(svc *service.AnalyticsService) orderEventHandlers {
	return eventhandlers.Build(svc)
}

func provideRBACEventHandlers(cache *rbac.Cache) rbacEventHandlers {
	return coreevents.BuildRBACHandlers(cache)
}

func provideConsumers(
	cfg *config.Env,
	broker messaging.Broker,
	deduper *repository.EventIDsRepo,
	orderHandlers orderEventHandlers,
	rbacHandlers rbacEventHandlers,
) *Consumers {
	orderCons := coreevents.NewConsumer(broker, deduper, messaging.ConsumerOptions{
		Exchange:           cfg.Rabbit.OrderEventsExchange,
		Queue:              cfg.Rabbit.OrderEventsQueue,
		BindingKeys:        cfg.Rabbit.OrderEventsBindings,
		DeadLetterExchange: cfg.Rabbit.OrderEventsDLX,
		DeadLetterQueue:    cfg.Rabbit.OrderEventsDLQ,
		Prefetch:           cfg.Rabbit.Prefetch,
	})
	for eventType, h := range orderHandlers {
		orderCons.Register(eventType, h)
	}

	rbacCons := coreevents.NewConsumer(broker, deduper, messaging.ConsumerOptions{
		Exchange:           cfg.Rabbit.CoreEventsExchange,
		Queue:              cfg.Rabbit.CoreEventsQueue,
		BindingKeys:        cfg.Rabbit.CoreEventsBindings,
		DeadLetterExchange: cfg.Rabbit.CoreEventsDLX,
		DeadLetterQueue:    cfg.Rabbit.CoreEventsDLQ,
		Prefetch:           cfg.Rabbit.Prefetch,
	})
	for eventType, h := range rbacHandlers {
		rbacCons.Register(eventType, h)
	}

	return &Consumers{OrderEvents: orderCons, CoreEvents: rbacCons}
}

// provideRouter assembles the chi router. The CONTROLLER LAYER mounts its
// own routes (see analytics/controller/routes.go) so this stays small and
// adding a new module is a one-line edit.
func provideRouter(
	log *slog.Logger,
	verifier *auth.Verifier,
	rbacCache *rbac.Cache,
	ctrl *controller.AnalyticsController,
) http.Handler {
	return NewRouter(log, verifier, rbacCache, ctrl)
}

// NewRouter exposes the production HTTP stack to integration tests and
// alternate process hosts without duplicating route or middleware wiring.
func NewRouter(
	log *slog.Logger,
	verifier *auth.Verifier,
	rbacCache *rbac.Cache,
	ctrl *controller.AnalyticsController,
) http.Handler {
	r := chi.NewRouter()

	// Global middleware — order matters: correlation first (logger setup),
	// access log next (sees the correlation_id), then routes.
	r.Use(middleware.Correlation(log))
	r.Use(middleware.AccessLog())

	r.Get("/health", healthHandler)

	r.Route("/api/v1", func(api chi.Router) {
		ctrl.MountRoutes(api, verifier, rbacCache)
	})

	return r
}

func healthHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"success":true,"data":{"status":"ok"}}`))
}

func provideHTTPServer(cfg *config.Env, h http.Handler) *http.Server {
	return &http.Server{
		Addr:              fmt.Sprintf(":%d", cfg.Port),
		Handler:           h,
		ReadHeaderTimeout: 5 * time.Second,
	}
}

// ─── invocations (start/stop hooks) ────────────────────────────────────────

func ensureMongoIndexes(lc fx.Lifecycle, db *mongo.Database, log *slog.Logger) {
	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			if err := repository.EnsureIndexes(ctx, db); err != nil {
				return fmt.Errorf("ensure indexes: %w", err)
			}
			log.Info("mongo indexes ensured")
			return nil
		},
	})
}

func startCoreEventsConsumer(lc fx.Lifecycle, cs *Consumers, log *slog.Logger) {
	// One cancel context for both consumers — shutdown cancels them together.
	consumerCtx, cancel := context.WithCancel(context.Background())
	lc.Append(fx.Hook{
		OnStart: func(_ context.Context) error {
			if err := cs.OrderEvents.Start(consumerCtx); err != nil {
				log.Warn("order events consumer failed to start", "err", err.Error())
			} else {
				log.Info("order events consumer started")
			}
			if err := cs.CoreEvents.Start(consumerCtx); err != nil {
				log.Warn("core events consumer failed to start", "err", err.Error())
			} else {
				log.Info("core events consumer started")
			}
			return nil
		},
		OnStop: func(_ context.Context) error {
			cancel()
			return nil
		},
	})
}

func startHTTPServer(lc fx.Lifecycle, srv *http.Server, log *slog.Logger) {
	lc.Append(fx.Hook{
		OnStart: func(_ context.Context) error {
			go func() {
				log.Info("http listening", "addr", srv.Addr)
				if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
					log.Error("http server failed", "err", err.Error())
				}
			}()
			return nil
		},
		OnStop: func(ctx context.Context) error {
			log.Info("http shutting down")
			return srv.Shutdown(ctx)
		},
	})
}
