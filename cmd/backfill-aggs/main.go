// cmd/backfill-aggs rebuilds analytics aggregates from order-service's
// orders and order_items rows.
package main

import (
	"context"
	"log/slog"
	"os"

	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/zrafat80/quickbite/analytics-service/app/analytics/repository"
	"github.com/zrafat80/quickbite/analytics-service/app/analytics/service"
	"github.com/zrafat80/quickbite/analytics-service/internal/backfill"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	cfg, err := backfill.ParseFlags(os.Args[1:], os.Stderr)
	if err != nil {
		log.Error("invalid arguments", "err", err.Error())
		os.Exit(2)
	}
	ctx := context.Background()

	pg, err := backfill.OpenPG(cfg)
	if err != nil {
		log.Error("postgres connect failed", "err", err.Error())
		os.Exit(1)
	}
	defer pg.Close()
	log.Info("postgres connected", "host", cfg.PGHost, "db", cfg.PGDB)

	mongoClient, err := mongo.Connect(ctx, options.Client().ApplyURI(cfg.MongoURI))
	if err != nil {
		log.Error("mongo connect failed", "err", err.Error())
		os.Exit(1)
	}
	defer mongoClient.Disconnect(context.Background())
	mongoDB := mongoClient.Database(cfg.MongoDB)
	log.Info("mongo connected", "database", cfg.MongoDB)

	if err := repository.EnsureIndexes(ctx, mongoDB); err != nil {
		log.Error("ensure indexes failed", "err", err.Error())
		os.Exit(1)
	}

	deduper := repository.NewEventIDsRepo(mongoDB)
	analyticsService := service.NewAnalyticsService(
		log,
		repository.NewAggRestaurantDayRepo(mongoDB),
		repository.NewAggBranchDayRepo(mongoDB),
		repository.NewAggProductDayRepo(mongoDB),
		repository.NewAggPlatformDayRepo(mongoDB),
	)

	stats := &backfill.Stats{}
	if err := backfill.Run(ctx, log, cfg, pg, deduper, analyticsService, stats); err != nil {
		log.Error("backfill failed", "err", err.Error())
		os.Exit(1)
	}

	log.Info("backfill complete",
		"orders_scanned", stats.Scanned(),
		"orders_replayed", stats.Replayed(),
		"orders_skipped_duplicate", stats.SkippedDuplicates(),
		"orders_failed", stats.Failed(),
		"dry_run", cfg.DryRun,
	)
}
