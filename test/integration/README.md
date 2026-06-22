# Analytics integration tests

This integration suite exercises the production HTTP router with `httptest`,
real MongoDB, and real RabbitMQ.

They use:

- a real local MongoDB through `go.mongodb.org/mongo-driver/mongo`;
- a dedicated database whose name must contain `test`;
- an `httptest.NewServer` adapter for Core RBAC responses;
- the production controller, service, repository, auth, and RBAC middleware.
- unique RabbitMQ exchanges and queues removed after each event test.

Run MongoDB and RabbitMQ locally, then execute:

```sh
go test -tags=integration -v ./test/integration/...
```

The helper loads the service `.env` first. Explicit environment variables
still take precedence:

```sh
MONGO_URI=mongodb://localhost:27017
MONGO_TEST_DATABASE=analytics_service_test
RABBITMQ_URL=amqp://guest:guest@localhost:5672/
```

The suite covers `/health`, every analytics HTTP endpoint, repository/index
behavior, all supported events, complete restaurant/branch/product/platform
fan-out, duplicate suppression, transient/permanent consumer failures, RBAC
invalidation, and DLQ routing.

Normal HTTP coverage is split by aggregate:

- `restaurant.integration_test.go`
- `branch.integration_test.go`
- `product.integration_test.go`
- `platform.integration_test.go`

Shared HTTP fixtures live in `http_helpers_test.go`; RabbitMQ event fan-out
is covered separately in `events.integration_test.go`.
