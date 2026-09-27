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

## Epic 15 AI Intelligence Layer

| Task | Status |
|------|--------|
| T-189 AI module contract + audited runs | Done |
| T-190 Manager daily summary | Done |
| T-191 Conversation summary + next step | Done |
| T-192 Reply draft (never auto-send) | Done |
| T-193 Lead priority scoring (deterministic + optional explain) | Done |
| T-194 Target recovery insights | Done |
| T-195 OCR extract + confirm gate | Done |
| T-196 Money/SLA/target stay deterministic | Done |
| T-197–T-201 FE surfaces + BYO setup wizard | Done |

- `GET/POST /v1/ai/setup` — BYO `openai` \| `anthropic` \| `gemini` API key (per branch; key never returned raw)
- `GET /v1/ai/daily-summary` · `POST /v1/ai/conversations/{id}/assist` · `POST /v1/ai/leads/{id}/score`
- `GET /v1/ai/targets/{id}/insight` · `POST /v1/ai/ocr` · `POST /v1/ai/runs/{id}/feedback`
- Permissions: `ai.read` / `ai.write` / `ai.setup`
- Providers in `adapter/ai` (OCP); domain scoring pure; runs stored in `ai_runs`
- Without a key, summaries/insights fall back to deterministic rules (`source=deterministic`)

## Epic 16 P2 Advanced Extensions

| Task | Status |
|------|--------|
| T-202 Connected Excel sync (OneDrive / SharePoint) | Done |
| T-203 Advanced supplier invoice / cost tracking | Done |
| T-204 External accounting / GDS / payment stubs | Done |
| T-205 File sync FE surfaces | Done (FE) |
| T-206 Supplier invoice FE | Done (FE) |
| T-207 External integrations FE | Done (FE) |

- `GET/POST /v1/file-sync/connections` · `GET/PATCH/DELETE /v1/file-sync/connections/{id}`
- `POST /v1/file-sync/connections/{id}/connect` · `POST /v1/file-sync/connections/{id}/sync` · `GET /v1/file-sync/runs`
- `GET/POST /v1/suppliers/invoices` · `GET/PATCH /v1/suppliers/invoices/{id}` · `PUT /v1/suppliers/invoices/{id}/lines` · `GET /v1/suppliers/{id}/invoices`
- `GET /v1/external-integrations/catalog` · `GET/POST /v1/external-integrations` · `GET/PATCH/DELETE /v1/external-integrations/{id}` · `POST .../probe`
- Permissions: `filesync.read` / `filesync.write`; extint reuses `integrations.read` / `integrations.write`; invoices reuse `suppliers.*`
- Platform DB remains authoritative on sync; conflict policy applied via pure domain `ResolveConflict` / `ApplySampleRows`
- Cloud + external adapters are stubs (no live network in MVP)

## Epic 17 Cross-Cutting Hardening & Definition of Done

| Task | Status |
|------|--------|
| T-208 E2E workflow contract (inbound→report) | Done |
| T-209 Idempotency key contract + task replay tests | Done |
| T-210 Permission matrix + AI leakage tests | Done |
| T-211 Backup/restore + secrets verification docs | Done |
| T-212 Perf indexes + pagination guarantees | Done |
| T-213–T-216 FE RTL / mobile / acceptance / state polish | Done (FE) |
| T-217 Acceptance gate checklist | Done |

- Domain: `internal/domain/hardening` (pure workflow + idempotency; DIP-friendly)
- Migration: `00020_epic17_hardening.sql` (list/dashboard composite indexes)
- Docs: `docs/BACKUP_RESTORE.md`, `docs/SECRETS.md`, `docs/ACCEPTANCE_GATE.md`
- Tests: `go test ./internal/domain/hardening/...` · expanded `rbac_test.go` · paging clamps

## Epic 18 %100 Gap Closure

| Task | Status |
|------|--------|
| T-218 Integrations accounts list (webhook_url + public_meta, secrets stripped) | Done |
| T-219–T-227 Admin settings (SLA, escalation merge, lost reasons, templates, fields, thresholds) | Done |
| T-228–T-229 Rooming + group list / CSV | Done |
| T-230 Dashboard KPI booked/collected/margin amounts | Done |
| T-231–T-232 Suggest / confirm next task (idempotent; never auto-create) | Done |
| T-233 Global search (customer/lead/booking/passport ILIKE) | Done |
| T-234 Events catalog | Done |
| T-235 Supplier issue history | Done |
| Settings / rooming / search FE surfaces | Done (FE) |

- Migration: `00021_epic18_gap_closure.sql` (escalation overlays, lost reasons, templates, field configs, thresholds, rooms, supplier issues)
- Domain: `adminconfig`, `rooming`, `search`, `task.SuggestNextTask`, `supplier.IssueEvent`
- Routes: `/v1/settings/*`, `/v1/events/catalog`, `/v1/search`, `/v1/departures/{id}/rooms|group-list`, `/v1/conversations/{id}/suggest-next-task|confirm-next-task`, `/v1/suppliers/{id}/issues`, `/v1/integrations/accounts`
- Permissions: `settings.read` / `settings.write` (GM/Admin/Manager write; Operations+Finance read)

## Design notes

- **ACID**: application services wrap multi-table writes in `tx.Manager.WithinTransaction` (payment ledger + booking balance; lead create + stage history).
- **SOLID**: domain ports in `internal/domain/*`; adapters depend inward; composition root in `internal/app/wire.go`.
- **Idempotency**: payments and auto-seeded tasks use `idempotency_key`.
- **Events**: published after successful commit (`booking.confirmed` → document/payment tasks).

## Epic 19 Security Foundation

| Task | Status |
|------|--------|
| T-236 Access scope model (`domain/access`, fail-closed) + role → level mapping | Done |
| T-237/T-238 Branch/owner-scoped repositories via `pgscope` (all business modules) | Done |
| T-239 Services/handlers resolve branch via scope (`request.Branch` / `TargetBranch`) | Done |
| T-240 Employee = own records, Manager = team, Finance/Ops = branch, GM/Admin = global | Done |
| T-241 Route-guard regression test (every `/v1` route → 403 without permission) | Done |
| T-242 `/customers` RBAC (`customers.read` / `customers.write`), `/dashboard/my-target` guarded | Done |
| T-243 Field redaction: cost/margin need `payments.read`, full passport needs `pii.read` | Done |
| T-246 `/v1/auth/me` returns permissions + scope | Done |
| T-248 Webhook signatures (Meta `X-Hub-Signature-256`, HMAC `X-Wodi-Signature`), Meta handshake | Done |
| T-249 Webhook branch resolved from integration account (`external_account_id`), no query param | Done |
| T-250 `webhook_events` idempotency log, per-IP rate limit, 1 MB body cap | Done |
| T-251 TOTP MFA (RFC 6238, encrypted secret, replay guard, recovery codes, GM/Admin enforced) | Done |
| T-253 Login rate limit + progressive lockout (15 min → 24 h), admin unlock | Done |
| T-255 Rotating refresh tokens with family reuse detection; revoke on password change/deactivation | Done |
| T-257 Security headers, HSTS, body limit, trusted-proxy client IP | Done |

**Scope rules.** Every authenticated request carries an `access.Scope`; repositories add predicates through
`pgscope.Append/Clause` and return NotFound for out-of-scope ids. Missing scope = 403 (fail closed).
Event reactors and worker jobs run with `access.System()`; verified webhooks with `access.ForBranch`.

**Login contract.** `POST /v1/auth/login` returns tokens, or `{mfa_required, mfa_challenge}`, or
`{mfa_enrollment_required, enrollment_token}` (GM/Admin without MFA get no tokens until
`/v1/auth/mfa/setup` + `/v1/auth/mfa/setup/confirm`). Locked: `423 account_locked` + `Retry-After`;
throttled: `429 rate_limited`. Refresh rotates on every call; reusing an old token revokes the family.

- Migration: `00022_epic19_security.sql` (MFA columns/challenges/recovery codes, lockout columns,
  `refresh_tokens` + revoke trigger, `integration_accounts.external_account_id`, `webhook_events`).
- Env: `ENCRYPTION_KEY` (base64 32 bytes, required in production), `ENCRYPTION_KEY_ID`,
  `ENCRYPTION_PREVIOUS_KEYS`, `TRUSTED_PROXIES`, `HTTP_MAX_BODY_BYTES`, `LOGIN_MAX_ATTEMPTS`,
  `LOGIN_LOCKOUT`, `LOGIN_LOCKOUT_MAX`, `LOGIN_WINDOW`, `LOGIN_IP_MAX_ATTEMPTS`, `MFA_ENFORCE`,
  `MFA_ISSUER`, `WEBHOOK_SECRET_<PROVIDER>`, `WEBHOOK_VERIFY_TOKEN_<PROVIDER>`, `WEBHOOK_RATE_LIMIT`,
  `WEBHOOK_RATE_WINDOW`.
- After deploy every user signs in once more (pre-rotation refresh tokens are invalid).
- Deferred to later epics: encrypted storage of channel secrets on connect (T-259, env fallback today),
  scheduling of the security cleanup job (T-279 scheduler; the job and ops endpoint exist).

### Session security

Server-side sessions (`auth_sessions`) back every login. The session id is the refresh-token family id and
the access token's `sid` claim; the middleware checks each request against the session and the user's
`token_version` (OWASP ASVS L2 V3, RFC 9700, BFF-ready).

| Token | Format | Lifetime | Storage | Revocation |
|-------|--------|----------|---------|------------|
| Access | JWT HS256, `kid` header; claims `iss`, `aud`, `sub`, `uid`, `sid`, `ver`, `iat`, `nbf`, `exp`, `jti` | `JWT_ACCESS_TTL` (15m) | not stored | session revoked → `401 session_revoked`; role/branch/team/active/password change bumps `users.token_version` → `401 token_stale` |
| Refresh | opaque `wrt_` + 256-bit base64url | idle `JWT_REFRESH_TTL` (7d), sliding, capped by `SESSION_ABSOLUTE_TTL` (30d) | SHA-256 hash only | rotated on every use; replay after `REFRESH_REUSE_GRACE` revokes the session |
| Session | UUID (`sid`) | `min(last refresh + idle, created + absolute)` | `auth_sessions` | logout, user/admin revoke, reuse, expiry, credential change (DB trigger) |

- Refresh within `REFRESH_REUSE_GRACE` of a rotation (concurrent tabs) returns a fresh sibling token
  (`auth.refresh_grace`); later replays revoke the session (`auth.refresh_reuse`). Refresh past the absolute
  limit returns 401 and ends the session with reason `expired`.
- **Revocation latency**: session checks are cached per instance for `SESSION_CHECK_CACHE_TTL` (default 10s).
  The revoking instance invalidates its cache immediately; other instances converge within the TTL.
  If the session store is unreachable the API fails closed with `503 service_unavailable`.
- Key rotation: set a new `JWT_ACCESS_SECRET` + `JWT_ACCESS_KEY_ID`, move the old pair into
  `JWT_PREVIOUS_ACCESS_SECRETS` for at least `JWT_ACCESS_TTL`, then drop it. Tokens without `kid` are rejected.
- Env: `JWT_ACCESS_KEY_ID` (`a1`), `JWT_PREVIOUS_ACCESS_SECRETS` (`kid:secret,...`), `JWT_AUDIENCE`
  (`wodi-crm-api`), `SESSION_ABSOLUTE_TTL` (`720h`, ≤ 90d in production), `SESSION_CHECK_CACHE_TTL` (`10s`),
  `REFRESH_REUSE_GRACE` (`10s`, `0` disables). `JWT_REFRESH_SECRET` is removed.
- Endpoints:
  - `POST /v1/auth/logout` `{"refresh_token"}` → always 200; needs no valid session, so it works after the
    access token expires. Revokes the refresh token's session and, if a signed Bearer is sent, its `sid` session.
  - `GET /v1/auth/sessions` → `{"data":[{"id","created_at","last_seen_at","last_ip","user_agent","auth_method","idle_expires_at","absolute_expires_at","current"}]}`
  - `DELETE /v1/auth/sessions/{id}` → `{"data":{"revoked":true}}` (404 unless the caller's active session; current = logout)
  - `POST /v1/auth/sessions/revoke-others` → `{"data":{"revoked":<int>}}`
  - `POST /v1/users/{id}/sessions/revoke` (`users.write`) → `{"data":{"revoked":<int>}}`
  - `POST /v1/ops/security-cleanup` (`users.write`) purges sessions/refresh tokens revoked or expired > 30 days,
    expired MFA challenges and webhook events > 90 days (also worker job `security.cleanup`).
- Migration: `00023_session_security.sql`. Existing refresh tokens are revoked; every user signs in once more.

## Go-Live Backlog (Epic 19–26)

Monzer.pdf %100 uyum ve canlıya çıkış için 110 task (T-236–T-345), sprint sırası ve kabul kriterleri: [`docs/GO_LIVE_BACKLOG.md`](docs/GO_LIVE_BACKLOG.md).
