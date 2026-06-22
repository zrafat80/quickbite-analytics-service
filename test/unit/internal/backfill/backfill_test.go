package backfill

import (
	"bytes"
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/zrafat80/quickbite/analytics-service/app/analytics"
	. "github.com/zrafat80/quickbite/analytics-service/internal/backfill"
)

type backfillDeduperFake struct {
	fresh     bool
	err       error
	forgetErr error
	seen      []string
	forgotten []string
}

func (f *backfillDeduperFake) MarkSeen(_ context.Context, eventID string) (bool, error) {
	f.seen = append(f.seen, eventID)
	return f.fresh, f.err
}

func (f *backfillDeduperFake) Forget(_ context.Context, eventID string) error {
	f.forgotten = append(f.forgotten, eventID)
	return f.forgetErr
}

type aggregateWriterFake struct {
	placed    []analytics.OnOrderPlacedInput
	rejected  []analytics.OnOrderRejectedInput
	delivered []analytics.OnOrderDeliveredInput
	err       error
}

type queryScript struct {
	orderRows   driver.Rows
	itemRows    driver.Rows
	orderErr    error
	itemErr     error
	itemQueries atomic.Int32
}

type scriptedDriver struct {
	script *queryScript
}

func (d scriptedDriver) Open(string) (driver.Conn, error) {
	return &scriptedConnection{script: d.script}, nil
}

type scriptedConnection struct {
	script *queryScript
}

func (*scriptedConnection) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("prepare not supported")
}
func (*scriptedConnection) Close() error { return nil }
func (*scriptedConnection) Begin() (driver.Tx, error) {
	return nil, errors.New("transactions not supported")
}

func (c *scriptedConnection) QueryContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Rows, error) {
	if strings.Contains(query, "FROM orders") {
		return c.script.orderRows, c.script.orderErr
	}
	if strings.Contains(query, "FROM order_items") {
		c.script.itemQueries.Add(1)
		return cloneRows(c.script.itemRows), c.script.itemErr
	}
	return nil, errors.New("unexpected query")
}

type staticRows struct {
	columns []string
	values  [][]driver.Value
	index   int
}

func (r *staticRows) Columns() []string { return r.columns }
func (r *staticRows) Close() error      { return nil }
func (r *staticRows) Next(destination []driver.Value) error {
	if r.index >= len(r.values) {
		return io.EOF
	}
	copy(destination, r.values[r.index])
	r.index++
	return nil
}

func cloneRows(rows driver.Rows) driver.Rows {
	typed, ok := rows.(*staticRows)
	if !ok {
		return rows
	}
	return &staticRows{columns: append([]string(nil), typed.columns...), values: typed.values}
}

var driverSequence atomic.Int64

func openScriptedDB(t *testing.T, script *queryScript) *sql.DB {
	t.Helper()
	name := "backfill_test_" + Int64Array([]int64{driverSequence.Add(1)})
	sql.Register(name, scriptedDriver{script: script})
	db, err := sql.Open(name, "")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func (f *aggregateWriterFake) OnOrderPlaced(_ context.Context, input analytics.OnOrderPlacedInput) error {
	f.placed = append(f.placed, input)
	return f.err
}

func (f *aggregateWriterFake) OnOrderRejected(_ context.Context, input analytics.OnOrderRejectedInput) error {
	f.rejected = append(f.rejected, input)
	return f.err
}

func (f *aggregateWriterFake) OnOrderDelivered(_ context.Context, input analytics.OnOrderDeliveredInput) error {
	f.delivered = append(f.delivered, input)
	return f.err
}

func testBackfillLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestParseFlags(t *testing.T) {
	var output bytes.Buffer
	parsed, err := ParseFlags([]string{
		"--from-date=2026-06-01",
		"--to-date=2026-06-30",
		"--batch=25",
		"--dry-run",
	}, &output)
	require.NoError(t, err)
	assert.Equal(t, "localhost", parsed.PGHost)
	assert.Equal(t, 5432, parsed.PGPort)
	assert.Equal(t, "eg", parsed.Region)
	assert.Equal(t, 25, parsed.BatchSize)
	assert.True(t, parsed.DryRun)

	for _, test := range []struct {
		name string
		args []string
	}{
		{"missing dates", nil},
		{"bad from", []string{"--from-date=bad", "--to-date=2026-06-30"}},
		{"bad to", []string{"--from-date=2026-06-01", "--to-date=bad"}},
		{"reversed", []string{"--from-date=2026-07-01", "--to-date=2026-06-30"}},
		{"bad batch", []string{"--from-date=2026-06-01", "--to-date=2026-06-30", "--batch=0"}},
		{"unknown flag", []string{"--unknown"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := ParseFlags(test.args, io.Discard)
			require.Error(t, err)
		})
	}
}

func TestInt64Array(t *testing.T) {
	assert.Equal(t, "{}", Int64Array(nil))
	assert.Equal(t, "{1,-2,300}", Int64Array([]int64{1, -2, 300}))
}

func TestReplayWithDedupe(t *testing.T) {
	t.Run("dry run does not reserve an event", func(t *testing.T) {
		deduper := &backfillDeduperFake{}
		stats := &Stats{}
		called := false
		require.NoError(t, ReplayWithDedupe(context.Background(), testBackfillLogger(), Config{DryRun: true}, deduper,
			"event-1", "placed", func() error { called = true; return nil }, stats))
		assert.False(t, called)
		assert.Empty(t, deduper.seen)
		assert.Equal(t, 1, stats.Replayed())
	})

	t.Run("duplicate skips apply", func(t *testing.T) {
		deduper := &backfillDeduperFake{fresh: false}
		stats := &Stats{}
		called := false
		require.NoError(t, ReplayWithDedupe(context.Background(), testBackfillLogger(), Config{}, deduper,
			"event-1", "placed", func() error { called = true; return nil }, stats))
		assert.False(t, called)
		assert.Equal(t, 1, stats.SkippedDuplicates())
	})

	t.Run("successful apply increments replayed", func(t *testing.T) {
		deduper := &backfillDeduperFake{fresh: true}
		stats := &Stats{}
		require.NoError(t, ReplayWithDedupe(context.Background(), testBackfillLogger(), Config{}, deduper,
			"event-1", "placed", func() error { return nil }, stats))
		assert.Equal(t, 1, stats.Replayed())
	})

	t.Run("dedupe and apply failures are propagated", func(t *testing.T) {
		deduper := &backfillDeduperFake{err: errors.New("dedupe")}
		err := ReplayWithDedupe(context.Background(), testBackfillLogger(), Config{}, deduper,
			"event-1", "placed", func() error { return nil }, &Stats{})
		assert.Contains(t, err.Error(), "dedupe")

		deduper = &backfillDeduperFake{fresh: true}
		err = ReplayWithDedupe(context.Background(), testBackfillLogger(), Config{}, deduper,
			"event-1", "placed", func() error { return errors.New("apply") }, &Stats{})
		assert.EqualError(t, err, "apply")
		assert.Equal(t, []string{"event-1"}, deduper.forgotten)

		deduper = &backfillDeduperFake{fresh: true, forgetErr: errors.New("forget")}
		err = ReplayWithDedupe(context.Background(), testBackfillLogger(), Config{}, deduper,
			"event-1", "placed", func() error { return errors.New("apply") }, &Stats{})
		assert.Contains(t, err.Error(), "release dedupe marker")
	})
}

func TestRunBackfillBatchesOrdersAndItems(t *testing.T) {
	createdOne := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	createdTwo := time.Date(2026, 6, 2, 10, 0, 0, 0, time.UTC)
	rejected := createdOne.Add(5 * time.Minute)
	delivered := createdOne.Add(30 * time.Minute)
	script := &queryScript{
		orderRows: &staticRows{
			columns: []string{
				"id", "public_id", "region", "country_code", "restaurant_id", "branch_id",
				"currency", "total", "created_at", "rejected_at", "delivered_at",
			},
			values: [][]driver.Value{
				{int64(1), "order-1", "eg", "EG", int64(10), int64(20), "EGP", int64(1500), createdOne, rejected, delivered},
				{int64(2), "order-2", "eg", "EG", int64(11), int64(21), "EGP", int64(700), createdTwo, nil, nil},
			},
		},
		itemRows: &staticRows{
			columns: []string{"order_id", "order_created_at", "product_id", "quantity", "line_total"},
			values: [][]driver.Value{
				{int64(1), createdOne, int64(100), int64(2), int64(1500)},
				{int64(2), createdTwo, int64(101), int64(1), int64(700)},
			},
		},
	}
	db := openScriptedDB(t, script)
	deduper := &backfillDeduperFake{fresh: true}
	writer := &aggregateWriterFake{}
	stats := &Stats{}
	err := Run(context.Background(), testBackfillLogger(), Config{
		Region: "eg", FromDate: "2026-06-01", ToDate: "2026-06-30", BatchSize: 1,
	}, db, deduper, writer, stats)
	require.NoError(t, err)
	assert.Equal(t, 2, stats.Scanned())
	assert.Equal(t, 4, stats.Replayed())
	assert.Equal(t, int32(2), script.itemQueries.Load())
	require.Len(t, writer.placed, 2)
	assert.Equal(t, int64(100), writer.placed[0].Items[0].ProductID)
	assert.Equal(t, int64(101), writer.placed[1].Items[0].ProductID)
}

func TestRunBackfillAndFetchItemsErrors(t *testing.T) {
	t.Run("order scan failure", func(t *testing.T) {
		db := openScriptedDB(t, &queryScript{orderErr: errors.New("scan unavailable")})
		err := Run(context.Background(), testBackfillLogger(), Config{
			FromDate: "2026-06-01", ToDate: "2026-06-30", BatchSize: 10,
		}, db, &backfillDeduperFake{}, &aggregateWriterFake{}, &Stats{})
		assert.Contains(t, err.Error(), "scan orders")
	})

	t.Run("item query failure", func(t *testing.T) {
		created := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
		db := openScriptedDB(t, &queryScript{
			orderRows: &staticRows{
				columns: []string{
					"id", "public_id", "region", "country_code", "restaurant_id", "branch_id",
					"currency", "total", "created_at", "rejected_at", "delivered_at",
				},
				values: [][]driver.Value{{int64(1), "order-1", "eg", "EG", int64(10), int64(20), "EGP", int64(1), created, nil, nil}},
			},
			itemErr: errors.New("items unavailable"),
		})
		err := Run(context.Background(), testBackfillLogger(), Config{
			FromDate: "2026-06-01", ToDate: "2026-06-30", BatchSize: 10,
		}, db, &backfillDeduperFake{}, &aggregateWriterFake{}, &Stats{})
		assert.Contains(t, err.Error(), "fetch items")
	})
}
