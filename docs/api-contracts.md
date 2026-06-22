# api-contracts.md

All endpoints live under `/api/v1/`. Every response is wrapped:

```json
{ "success": true,  "data": ... }
{ "success": false, "error": { "code": "...", "message": "..." } }
```

## GET /health

Unauthenticated liveness probe. Always 200.

```json
{ "success": true, "data": { "status": "ok" } }
```

## GET /api/v1/analytics/restaurants/{restaurantId}/days

Returns one row per UTC day in the requested range for the given restaurant.

| Property | Type | Notes |
| --- | --- | --- |
| restaurantId (path) | int64 | > 0 required |
| from (query) | string | `YYYY-MM-DD`, required, UTC |
| to (query) | string | `YYYY-MM-DD`, required, UTC, must be ≥ from |

**Auth:** JWT (cookie `access_token` OR `Authorization: Bearer`).
**RBAC:** requires `analytics:read`.

### Success (200)

```json
{
  "success": true,
  "data": [
    {
      "date":          "2026-05-21",
      "ordersCount":   2,
      "revenueMinor":  4000,
      "currency":      "EGP",
      "avgOrderMinor": 2000
    }
  ]
}
```

- `revenueMinor` and `avgOrderMinor` are integer minor units (e.g. EGP
  piasters).
- `avgOrderMinor` is derived in the service layer
  (`revenueMinor / ordersCount`), not stored.
- An empty result returns `data: []`, not 404.

### Error codes

| HTTP | code | When |
| --- | --- | --- |
| 400 | `VALIDATION_ERROR` | missing/malformed `from` or `to`, or non-integer path id |
| 400 | `ANALYTICS_INVALID_DATE_RANGE` | `from` > `to` |
| 401 | `UNAUTHENTICATED` | missing/invalid/expired token |
| 403 | `FORBIDDEN` | role lacks `analytics:read` |
| 500 | `INTERNAL_ERROR` | unhandled error — check logs by `correlation_id` |

### Headers

All responses include `x-correlation-id`. If the caller supplies one in the
request, we echo it; otherwise we generate a UUID. Log lines for the
request include the same id under `correlation_id`.

## Event contract — inbound `order.placed`

This is **not** an HTTP endpoint — it is the envelope our consumer reads
from RabbitMQ. Documented here so the publisher (order-service) and
consumer stay in sync.

```json
{
  "eventId":       "ce6f9b3d-...",
  "eventType":     "order.placed",
  "occurredAt":    "2026-05-21T13:14:15Z",
  "aggregateType": "order",
  "aggregateId":   "<order public uuid>",
  "payload": {
    "orderId":      "<order public uuid>",
    "region":       "eg",
    "countryCode":  "EG",
    "restaurantId": 42,
    "branchId":     1,
    "customerId":   7,
    "status":       "placed",
    "paymentMethod":"cod",
    "subtotal":     2500,
    "deliveryFee":  0,
    "serviceFee":   0,
    "total":        2500,
    "currency":     "EGP",
    "items": [
      { "productId": 10, "quantity": 1, "unitPriceSnapshot": 2500, "lineTotal": 2500 }
    ],
    "placedAt":     "2026-05-21T13:14:15Z"
  }
}
```

Required for the aggregate write: `eventId`, `restaurantId`, `total`,
`currency`, `placedAt`. Additional handlers consume rejection, delivery, and
payment-completion payloads from the same envelope format.

## Additional analytics endpoints

| Endpoint | Notes |
| --- | --- |
| `GET /analytics/restaurants/{id}/failures?from=&to=` | Derived rejected/order ratio; restaurant scoped. |
| `GET /analytics/restaurants/{id}/delivery-avg?from=&to=` | Derived delivery average; restaurant scoped. |
| `GET /analytics/platform/active-restaurants?from=&to=` | Distinct restaurants with orders; system admin only. |
| `GET /analytics/branches/{id}/days?from=&to=` | Branch daily totals with tenant/assignment checks. |
| `GET /analytics/products/{id}/days?from=&to=` | Product orders, units, and revenue. |
| `GET /analytics/platform/days?from=&to=` | Platform totals grouped by day and currency; system admin only. |
| `GET /analytics/restaurants/top?from=&to=&limit=` | Revenue ranking, limit 1-100; system admin only. |
