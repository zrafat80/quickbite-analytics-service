// Package mongo is a thin connection wrapper around the official mongo-driver.
//
// LAYERING: pkg/ is framework-agnostic and app-agnostic. This file knows how
// to open a connection and ping; it does NOT know about any specific
// collection. Collection names + indexes live in app/<module>/repository/.
package mongo

import (
	"context"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.mongodb.org/mongo-driver/mongo/readpref"
)

type Config struct {
	URI       string
	Database  string
	ConnectTO time.Duration
}

// Connect opens a client, runs ServerSelection + Ping under ConnectTO, and
// returns both the client and the resolved *mongo.Database handle. Caller is
// responsible for Disconnect on shutdown.
func Connect(ctx context.Context, cfg Config) (*mongo.Client, *mongo.Database, error) {
	if cfg.ConnectTO == 0 {
		cfg.ConnectTO = 5 * time.Second
	}
	pingCtx, cancel := context.WithTimeout(ctx, cfg.ConnectTO)
	defer cancel()

	client, err := mongo.Connect(pingCtx, options.Client().ApplyURI(cfg.URI))
	if err != nil {
		return nil, nil, fmt.Errorf("mongo connect: %w", err)
	}
	if err := client.Ping(pingCtx, readpref.Primary()); err != nil {
		_ = client.Disconnect(context.Background())
		return nil, nil, fmt.Errorf("mongo ping: %w", err)
	}
	return client, client.Database(cfg.Database), nil
}

// Disconnect closes the client; safe to call with a nil client.
func Disconnect(ctx context.Context, client *mongo.Client) error {
	if client == nil {
		return nil
	}
	return client.Disconnect(ctx)
}
