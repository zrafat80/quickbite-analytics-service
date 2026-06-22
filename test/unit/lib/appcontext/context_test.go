package appcontext

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	. "github.com/zrafat80/quickbite/analytics-service/lib/appcontext"
)

func TestContextValues(t *testing.T) {
	_, ok := ClaimsFrom(context.Background())
	assert.False(t, ok)
	assert.Empty(t, CorrelationIDFrom(context.Background()))

	claims := &Claims{UserID: 7, Role: "customer"}
	ctx := WithClaims(context.Background(), claims)
	got, ok := ClaimsFrom(ctx)
	require.True(t, ok)
	assert.Same(t, claims, got)

	ctx = WithCorrelationID(ctx, "correlation-1")
	assert.Equal(t, "correlation-1", CorrelationIDFrom(ctx))
}
