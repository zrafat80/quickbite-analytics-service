//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

type successEnvelope[T any] struct {
	Success bool `json:"success"`
	Data    T    `json:"data"`
}

type errorEnvelope struct {
	Success bool `json:"success"`
	Error   struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func decodeSuccess[T any](t *testing.T, response *httptest.ResponseRecorder) successEnvelope[T] {
	t.Helper()
	var body successEnvelope[T]
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body), response.Body.String())
	require.True(t, body.Success)
	return body
}

func decodeError(t *testing.T, response *httptest.ResponseRecorder) errorEnvelope {
	t.Helper()
	var body errorEnvelope
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body), response.Body.String())
	require.False(t, body.Success)
	return body
}

func assertErrorResponse(t *testing.T, response *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	require.Equal(t, status, response.Code, response.Body.String())
	assert.Equal(t, code, decodeError(t, response).Error.Code)
}

func ownerToken(t *testing.T, restaurantID int64, restaurantRole string) string {
	t.Helper()
	return mintToken(t, tokenSpec{
		UserID: 1, Role: "restaurant_user", RestaurantID: restaurantID, RestaurantRole: restaurantRole,
	})
}

func adminToken(t *testing.T) string {
	t.Helper()
	return mintToken(t, tokenSpec{UserID: 99, Role: "system_admin"})
}

func seedRestaurantRows(t *testing.T, app *integrationApp, rows ...bson.M) {
	t.Helper()
	seedRows(t, app.restaurantCollection, rows...)
}

func seedBranchRows(t *testing.T, app *integrationApp, rows ...bson.M) {
	t.Helper()
	seedRows(t, app.branchCollection, rows...)
}

func seedProductRows(t *testing.T, app *integrationApp, rows ...bson.M) {
	t.Helper()
	seedRows(t, app.productCollection, rows...)
}

func seedPlatformRows(t *testing.T, app *integrationApp, rows ...bson.M) {
	t.Helper()
	seedRows(t, app.platformCollection, rows...)
}

func seedRows(t *testing.T, collection *mongo.Collection, rows ...bson.M) {
	t.Helper()
	if len(rows) == 0 {
		return
	}
	values := make([]any, len(rows))
	for index, row := range rows {
		row["updated_at"] = time.Now().UTC()
		values[index] = row
	}
	_, err := collection.InsertMany(context.Background(), values)
	require.NoError(t, err)
}
