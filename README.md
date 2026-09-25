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
- `GET /v1/dashboard/kpis` · `GET /v1/dashboard/team` · `GET /v1/dashboard/attention` (gm/manager)
- `GET /v1/dashboard/my-work` · `GET /v1/dashboard/my-target` (authenticated; `?scope=branch` for managers)
- `POST /v1/documents/presign`
- Inbox + webhooks — see Epic 8 below

## Epic 7 Manager + Employee Workspaces

| Task | Status |
|------|--------|
| T-084 Manager KPI tiles (period + branch scope) | Done |
| T-085 Exceptions-first KPI definitions | Done |
| T-086 Team performance board | Done |
| T-087 Attention / exception feed | Done |
| T-088 Consistent period/branch scope | Done |
| T-089 Employee My Work Today | Done |
| T-090 Target progress (`revenue_targets`) | Done |
| T-091–T-096 Quick actions + drill-downs + RBAC | Done |

Aggregator port in `internal/app/dashboard`; Postgres adapter; JWT RBAC (`dashboard.read` for manager surfaces).

## Epic 8 Unified Inbox + Integrations + SLA

| Task | Status |
|------|--------|
| T-097 Message + Conversation schema | Done |
| T-098 Provider-agnostic ChannelProvider | Done |
| T-099–T-101 WhatsApp / Instagram / Email adapters (stub-backed) | Done |
| T-102–T-103 Customer match + lead shell | Done |
| T-104 Assign / reassign | Done |
| T-105 Outbound reply via channel | Done |
| T-106 SLA start/stop + breach sweep | Done |
| T-107 Integration health (+ ops DLQ reuse) | Done |
| T-108 Stub providers | Done |

- `GET /v1/inbox/conversations` · `GET .../{id}` · `GET .../messages`
- `POST .../assign|reply|status` · `POST /v1/inbox/sla/check`
- `GET /v1/integrations/health`
- `POST /v1/integrations/accounts/{whatsapp|instagram|facebook|gmail}/connect` (BYO credentials JSON)
- `POST /v1/integrations/accounts/{provider}/disconnect`
- `POST /v1/webhooks/{whatsapp|instagram|facebook|gmail|email|stub}?branch_id=`

**Connect body (examples):** WhatsApp `{access_token, phone_number_id, …}` · Instagram `{access_token, page_id, ig_user_id}` · Facebook `{access_token, page_id}` · Gmail `{client_id, client_secret, refresh_token, mailbox_email}`. Secrets stored in `integration_accounts.config_json`; responses return `public_meta` + `webhook_url` only.

## Epic 9 Finance, Payments & Revenue Metrics

| Task | Status |
|------|--------|
| T-116 Payment ledger (charge/reverse/adjust/refund) | Done |
| T-117 Booking financial summary + balance | Done |
| T-118 Payment schedule / promise dates | Done |
| T-119 Refund/adjustment with `payments.approve` + audit | Done |
| T-120 Reporting currency policy hook (`finance_settings`) | Done |
| T-121 Revenue basis metrics (booked/collected/recognized/margin) | Done |
| T-122 Finance queues API (overdue/unverified/refunds/credit) | Done |
| T-123 Payment-due reminder → task | Done |

- `POST /v1/payments` · `POST /v1/payments/{id}/verify|reverse` · `POST /v1/payments/adjust|refunds`
- `POST /v1/payments/{id}/approve|reject` · `GET /v1/bookings/{id}/financial-summary|payments|payment-schedules`
- `GET /v1/finance/queues/{kind}` · `GET /v1/finance/export?kind=` · `POST /v1/finance/reminders/process`

## Epic 10 Revenue Target & Performance Engine

| Task | Status |
|------|--------|
| T-130 RevenueTarget entity (metric/scope/curve) | Done |
| T-131 Linear + seasonal weights (sum 10000 bps) | Done |
| T-132 Deterministic calc engine | Done |
| T-133 Target shares to employees | Done |
| T-134 Payment/booking → recompute snapshots | Done |
| T-135 Behind → recovery task | Done |
| T-136 Drill-down sources | Done |
| T-137 Revision audit | Done |

- `GET/POST /v1/targets` · `PATCH /v1/targets/{id}` · `GET/PUT .../weights` · `PUT .../shares`
- `GET .../progress|contributions|series|sources|revisions` · `POST .../recompute`

Domain engine is pure (`domain/revenuetarget.Engine`); HTTP/app/adapters depend inward (SOLID).

## Epic 11 Excel Import/Export

| Task | Status |
|------|--------|
| T-144 Import job lifecycle (file/mapping/counts/status) | Done |
| T-145 Parse CSV + XLSX + preview rows | Done |
| T-146 Column mapping templates per entity | Done |
| T-147 Validation + normalize phone/date/currency/status | Done |
| T-148 Duplicate detection + create/update/upsert | Done |
| T-149 Async JobImportProcess + sync confirm (idempotent) | Done |
| T-150 Row-level error report download | Done |
| T-151 Export builder CSV (UTF-8 BOM, AR-safe) | Done |

- `POST/GET /v1/imports` · `GET /v1/imports/{id}` · `PUT .../mapping` · `POST .../validate|confirm`
- `GET /v1/imports/{id}/errors` · `GET/POST/DELETE /v1/imports/templates`
- `POST /v1/exports` · `GET /v1/exports/schemas`
- Permissions: `imports.read` / `imports.write` (exports share the same)
- File bytes stored on `import_jobs.file_bytes` (source of truth); `rollback_token` is an audit reference only
- Customer import is full create/update/upsert; bookings/payments/departures validate + skip on Process

Domain helpers are pure (`domain/importexport` SuggestMapping/Normalize/Parse); app Process is idempotent for worker + sync confirm.

## Epic 12 Documents, Visa & Suppliers

| Task | Status |
|------|--------|
| T-156 Configurable document requirements policy | Done |
| T-157 Document entity + status/review/version/expiry | Done |
| T-158 VisaCase workflow + external reference | Done |
| T-159 Expiry/validity reminder rules | Done |
| T-160 Departure missing-document aggregate API | Done |
| T-161 Booking readiness docs gate + manager override | Done |
| T-162 Supplier entity + contacts + terms | Done |
| T-163 Link supplier + confirmation refs | Done |
| T-164 Unconfirmed supplier reminder + sold>allotment | Done |
| T-165 Lightweight supplier cost fields | Done |

- `PATCH /v1/documents/{id}` · `POST .../submit|approve|reject|replace` · `GET/PUT /v1/documents/policies`
- `GET /v1/documents/checklist?booking_id=` · `GET /v1/documents/missing-docs?departure_id=` · `POST /v1/documents/reminders/expiry`
- `GET|POST /v1/visa-cases` · `GET /v1/visa-cases/{id}` · `POST .../transition`
- `GET|POST /v1/suppliers` · `GET|PATCH /v1/suppliers/{id}` · `GET|POST .../links` · `DELETE .../links/{linkId}` · `POST /v1/suppliers/links/{linkId}/confirm`
- `GET /v1/suppliers/unconfirmed|oversold` · `POST /v1/suppliers/reminders/unconfirmed`
- `POST /v1/bookings/{id}/readiness-override`

Domain status machines stay pure (`document` / `visa` / `supplier`); HTTP/app/adapters depend inward (SOLID).
- Permissions: `documents.read|write|review`, `visa.read|write`, `suppliers.read|write` (existing Presign/Complete/List gated)

Document domain owns lifecycle transitions; visa `ValidTransition` is pure; supplier `IsOversold` when `allotment>0 && sold>allotment`. Booking readiness blocks on unapproved policy docs unless an override is active.

## Epic 13 Notifications & Escalation

| Task | Status |
|------|--------|
| T-173 In-app notification domain (mandatory channel) | Done |
| T-174 Escalation rules matrix (message/lead/task/payment/doc/target/integration) | Done |
| T-175 Alert acknowledge/resolve + high-volume grouping | Done |
| T-176 Optional external email/push toggles | Done |
| T-177–T-179 FE notification center / ack-resolve / prefs | Done (FE) |

- `GET /v1/notifications` · `GET .../unread-count` · `POST .../{id}/acknowledge|resolve` · `POST .../ack-all`
- `GET|PUT /v1/notifications/preferences` · `GET /v1/notifications/rules`
- `POST /v1/notifications/escalate` · `POST /v1/notifications/emit` (manage)
- Permissions: `notifications.read|write|manage`
- Domain matrix is pure (`domain/notification` MatchRule / ShouldEscalate / BuildGroupKey); reactors subscribe to SLA / booking / task events
- Groupable kinds upsert by `group_key` and bump `occurrence_count` (“12 conversations overdue”)
- In-app is always on; email/push are opt-in stubs (`LogExternal`)

## Epic 14 Reporting & Audit Completion

| Task | Status |
|------|--------|
| T-180 Sales performance report API | Done |
| T-181 Target performance report API | Done |
| T-182 Operational readiness report API | Done |
| T-183 Communication SLA report API | Done |
| T-184 Finance report API | Done |
| T-185 Integration log query API | Done |
| T-186–T-188 FE reports hub / drill-down / export | Done (FE) |

- `GET /v1/reports/kinds` · `GET /v1/reports/{sales|targets|readiness|sla|finance|integrations}`
- `GET /v1/reports/export?kind=` (UTF-8 BOM CSV; sensitive kinds write `report_export_audits`)
- Query: `from`/`to` (RFC3339 or YYYY-MM-DD), `owner_id`, `channel`, `provider`, `status`, `departure_id`, `limit`
- Permissions: `reports.read` / `reports.export`
- Domain owns kind matrix + CSV builder; postgres adapter aggregates; rows include `drilldowns` href hints

## Design notes

- **ACID**: application services wrap multi-table writes in `tx.Manager.WithinTransaction` (payment ledger + booking balance; lead create + stage history).
- **SOLID**: domain ports in `internal/domain/*`; adapters depend inward; composition root in `internal/app/wire.go`.
- **Idempotency**: payments and auto-seeded tasks use `idempotency_key`.
- **Events**: published after successful commit (`booking.confirmed` → document/payment tasks).
