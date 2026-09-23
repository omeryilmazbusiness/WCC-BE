# wodi-crm-be

Go modular monolith for the WODI Command Center (Hajj/Umrah internal CRM).

## Stack

| Layer | Choice |
|-------|--------|
| HTTP | chi |
| DB | PostgreSQL + goose migrations + pgx |
| Auth | JWT access + refresh, RBAC |
| Queue | Asynq/Redis (retry + archived DLQ; memory fallback) |
| Files | S3-compatible ObjectStore port (MinIO adapter) |
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

Health: `GET /healthz` · Ready: `GET /readyz` (db + queue)  
Ops (GM): `GET /v1/ops/queue` · `GET /v1/ops/jobs/{id}?queue=default`  
API prefix: `/v1`

### Epic 0 foundation

| Task | Status |
|------|--------|
| T-001 Modular monolith + env + lint + CI | Done |
| T-002 Migrations/seed + entity conventions | Done |
| T-003 REST envelope + pagination/filter/sort | Done |
| T-004 Queue retry / DLQ / job status | Done |
| T-005 Object storage adapter (metadata in DB) | Done |
| T-006 Observability (logs, health, queue metrics) | Done |

### Epic 1 identity & admin

| Task | Status |
|------|--------|
| T-011 Auth login/refresh/logout + password policy | Done |
| T-012 Optional MFA hooks | Done |
| T-013 Users CRUD + branch/team | Done |
| T-014 Roles/permissions + API RBAC | Done |
| T-015 Ownership + branch/team scoping | Done |
| T-016 AuditEvent service + list | Done |
| T-017 Sensitive action audit hooks | Done |

Demo users (password `ChangeMe123!`):

- `gm@wodi.local`
- `manager@wodi.local`
- `sales@wodi.local`

### Epic 2 Customer 360

| Task | Status |
|------|--------|
| T-023 Customer PII fields + normalize/mask | Done |
| T-024 Duplicate detection (phone/email/passport/name) | Done |
| T-025 Soft duplicate warn on create | Done |
| T-026 Customer update + preferences | Done |
| T-027 Merge (merged_into_id, deactivate source) | Done |
| T-028 Companions link/unlink | Done |
| T-029 Unified timeline (leads/bookings/docs/payments/tasks) | Done |

### Epic 3 CRM / Lead Pipeline

| Task | Status |
|------|--------|
| T-034 Lead entity + stage taxonomy | Done |
| T-035 Append-only stage history | Done |
| T-036 Assign / bulk-assign ownership | Done |
| T-037 Convert lead → booking draft | Done |
| T-038 Lost reason taxonomy + note | Done |
| T-039 No-follow-up flag | Done |
| T-040 Lead analytics (stage/source/owner) | Done |

### Epic 4 Packages / Departures / Capacity

| Task | Status |
|------|--------|
| T-047 Package template entity | Done |
| T-048 Departure instance entity | Done |
| T-049 Pricing tiers (room/occupancy/age) | Done |
| T-050 Capacity from confirmed bookings | Done |
| T-051 Clone package + create departure | Done |
| T-052 Historical pricing immutability | Done |
| T-053 Capacity threshold / oversell alerts | Done |

### Epic 5 Booking Workspace

| Task | Status |
|------|--------|
| T-059 Booking status machine (draft→confirmed→completed/cancelled) | Done |
| T-060 Participants add/update/delete with pax cap | Done |
| T-061 Line items (package/hotel/room/transport/flight/extras) | Done |
| T-062 Discount / cost / margin recalculation | Done |
| T-063 Travel checklist seed + toggle | Done |
| T-064 Readiness + risk alerts | Done |
| T-065 Confirm gates (pax, checklist, capacity) | Done |
| T-066 Booking list/filter | Done |

### Epic 6 Tasks & Workflow

| Task | Status |
|------|--------|
| T-073 Task entity (status/priority/type/outcome) | Done |
| T-074 Manual assign + bulk assign APIs | Done |
| T-075 Rule engine: auto-create from ops events | Done |
| T-076 Idempotency keys for open tasks | Done |
| T-077 Overdue detection + escalation after grace | Done |
| T-078 Resolve condition → auto-close/suppress | Done |

## P0 route map

- `POST /v1/auth/login|refresh` · `GET /v1/auth/me`
- `GET|POST /v1/customers` · `GET|PATCH /v1/customers/{id}` · `GET /v1/customers/duplicates`
- `POST /v1/customers/{id}/merge` · `GET /v1/customers/{id}/timeline`
- `GET|POST /v1/customers/{id}/companions` · `DELETE .../companions/{companionId}`
- `GET|POST /v1/leads` · `GET /v1/leads/analytics` · `GET /v1/leads/lost-reasons` · `POST /v1/leads/assign`
- `GET /v1/leads/{id}` · `POST .../stage|assign|convert|no-follow-up` · `GET .../history`
- `GET|POST /v1/packages` · `GET|PATCH /v1/packages/{id}` · `POST .../clone` · `GET|PUT .../tiers`
- `GET|POST /v1/packages/{id}/departures`
- `GET|PATCH /v1/departures/{id}` · `POST .../clone|close-sales|mark-full|recompute-capacity`
- `GET /v1/departures/{id}/tiers|readiness`
- `GET|POST /v1/packages` · `GET /v1/packages/{id}` · `GET|POST /v1/packages/{id}/departures`
- `GET /v1/departures/{id}` · `POST /v1/departures/{id}/clone`
- `GET|POST /v1/bookings` · `GET|PATCH /v1/bookings/{id}` · `POST .../confirm|status`
- `GET /v1/bookings/{id}/readiness`
- `GET|POST /v1/bookings/{id}/participants` · `PATCH|DELETE .../participants/{participantId}`
- `GET|PUT /v1/bookings/{id}/line-items` · `GET|PATCH .../checklist|checklist/{itemId}`
- `GET /v1/bookings/{id}/payments`
- `POST /v1/payments` (immutable ledger; GM/Manager)
- `GET|POST /v1/tasks` · `GET /v1/tasks/mine` · `POST /v1/tasks/assign` · `POST /v1/tasks/escalate-overdue`
- `GET /v1/tasks/{id}` · `POST .../status|complete|reschedule|assign`
- `GET /v1/dashboard/kpis` (gm/manager)
- `POST /v1/documents/presign`

## Design notes

- **ACID**: application services wrap multi-table writes in `tx.Manager.WithinTransaction` (payment ledger + booking balance; lead create + stage history).
- **SOLID**: domain ports in `internal/domain/*`; adapters depend inward; composition root in `internal/app/wire.go`.
- **Idempotency**: payments and auto-seeded tasks use `idempotency_key`.
- **Events**: published after successful commit (`booking.confirmed` → document/payment tasks).
