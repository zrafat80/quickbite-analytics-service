// Package config is the equivalent of order-service's lib/config/app.config.ts:
// one struct, one Load() function. Read at boot, passed to anything that
// needs it via constructor injection.
package config

import (
	"fmt"
	"strings"
	"time"

	"github.com/caarlos0/env/v11"
	"github.com/joho/godotenv"
)

type Env struct {
	Port        int    `env:"PORT" envDefault:"5000"`
	Environment string `env:"ENVIRONMENT" envDefault:"development"`
	LogLevel    string `env:"LOG_LEVEL" envDefault:"info"`

	Mongo struct {
		URI      string `env:"MONGO_URL" envDefault:"mongodb://localhost:27017"`
		Database string `env:"MONGO_DATABASE" envDefault:"analytics"`
	} `envPrefix:""`

	Rabbit struct {
		URL                 string   `env:"RABBITMQ_URL,required" envDefault:"amqp://guest:guest@localhost:5672/"`
		OrderEventsExchange string   `env:"RABBITMQ_ORDER_EVENTS_EXCHANGE" envDefault:"order.events"`
		OrderEventsQueue    string   `env:"RABBITMQ_ORDER_EVENTS_QUEUE" envDefault:"analytics-service.order-events"`
		OrderEventsBindings []string `env:"RABBITMQ_ORDER_EVENTS_BINDINGS" envDefault:"order.#,payment.#" envSeparator:","`
		OrderEventsDLX      string   `env:"RABBITMQ_ORDER_EVENTS_DLX" envDefault:"order.events.dlx"`
		OrderEventsDLQ      string   `env:"RABBITMQ_ORDER_EVENTS_DLQ" envDefault:"analytics-service.order-events.dlq"`

		// Second consumer — RBAC cache invalidation from core.events.
		// Distinct queue so RBAC poison messages don't stall order ingestion.
		CoreEventsExchange string   `env:"RABBITMQ_CORE_EVENTS_EXCHANGE" envDefault:"core.events"`
		CoreEventsQueue    string   `env:"RABBITMQ_CORE_EVENTS_QUEUE" envDefault:"analytics-service.core-events"`
		CoreEventsBindings []string `env:"RABBITMQ_CORE_EVENTS_BINDINGS" envDefault:"rbac.#" envSeparator:","`
		CoreEventsDLX      string   `env:"RABBITMQ_CORE_EVENTS_DLX" envDefault:"core.events.dlx"`
		CoreEventsDLQ      string   `env:"RABBITMQ_CORE_EVENTS_DLQ" envDefault:"analytics-service.core-events.dlq"`

		Prefetch int `env:"RABBITMQ_PREFETCH" envDefault:"32"`
	} `envPrefix:""`

	Auth struct {
		AccessSecret string `env:"ACCESS_SECRET,required"`
	} `envPrefix:""`

	Core struct {
		BaseURL        string        `env:"CORE_SERVICE_BASE_URL" envDefault:"http://localhost:4000"`
		InternalAPIKey string        `env:"CORE_INTERNAL_API_KEY,required"`
		RBACCacheTTL   time.Duration `env:"RBAC_CACHE_TTL_SEC" envDefault:"300s"`
	} `envPrefix:""`
}

// Load reads .env (if present, best-effort) then parses env into Env.
// Required vars without defaults produce a clear error — no silent fallbacks.
func Load() (*Env, error) {
	_ = godotenv.Load() // best-effort: missing .env is fine
	cfg := &Env{}
	if err := env.Parse(cfg); err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}
	// caarlos0/env returns the literal env value for slices; trim and drop
	// empties so a trailing comma in the env doesn't yield "".
	cfg.Rabbit.OrderEventsBindings = trimNonEmpty(cfg.Rabbit.OrderEventsBindings)
	cfg.Rabbit.CoreEventsBindings = trimNonEmpty(cfg.Rabbit.CoreEventsBindings)
	return cfg, nil
}

func trimNonEmpty(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}
