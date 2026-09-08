# wodi-crm-be

Go modular monolith for the WODI Command Center (Hajj/Umrah internal CRM).

## Stack

| Layer | Choice |
|-------|--------|
| HTTP | chi |
| DB | PostgreSQL + goose migrations + pgx |
| Auth | JWT access + refresh, RBAC |
| Queue | Asynq/Redis (worker stub) |
| Files | S3-compatible (MinIO stub) |
| Events | In-process domain bus |

## Architecture

Clean / hexagonal modular monolith:

```
cmd/            process entrypoints (api, migrate, worker)
internal/
  domain/       entities + ports + invariants (SOLID)
  app/          use cases + unit-of-work (ACID boundaries)
  adapter/      http, postgres, storage, queue
  platform/     config consumers: db, tx, jwt, events, logger
  config/       env loading
migrations/     P0 schema
api/openapi/    contract stub
```

## Quick start

```bash
cp .env.example .env
make docker-up
make migrate-up
make run
```

Health: `GET /healthz` · Ready: `GET /readyz`  
API prefix: `/v1`

Demo users (password `ChangeMe123!`):

- `gm@wodi.local`
- `manager@wodi.local`
- `sales@wodi.local`

## P0 route map

- `POST /v1/auth/login|refresh` · `GET /v1/auth/me`
- `GET|POST /v1/customers` · `GET /v1/customers/{id}`
- `POST /v1/leads` · `POST /v1/leads/{id}/stage`
- `POST /v1/bookings` · `POST /v1/bookings/{id}/confirm`
- `POST /v1/payments`
- `GET /v1/tasks/mine`
- `GET /v1/dashboard/kpis` (gm/manager)
- `POST /v1/documents/presign`
- `GET /v1/packages/{id}` · `GET /v1/packages/{id}/departures`

## Design notes

- **ACID**: application services wrap multi-table writes in `tx.Manager.WithinTransaction` (payment ledger + booking balance; lead create + stage history).
- **SOLID**: domain ports in `internal/domain/*`; adapters depend inward; composition root in `internal/app/wire.go`.
- **Idempotency**: payments and auto-seeded tasks use `idempotency_key`.
- **Events**: published after successful commit (`booking.confirmed` → document/payment tasks).
