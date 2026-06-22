// Package backfill contains the testable implementation behind
// cmd/backfill-aggs. It rebuilds analytics aggregates from order-service's
// orders and order_items rows.
//
// Usage:
//
//	go run ./cmd/backfill-aggs \
//	    --pg-host=localhost --pg-port=5432 \
//	    --pg-user=postgres --pg-pass=zeyiad123123 --pg-db=order_service_eg \
//	    --from-date=2026-01-01 --to-date=2026-05-21 \
//	    --mongo-uri="mongodb://admin:secret@localhost:27017/?authSource=admin" \
//	    --mongo-db=analytics
//
// Add --dry-run to log what would be written without touching mongo.
//
// Idempotency: each (region, orderId) becomes a synthetic eventId of the
// form "backfill:<region>:<orderId>". MarkSeen on the existing event_ids
// collection rejects duplicates, so re-running over the same window is a
// no-op for orders already counted (live or by a previous backfill).
package backfill

import (
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"time"

	_ "github.com/lib/pq"

	"github.com/zrafat80/quickbite/analytics-service/app/analytics"
)

type Config struct {
	PGHost, PGUser, PGPass, PGDB string
	PGPort                       int
	Region                       string
	FromDate, ToDate             string
	MongoURI, MongoDB            string
	BatchSize                    int
	DryRun                       bool
}

func ParseFlags(args []string, output io.Writer) (Config, error) {
	f := Config{}
	set := flag.NewFlagSet("backfill-aggs", flag.ContinueOnError)
	set.SetOutput(output)
	set.StringVar(&f.PGHost, "pg-host", "localhost", "postgres host")
	set.IntVar(&f.PGPort, "pg-port", 5432, "postgres port")
	set.StringVar(&f.PGUser, "pg-user", "postgres", "postgres user")
	set.StringVar(&f.PGPass, "pg-pass", "", "postgres password")
	set.StringVar(&f.PGDB, "pg-db", "order_service_eg", "postgres database")
	set.StringVar(&f.Region, "region", "eg", "region tag stamped into orders table")
	set.StringVar(&f.FromDate, "from-date", "", "inclusive YYYY-MM-DD (placement date, UTC)")
	set.StringVar(&f.ToDate, "to-date", "", "inclusive YYYY-MM-DD (placement date, UTC)")
	set.StringVar(&f.MongoURI, "mongo-uri", "mongodb://localhost:27017", "mongo uri")
	set.StringVar(&f.MongoDB, "mongo-db", "analytics", "mongo database")
	set.IntVar(&f.BatchSize, "batch", 500, "rows fetched per page")
	set.BoolVar(&f.DryRun, "dry-run", false, "scan + log without writing to mongo")
	if err := set.Parse(args); err != nil {
		return Config{}, err
	}

	if f.FromDate == "" || f.ToDate == "" {
		return Config{}, fmt.Errorf("--from-date and --to-date are required")
	}
	from, err := time.Parse("2006-01-02", f.FromDate)
	if err != nil {
		return Config{}, fmt.Errorf("bad --from-date: %w", err)
	}
	to, err := time.Parse("2006-01-02", f.ToDate)
	if err != nil {
		return Config{}, fmt.Errorf("bad --to-date: %w", err)
	}
	if from.After(to) {
		return Config{}, fmt.Errorf("--from-date must be on or before --to-date")
	}
	if f.BatchSize <= 0 {
		return Config{}, fmt.Errorf("--batch must be greater than zero")
	}
	return f, nil
}

func OpenPG(f Config) (*sql.DB, error) {
	dsn := fmt.Sprintf(
		"host=%s port=%d user=%s password=%s dbname=%s sslmode=disable",
		f.PGHost, f.PGPort, f.PGUser, f.PGPass, f.PGDB,
	)
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(4)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping: %w", err)
	}
	return db, nil
}

type Stats struct {
	scanned, replayed, skippedDup, failed int
}

func (s *Stats) Scanned() int           { return s.scanned }
func (s *Stats) Replayed() int          { return s.replayed }
func (s *Stats) SkippedDuplicates() int { return s.skippedDup }
func (s *Stats) Failed() int            { return s.failed }

type orderRow struct {
	publicID     string
	region       string
	countryCode  string
	restaurantID int64
	branchID     int64
	currency     string
	total        int64
	createdAt    time.Time
	rejectedAt   sql.NullTime
	deliveredAt  sql.NullTime
}

type itemRow struct {
	orderID        int64
	orderCreatedAt time.Time
	productID      int64
	quantity       int64
	lineTotal      int64
}

type backfillDeduper interface {
	MarkSeen(ctx context.Context, eventID string) (bool, error)
	Forget(ctx context.Context, eventID string) error
}

type aggregateWriter interface {
	OnOrderPlaced(ctx context.Context, in analytics.OnOrderPlacedInput) error
	OnOrderRejected(ctx context.Context, in analytics.OnOrderRejectedInput) error
	OnOrderDelivered(ctx context.Context, in analytics.OnOrderDeliveredInput) error
}

func Run(
	ctx context.Context,
	log *slog.Logger,
	f Config,
	pg *sql.DB,
	deduper backfillDeduper,
	svc aggregateWriter,
	stats *Stats,
) error {
	// Scan orders in (placed-at-day) window. We use created_at as the
	// placement timestamp — matches what the live publisher writes.
	rows, err := pg.QueryContext(ctx, `
		SELECT id, public_id, region, country_code, restaurant_id, branch_id,
		       currency, total, created_at, rejected_at, delivered_at
		FROM orders
		WHERE created_at >= $1::date
		  AND created_at <  ($2::date + INTERVAL '1 day')
		ORDER BY id ASC`, f.FromDate, f.ToDate)
	if err != nil {
		return fmt.Errorf("scan orders: %w", err)
	}
	defer rows.Close()

	// Buffer (id, row) pairs so we can batch-fetch items.
	type pair struct {
		id    int64
		order orderRow
	}
	batch := make([]pair, 0, f.BatchSize)

	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		ids := make([]int64, 0, len(batch))
		for _, p := range batch {
			ids = append(ids, p.id)
		}
		itemsByOrder, err := fetchItems(ctx, pg, ids)
		if err != nil {
			return fmt.Errorf("fetch items: %w", err)
		}
		for _, p := range batch {
			if err := replayOne(ctx, log, f, deduper, svc, p.order, itemsByOrder[p.id], stats); err != nil {
				stats.failed++
				log.Warn("replay failed", "orderId", p.order.publicID, "err", err.Error())
			}
		}
		batch = batch[:0]
		return nil
	}

	for rows.Next() {
		var (
			id int64
			o  orderRow
		)
		if err := rows.Scan(
			&id, &o.publicID, &o.region, &o.countryCode,
			&o.restaurantID, &o.branchID, &o.currency, &o.total,
			&o.createdAt, &o.rejectedAt, &o.deliveredAt,
		); err != nil {
			return fmt.Errorf("scan row: %w", err)
		}
		stats.scanned++
		batch = append(batch, pair{id: id, order: o})
		if len(batch) >= f.BatchSize {
			if err := flush(); err != nil {
				return err
			}
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	return flush()
}

func fetchItems(ctx context.Context, pg *sql.DB, orderIDs []int64) (map[int64][]itemRow, error) {
	if len(orderIDs) == 0 {
		return map[int64][]itemRow{}, nil
	}
	// Single round-trip via ANY($1) — CLAUDE.md §9 #1: no per-iteration reads.
	rows, err := pg.QueryContext(ctx, `
		SELECT order_id, order_created_at, product_id, quantity, line_total
		FROM order_items
		WHERE order_id = ANY($1::bigint[])`, Int64Array(orderIDs))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[int64][]itemRow, len(orderIDs))
	for rows.Next() {
		var it itemRow
		if err := rows.Scan(&it.orderID, &it.orderCreatedAt, &it.productID, &it.quantity, &it.lineTotal); err != nil {
			return nil, err
		}
		out[it.orderID] = append(out[it.orderID], it)
	}
	return out, rows.Err()
}

// Int64Array marshals an []int64 into a Postgres-array literal (lib/pq
// doesn't ship a parameterizable typed-array binder for ints; the literal
// form sidesteps it).
func Int64Array(in []int64) string {
	b := []byte{'{'}
	for i, n := range in {
		if i > 0 {
			b = append(b, ',')
		}
		b = appendInt(b, n)
	}
	b = append(b, '}')
	return string(b)
}

func appendInt(b []byte, n int64) []byte {
	return []byte(fmt.Sprintf("%s%d", string(b), n))
}

func replayOne(
	ctx context.Context,
	log *slog.Logger,
	f Config,
	deduper backfillDeduper,
	svc aggregateWriter,
	o orderRow,
	items []itemRow,
	stats *Stats,
) error {
	itemInputs := make([]analytics.OrderItemInput, 0, len(items))
	for _, it := range items {
		itemInputs = append(itemInputs, analytics.OrderItemInput{
			ProductID: it.productID,
			Quantity:  it.quantity,
			LineTotal: it.lineTotal,
		})
	}

	// PLACED replay — dedupe key is order-specific so re-running is safe.
	placedID := fmt.Sprintf("backfill:%s:%s:placed", o.region, o.publicID)
	if err := ReplayWithDedupe(ctx, log, f, deduper, placedID, "order.placed", func() error {
		return svc.OnOrderPlaced(ctx, analytics.OnOrderPlacedInput{
			OrderID:      o.publicID,
			RestaurantID: o.restaurantID,
			BranchID:     o.branchID,
			CountryCode:  o.countryCode,
			Currency:     o.currency,
			Total:        o.total,
			PlacedAt:     o.createdAt,
			Items:        itemInputs,
		})
	}, stats); err != nil {
		return err
	}

	if o.rejectedAt.Valid {
		rejectedID := fmt.Sprintf("backfill:%s:%s:rejected", o.region, o.publicID)
		if err := ReplayWithDedupe(ctx, log, f, deduper, rejectedID, "order.rejected", func() error {
			return svc.OnOrderRejected(ctx, analytics.OnOrderRejectedInput{
				OrderID:      o.publicID,
				RestaurantID: o.restaurantID,
				BranchID:     o.branchID,
				Currency:     o.currency,
				RejectedAt:   o.rejectedAt.Time,
			})
		}, stats); err != nil {
			return err
		}
	}

	if o.deliveredAt.Valid {
		deliveredID := fmt.Sprintf("backfill:%s:%s:delivered", o.region, o.publicID)
		if err := ReplayWithDedupe(ctx, log, f, deduper, deliveredID, "order.delivered", func() error {
			return svc.OnOrderDelivered(ctx, analytics.OnOrderDeliveredInput{
				OrderID:      o.publicID,
				RestaurantID: o.restaurantID,
				BranchID:     o.branchID,
				Currency:     o.currency,
				DeliveredAt:  o.deliveredAt.Time,
				DeliveryMs:   o.deliveredAt.Time.Sub(o.createdAt).Milliseconds(),
			})
		}, stats); err != nil {
			return err
		}
	}
	return nil
}

func ReplayWithDedupe(
	ctx context.Context,
	log *slog.Logger,
	f Config,
	deduper backfillDeduper,
	eventID, label string,
	apply func() error,
	stats *Stats,
) error {
	if f.DryRun {
		b, _ := json.Marshal(map[string]any{"label": label, "eventId": eventID})
		log.Info("dry-run", "row", string(b))
		stats.replayed++
		return nil
	}
	fresh, err := deduper.MarkSeen(ctx, eventID)
	if err != nil {
		return fmt.Errorf("dedupe: %w", err)
	}
	if !fresh {
		stats.skippedDup++
		return nil
	}
	if err := apply(); err != nil {
		if releaseErr := deduper.Forget(ctx, eventID); releaseErr != nil {
			return fmt.Errorf("apply: %w; release dedupe marker: %v", err, releaseErr)
		}
		return err
	}
	stats.replayed++
	return nil
}
