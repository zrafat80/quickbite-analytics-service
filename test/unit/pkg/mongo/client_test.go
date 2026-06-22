package mongo

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	. "github.com/zrafat80/quickbite/analytics-service/pkg/mongo"
)

func TestConnectRejectsInvalidURI(t *testing.T) {
	client, database, err := Connect(context.Background(), Config{
		URI: "://invalid", Database: "test", ConnectTO: 20 * time.Millisecond,
	})
	require.Error(t, err)
	assert.Nil(t, client)
	assert.Nil(t, database)
}

func TestDisconnectNilClient(t *testing.T) {
	require.NoError(t, Disconnect(context.Background(), nil))
}
