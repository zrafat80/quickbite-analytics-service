package repository

import (
	"context"
	"errors"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

// EventIDsRepo implements lib/coreevents.EventDeduper (via duck typing — the
// interface lives in lib/, and Go's structural typing picks it up here so
// lib never imports app).
//
// Strategy: unique index on event_id + InsertOne. A duplicate insert
// returns a duplicate-key error, which we interpret as "already seen, skip".
// We pick Mongo over Redis SETNX because it shares fate with the aggregate
// writes — losing the dedupe set would let a replay double-count, but
// losing both stores together is much rarer than losing just Redis.
type EventIDsRepo struct {
	coll *mongo.Collection
}

func NewEventIDsRepo(db *mongo.Database) *EventIDsRepo {
	return &EventIDsRepo{coll: db.Collection(CollectionEventIDs)}
}

// MarkSeen returns fresh=true when this is the first observation of
// eventID, fresh=false when the doc already exists. Anything else is an
// error the caller routes to DLQ.
func (r *EventIDsRepo) MarkSeen(ctx context.Context, eventID string) (bool, error) {
	_, err := r.coll.InsertOne(ctx, bson.M{
		"event_id":    eventID,
		"received_at": time.Now().UTC(),
	})
	if err == nil {
		return true, nil
	}
	if isDupKey(err) {
		return false, nil
	}
	return false, err
}

func (r *EventIDsRepo) Forget(ctx context.Context, eventID string) error {
	_, err := r.coll.DeleteOne(ctx, bson.M{"event_id": eventID})
	return err
}

func isDupKey(err error) bool {
	var werr mongo.WriteException
	if errors.As(err, &werr) {
		for _, we := range werr.WriteErrors {
			if we.Code == 11000 {
				return true
			}
		}
	}
	return false
}
