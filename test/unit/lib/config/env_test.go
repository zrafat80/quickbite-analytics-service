package config

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	. "github.com/zrafat80/quickbite/analytics-service/lib/config"
)

func TestLoadUsesEnvironmentAndCleansBindings(t *testing.T) {
	t.Setenv("PORT", "7777")
	t.Setenv("LOG_LEVEL", "debug")
	t.Setenv("MONGO_URI", "mongodb://unit")
	t.Setenv("MONGO_DATABASE", "analytics_unit")
	t.Setenv("RABBITMQ_URL", "amqp://unit")
	t.Setenv("RABBITMQ_ORDER_EVENTS_BINDINGS", " order.#, ,payment.# ")
	t.Setenv("RABBITMQ_CORE_EVENTS_BINDINGS", " rbac.#, ")
	t.Setenv("ACCESS_SECRET", "secret")
	t.Setenv("CORE_INTERNAL_API_KEY", "internal")
	t.Setenv("RBAC_CACHE_TTL_SEC", "15s")

	cfg, err := Load()
	require.NoError(t, err)
	assert.Equal(t, 7777, cfg.Port)
	assert.Equal(t, "debug", cfg.LogLevel)
	assert.Equal(t, "analytics_unit", cfg.Mongo.Database)
	assert.Equal(t, []string{"order.#", "payment.#"}, cfg.Rabbit.OrderEventsBindings)
	assert.Equal(t, []string{"rbac.#"}, cfg.Rabbit.CoreEventsBindings)
	assert.Equal(t, 15*time.Second, cfg.Core.RBACCacheTTL)
}
