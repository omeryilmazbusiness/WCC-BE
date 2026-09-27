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
- `GET /v1/search?q=...&kind=customer,lead,booking,passport&limit=20` — `q` ≥ 2 characters; `kind` (comma-separated or
  repeated) narrows the kinds, omitted = all; an unknown kind is `400 validation_error`. Hits: `{kind,id,title,subtitle,href_hint,score}`
  (passport hits carry the participant id and a masked last-four subtitle).
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
- Deferred to later epics: scheduling of the security cleanup job (T-279 scheduler; the job and ops endpoint exist).

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

## Epic 20 Audit Trail & Immutable Ledger

| Task | Status |
|------|--------|
| T-263 Actor from request context; before/after on booking, payment, capacity, document review, target, settings | Done |
| T-264 Fail-closed audit inside the business transaction; append-only `audit_events` (trigger + REVOKE) | Done |
| T-265 Immutable payment ledger (DB trigger + `payment.StatusTransitions` in Go) | Done |

**Actor model.** `audit.Actor{Type, UserID, SessionID, BranchID, IP, UserAgent, RequestID}` rides on the
request context. `AuditContext` (global) records IP/UA/request id for every request, `Authenticate` upgrades
it to the verified user (`actor_type=user`, `session_id` = token `sid`), webhook routes set `webhook`, and
event reactors / worker jobs run as `system` (`audit.AsSystem`, request id kept for correlation).
`RecordInput.ActorID` is only a fallback: a verified user on the context always wins, so a service cannot
attribute an event to someone else. Without any actor the event is `system` with a NULL `actor_id`.

**Storage.** `audit_events` gains `actor_type`, `session_id`, `request_id`, dedicated `before`/`after` JSONB
columns (`metadata` now holds only extra context) and `row_hash` (SHA-256 of the canonical row, set by a
trigger). Tamper check: `SELECT id FROM audit_events a WHERE row_hash <> audit_event_hash(a)`.

**Append-only.** Triggers reject `UPDATE`/`DELETE` (row) and `TRUNCATE` (statement); `UPDATE, DELETE,
TRUNCATE` are revoked from `PUBLIC` and from the migrating (application) role. No code path updates or
deletes audit rows; the security retention job never touches `audit_events`.

**Fail closed.** These actions write their audit row in the same transaction as the change and roll back if
the insert fails: `booking.created`, `booking.updated`, `booking.discount_changed`, `booking.status_changed`
(confirm/cancel/complete, with departure `capacity_sold` before/after), `booking.participant_added|updated|removed`
(passport never stored, only `passport_on_file`), `booking.line_items_changed`, `booking.readiness_overridden`,
`payment.recorded|verified|reversed|adjusted|refund_requested|refund_approved|refund_rejected` (with booking
money before/after), `payment.schedule_created|cancelled`, `finance.reporting_currency_set`,
`departure.updated|capacity_changed|sales_closed|sales_reopened|marked_full|capacity_recomputed`,
`document.approved|rejected`, `revenue_target.created|updated|weights_changed|shares_changed`,
`settings.sla_updated|escalation_updated|escalation_deleted|fields_updated|thresholds_updated`, plus the existing
user/branch/customer events and `audit.exported`. `ai.settings_updated` / `ai.disabled` (provider, model,
enabled, `key_rotated`; never the key) and auth/webhook events stay best-effort.

**Payment ledger.** `payments_ledger_guard` rejects `DELETE`/`TRUNCATE` and any change to `amount`, `currency`,
`booking_id`, `event_type`, `reverses_payment_id`, `recorded_by`, `idempotency_key`, `created_at`, `method`,
`reference`. `approved_by`, `approved_at` and `note` can be set once. Allowed status transitions (mirrored by
`payment.StatusTransitions`, a test keeps both in sync):

| event_type | from | to |
|------------|------|----|
| charge | unverified | verified |
| refund | pending_approval | approved (needs approved_by/at) |
| refund | pending_approval | rejected (needs approved_by/at) |

Corrections are new ledger rows (`reverse`, `adjust`, `refund`), never edits.

**API** (`audit.read`, branch-scoped like other reads):
- `GET /v1/audit-events?actor_id&entity_type&entity_id&action&from&to&branch_id&limit&offset` →
  `{"data":[{"id","actor_id","actor_name","actor_type","action","entity_type","entity_id","branch_id","before","after","extra","ip","user_agent","session_id","request_id","created_at"}],"meta":{"total","limit","offset","page","total_pages"}}`
  (`from`/`to` accept RFC3339 or `YYYY-MM-DD`; a date-only `to` covers the whole day; `actor_name` is the
  user's `full_name` or `""`).
- `GET /v1/audit-events/export.csv` (same filters) streams up to 50,000 rows with header
  `created_at,actor_name,actor_type,action,entity_type,entity_id,branch_id,ip,session_id,request_id,before,after,extra`;
  cells starting with `= + - @` are prefixed with `'`. The export is audited (`audit.exported`) before any row is sent.
- `GET /v1/audit-events/actions` → `{"data":["booking.status_changed", ...]}` (distinct actions in scope).
- Migration: `00025_epic20_audit_ledger.sql` (backfills `before`/`after`/`actor_type` from `metadata`).

## Epic 20 — Data protection

| Task | Status |
|------|--------|
| T-259 Integration / AI / external-integration / file-sync secrets sealed into `secrets_enc` (AES-256-GCM, AAD = table + row + branch); UI gets hints (`••••1234`) | Done |
| T-260 Passports encrypted (`passport_enc`), blind index (`passport_hash`) for dedupe/search, `passport_last4`; exports masked without `pii.read` | Done |
| T-261 Audited passport reveal (`pii.revealed`); every normal read is masked | Done |
| T-267 KVKK export bundle (`privacy.exported`) and anonymization (`privacy.anonymized`), `privacy.manage` (GM, Admin) | Done |

**Secrets.** Config keys containing `secret`, `token`, `password`, `api_key`, `private_key` or `credential`
leave `config_json` and are sealed into `secrets_enc`; the ciphertext only opens for the same table, row and
branch. Rows not yet backfilled are still read from `config_json`. Inbox connect returns the webhook
`verify_token` once (`{"data":{..., "verify_token":"..."}}`), stores only `verify_token_hash`, and fails closed
when encryption is not configured. External integrations and file-sync responses carry `secret_hints`.

**Passports.** New writes store `passport_no = ''` plus `passport_enc`, `passport_hash`, `passport_last4`.
Customer, companion, participant, search and rooming JSON always return the masked value (`••••5678`) and
`passport_last4`; the rooming CSV and customer Excel export contain the full number only with `pii.read`.
Dedupe (`FindByPassport`) and search match the blind index exactly (no partial passport search). Masked values
in an import never overwrite a stored passport.

**API**
- `POST /v1/customers/{id}/reveal-passport` (`customers.read` + `pii.read`) →
  `{"data":{"passport_no":"U12345678"}}`, `Cache-Control: no-store`.
- `POST /v1/bookings/{id}/participants/{participantId}/reveal-passport` (`bookings.read` + `pii.read`) → same shape.
  Reveals are audited (`pii.revealed`, `extra.field = "passport"`) before the value is returned; if the audit
  write fails nothing is revealed. Limit: 30 reveals per user per hour → `429 rate_limited` + `Retry-After`.
- `GET /v1/customers/{id}/export` (`privacy.manage`) → JSON attachment
  `{"data":{"generated_at","customer":{..., "passport_no"},"bookings":[{..., "participants":[{"id","full_name","nationality","date_of_birth","passport_last4"}]}],"payments_summary":[{"currency","count","total"}],"documents":[...],"conversations":[{..., "message_count"}],"companions":[...]}}`
  (audited `privacy.exported`).
- `POST /v1/customers/{id}/anonymize` (`privacy.manage`) `{"reason":"at least 10 characters"}` →
  `{"data":{"customer_id","full_name":"Anonymized 1a2b3c4d","anonymized_at"}}`. Errors: `400 validation_error`
  (reason), `404 not_found` (unknown or out of scope), `409 customer_has_active_bookings`
  (any booking not `completed` or `cancelled`), `409 customer_already_anonymized`. One transaction clears the customer's
  PII and passport, deactivates it, clears matching booking participants, lead contact fields, channel
  identities and conversation subjects, deletes companion links and writes `privacy.anonymized`
  (`extra.reason`). Payments and audit events are append-only and stay untouched.
- `POST /v1/ops/encrypt-backfill` (`users.write`) optional `{"rehash":true}` →
  `{"data":{"secrets":{"integration_accounts":n,"ai_settings":n,"external_integrations":n,"file_sync_connections":n},"passports":{"customers":n,"booking_participants":n}}}`
  (audited `ops.encrypt_backfill`; also worker job `security.encrypt_backfill` with payload `{"rehash":bool}`).

**Backfill procedure**
1. Set `ENCRYPTION_KEY`, `ENCRYPTION_KEY_ID` and `ENCRYPTION_BLIND_INDEX_KEY` (all base64 32 bytes); keep the
   blind index key fixed, otherwise it follows the active key and changes on rotation.
2. Run migration `00024_epic20_pii_secrets.sql`, deploy API and worker.
3. `POST /v1/ops/encrypt-backfill` (idempotent, batched, safe to rerun and to run while traffic flows; rows
   changed concurrently are skipped and picked up next run). It moves plaintext into `secrets_enc` /
   `passport_enc`, blanks `passport_no`, and re-seals values under a non-active key.
4. Verify: `SELECT count(*) FROM customers WHERE passport_no <> ''` and the same for `booking_participants`
   return 0, and secret keys are gone from `config_json` (see [`docs/SECRETS.md`](docs/SECRETS.md)).
5. Key rotation: new key + id, old pair into `ENCRYPTION_PREVIOUS_KEYS`, deploy, rerun step 3, then drop the old
   key. After changing the blind index key run the backfill with `{"rehash":true}`.

- Migration: `00024_epic20_pii_secrets.sql` (`secrets_enc` on `integration_accounts`, `ai_settings`,
  `external_integrations`, `file_sync_connections`; `integration_accounts.verify_token_hash`; `passport_enc`,
  `passport_hash`, `passport_last4` on `customers` and `booking_participants`; `customers.anonymized_at`).
- Env: `ENCRYPTION_BLIND_INDEX_KEY`.
- Deferred: dropping `customers.passport_no` / `booking_participants.passport_no` in a follow-up migration once the
  backfill is verified in every environment (the columns stay as the rolling-deploy fallback and are always
  written as `''`). Message bodies, stored document files and audit history are not rewritten by anonymization
  (retention policy); verify-token hashes are case-insensitive.

## Epic 21 — Finance

| Task | Status |
|------|--------|
| T-272 FX rates (`fx_rates`, 8-decimal fixed point), converter, payment + booking reporting snapshots, optional provider sync | Done |
| T-274 Payment promises, follow-up / broken-promise tasks, new financial summary | Done |
| T-276 Refund segregation of duties, `auto_verify` needs `payments.approve`, `received_at` | Done |
| T-277 Finance queues are read-only; overdue schedules maintained by a job | Done |
| T-278 Finance CSV export: UTF-8 BOM, formula neutralization, audited `finance.exported` | Done |
| Live exchange rates (Damascus): LiraScope + ExchangeRate-API board, audited adopt, optional accounting sync | Done |

**Money.** Amounts are integer minor units (2 decimals for every currency). Rates are stored as
`rate_scaled = rate × 10^8` and travel as decimal strings (`"3.75000000"`). Conversion is
`round_half_away_from_zero(amount × rate_scaled / 10^8)` with big integers. When only the reverse pair exists
the rate is inverted to 8 decimals first (the snapshot stores that inverted rate, so every stored amount is
reproducible). The newest rate with `effective_date ≤ on` wins; direct beats inverse on the same date. Rates are
company-wide. Business dates (`received_at`, `promised_on`, "today") use `SCHEDULER_TZ`.

**Snapshots.** Every new ledger entry stores `amount_reporting`, `reporting_currency`, `fx_rate_scaled`,
`fx_effective_date` (branch reporting currency, rate as of `received_at`) on INSERT; the ledger trigger
(`payments_ledger_guard`) keeps them and `received_at` immutable. Reversals copy the negated original snapshot.
No rate → the entry is recorded with NULLs and `"fx_missing": true`. Confirming a booking snapshots its total
into `bookings.total_reporting` (same columns) via the `booking.confirmed` reactor.

**API**
- `GET /v1/fx-rates?base&quote&from&to&page&limit` (`payments.read`) →
  `{"data":[{"id","base","quote","rate":"3.75000000","effective_date":"2026-09-01","source","created_by","created_at"}],"meta":{"total","limit","offset","page","total_pages"}}`.
- `POST /v1/fx-rates` (`fx.manage`: GM, finance) `{"base":"USD","quote":"SAR","rate":"3.75","effective_date":"2026-09-01","source":"manual"}`
  → `201` rate object; `409 fx_rate_exists` for a duplicate pair/date.
- `PUT /v1/fx-rates/{id}` (`fx.manage`) `{"rate":"3.751","source":"corrected"}` → rate object (existing snapshots never change).
- `DELETE /v1/fx-rates/{id}` (`fx.manage`) → `{"data":{"deleted":true}}`. Mutations are audited
  (`fx.rate_created|updated|deleted`, before/after) in the same transaction.
- `GET /v1/fx-rates/convert?amount=10000&from=SAR&to=USD&on=2026-09-20` (`payments.read`) →
  `{"data":{"amount":10000,"from":"SAR","to":"USD","converted":2667,"rate":"0.26666667","effective_date":"2026-09-01"}}`;
  `404 fx_rate_not_found`.
- `POST /v1/payments` accepts `received_at` (`YYYY-MM-DD`, default today, not in the future).
  `auto_verify: true` without `payments.approve` → `403 forbidden_auto_verify`. Payment JSON adds
  `"received_at"`, `"reporting":{"currency","amount","rate","effective_date"}|null`, `"fx_missing"`.
- `POST /v1/payments/{id}/approve` by the refund requester → `403 sod_violation` (the requester may still reject).
- `GET /v1/bookings/{id}/financial-summary` (`payments.read`) →
  `{"data":{"currency","subtotal","discount","tax","fees","total","cost","margin","collected","pending","balance","reporting":{"currency","total","collected","balance","rate","effective_date"}|null,"promises":{"open_count","open_amount","next_promised_on"}}}`.
  `subtotal/tax/fees` come from line items (`kind` item/tax/fee); `balance = total − collected` (negative = credit);
  `pending` = unverified charges; `cost`/`margin` follow the booking redaction rule (`payments.read`); `reporting` uses
  the confirmation snapshot rate, else today's rate, else `null`.
- `GET /v1/bookings/{id}/payment-promises` (`payments.read`) →
  `{"data":[{"id","booking_id","amount","currency","promised_on","note","status","task_id","created_by","created_at","resolved_at"}]}`;
  `status` is the effective one (`open|kept|broken|cancelled`) — kept once verified/approved collections recorded since the
  promise was created cover its amount, broken after `promised_on`.
- `POST /v1/bookings/{id}/payment-promises` (`payments.write`) `{"amount":20000,"promised_on":"2026-10-05","note":"..."}`
  (currency = booking currency) → `201` promise; creates a follow-up task for the booking owner due 09:00 on `promised_on`.
- `POST /v1/payment-promises/{id}/cancel` (`payments.write`) → promise (`422 invalid_state` unless open).
- `GET /v1/finance/queues/{kind}` never writes. `GET /v1/finance/export?kind=` returns a UTF-8 BOM CSV whose text cells
  starting with `= + - @ \t \r` are prefixed with `'`; `finance.exported` (`extra.filters`, `rows`, `format`) is
  audited before any byte is sent.

**Jobs** (scheduler zone `SCHEDULER_TZ`)
- `finance.promises_check` `0 8 * * *` — persists kept/broken, closes the follow-up task (kept) or creates an urgent
  task + in-app notification for the owner (broken); audited as system.
- `finance.schedules_overdue` `0 * * * *` — flips open schedules past `due_at` to `overdue` in batches of 500, one
  audit row (`payment.schedule_overdue`, system) per schedule; idempotent.
- `fx.rates_sync` `30 6 * * *` — scheduled when `FX_PROVIDER_URL` is set or `FX_ACCOUNTING_SOURCE` is not `off`;
  inserts missing rates for today (`GET {url}?base={FX_PROVIDER_BASE}&date=YYYY-MM-DD` → `{"rates":{"SAR":"3.75"}}`,
  plus live rates, see below), never overwrites manual ones. A failing provider does not block the others.
- `fx.live_sync` `*/10 * * * *` — scheduled when `FX_LIVE_ENABLED`; refreshes the live board (below).

- Migration: `00027_epic21_finance.sql` (`fx_rates`; payment snapshot columns + `received_at`; extended ledger
  trigger; booking snapshot columns; `payment_promises`).
- Env: `FX_PROVIDER_URL`, `FX_PROVIDER_BASE` (default `USD`), `FX_PROVIDER_TIMEOUT` (default `10s`).

### Live exchange rates (Damascus)

A read-only rate board for the Syria deployment. It is informational: accounting only ever uses `fx_rates`, and a
live quote reaches `fx_rates` through a human "adopt" (or the opt-in daily accounting sync).

**Sources**
- **LiraScope** (`GET {LIRASCOPE_BASE_URL}/rates/latest?lang=en`, Syria-hosted, no key; limit 60 req/min, 1200/h per
  IP) — `cbsRates` = Central Bank of Syria **official** rate (currently USD only), `marketRates` = Damascus parallel
  **market**. Values are SYP per 1 unit; every quote keeps its own `observed_at` (many market quotes are months old).
  Optional `LIRASCOPE_API_KEY` / `LIRASCOPE_API_SECRET` are sent as `X-Api-Key` / `X-Api-Secret` only when set.
- **ExchangeRate-API** open access (`GET {ERAPI_BASE_URL}/latest/USD`, no key) — international **reference** crosses
  (units per 1 USD), updated daily. Polled at most every 6 h and never before `time_next_update_unix`. Its terms
  **require attribution**: the UI must show "Rates By Exchange Rate API" linking to https://www.exchangerate-api.com
  wherever these values (`usd_cross`, derived quotes) appear; the source entry carries `attribution` for this.

**Redenomination.** Syria redenominated on 2026-01-01 (1 new SYP = 100 old; ISO code still `SYP`). All values are new
SYP. A payload quoting more than 2000 SYP per USD is old-lira data: that source's whole payload is rejected (source
`ok:false` with an error), never divided by 100.

**Official vs market.** Per currency: `official` is the CBS quote, or — when CBS publishes none — `derived`
= CBS USD ÷ reference units-per-USD. `market` is the direct LiraScope quote when observed within
`FX_LIVE_MARKET_MAX_AGE` (48 h); otherwise it is derived from the market USD rate the same way (`derived:true`,
`stale` if the market USD itself is old); with no derivation possible an old direct quote is returned with
`stale:true`; else `null`. Numbers are 8-decimal fixed point with half-away-from-zero rounding (no floats); e.g.
official EUR = 122 ÷ 0.877506 = `139.03038840`.

**Resilience.** Sources are fetched independently; a failure is stored per source (`ok:false`, `error`) while the
last good quotes and their `fetched_at` stay on the board. Snapshots live in `fx_live_snapshots` (one row per
source + kind, quotes as decimal strings). The board is cached in-process for 30 s. `stale` is `true` when the newest
successful LiraScope fetch is older than 1 h (or never happened).

**API**
- `GET /v1/fx/live` (any authenticated user; `Cache-Control: private, max-age=60`) →
  `{"data":{"local_currency":"SYP","updated_at":RFC3339|null,"stale":bool,"quotes":[{"currency":"USD","pinned":true,"official":{"buy","sell","mid","observed_at","derived"}|null,"market":{"buy","sell","mid","observed_at","derived","stale"}|null,"usd_cross":"1.00000000"|null}],"sources":[{"id","name","url","attribution"?,"kinds","ok","fetched_at","error"}],"disclaimer"}}`.
  Quotes: pinned first (`FX_LIVE_PINNED` order), then `FX_LIVE_CURRENCIES`. `404 fx_live_disabled` when
  `FX_LIVE_ENABLED=false`.
- `POST /v1/fx/live/refresh` (`fx.manage`) — syncs inline (20 s budget; a source is not refetched within 30 s, and
  ExchangeRate-API never before its next update) → same board.
- `POST /v1/fx-rates/adopt` (`fx.manage`) `{"currency":"USD","kind":"official|market","side":"mid|buy|sell","effective_date":"YYYY-MM-DD"}`
  (`effective_date` defaults to today in `SCHEDULER_TZ`) → `201` rate object (as in `GET /v1/fx-rates`) with
  `base=currency`, `quote=FX_LOCAL_CURRENCY`, `source` `lirascope:<kind>:<side>` or `derived:<kind>:<side>`;
  `409 fx_rate_exists`; `422 live_quote_unavailable` when that quote is `null`. Audited as `fx.rate_adopted`
  (`extra`: kind, side, derived, stale, observed_at) in the same transaction (fail closed).

**Accounting sync.** With `FX_ACCOUNTING_SOURCE=official|market`, the daily `fx.rates_sync` also stores today's
mid of each pinned currency → local currency via insert-if-absent (source `lirascope:<kind>`, or `derived:<kind>`
for derived values). It skips stale market quotes, writes nothing when the board is stale and never overwrites a
manual rate.

**Config** — `FX_LIVE_ENABLED` (`true`), `FX_LOCAL_CURRENCY` (`SYP`), `FX_LIVE_PINNED` (`USD,EUR,SAR`),
`FX_LIVE_CURRENCIES` (`USD,EUR,SAR,TRY,AED,GBP,JOD,EGP,KWD,QAR,LBP,IQD`), `FX_LIVE_MARKET_MAX_AGE` (`48h`),
`FX_LIVE_TIMEOUT` (`10s`), `LIRASCOPE_BASE_URL` (`https://lirascope.syria-cloud.sy/api/v1`), `ERAPI_BASE_URL`
(`https://open.er-api.com/v6`), `LIRASCOPE_API_KEY` / `LIRASCOPE_API_SECRET` (optional secrets),
`FX_ACCOUNTING_SOURCE` (`off`). Migration `00028_fx_live_snapshots.sql`.

## Epic 21 — Booking lifecycle

| Task | Status |
|------|--------|
| T-268 Nine statuses, pure state machine, `POST /v1/bookings/{id}/status`, `allowed_transitions` | Done |
| T-269 Automatic transitions (derived paid/ready, option hold expiry, travelled) + jobs | Done |
| T-271 Manager/GM override (`bookings.override`), readiness override folded in | Done |
| T-274 (booking side) Line `kind` item/tax/fee, `subtotal − discount + tax + fees = total`, `bookings.discount` | Done |

The rules live in `internal/domain/booking/lifecycle.go` (no I/O); `internal/app/booking` loads facts, locks rows and
audits. Seat-holding statuses are `option_hold, confirmed, partially_paid, ready, travelled, completed`;
`departures.capacity_sold` is recomputed from them under a `FOR UPDATE` lock on the departure, and each seat change
records `capacity_sold_before/after`.

| From | To | Actor | Guards |
|------|----|-------|--------|
| draft | quoted, option_hold, confirmed, cancelled | user | see below |
| quoted | draft, option_hold, confirmed, cancelled | user | |
| option_hold | quoted, confirmed, cancelled | user | |
| option_hold | draft (`hold_expired`) | system | `hold_not_expired` |
| confirmed ⇄ partially_paid ⇄ ready | each other (`derived`) | system | target must equal `derive(collected, balance, readiness_ok)` |
| confirmed, partially_paid | ready | override | readiness / balance bypassed and recorded |
| confirmed, partially_paid, ready | cancelled | user | reason required |
| confirmed, partially_paid, ready | travelled (`departed`) | system | `departure_not_reached` |
| travelled | completed | user | |
| cancelled, completed | — | — | terminal |

- Entering `option_hold` or the confirmed family from outside: `customer_required`, `departure_required`,
  `sales_closed`, `no_capacity` (seat acquisition only). `option_hold` needs `hold_expires_at` in the future and at
  most 14 days away (`hold_expiry_required|in_past|too_far`). `cancelled` needs a reason (`reason_required`).
- `derive`: `collected = 0` → `confirmed`; `balance ≤ 0` and readiness OK → `ready`; otherwise `partially_paid`.
  Readiness OK = participants complete + required checklist + required documents (a readiness override lifts
  checklist/documents, never participants). A forced `ready` (`ready_forced`) sticks until the booking leaves the family.
- Override: `bookings.override` (manager, GM), reason ≥ 10 characters (`override_reason_too_short`); bypasses only
  `readiness_incomplete` / `balance_outstanding`; capacity, customer and departure stay hard. Audited as
  `booking.status_overridden` with `extra.guards_bypassed`. `POST /{id}/readiness-override` now requires
  `bookings.override` and re-derives the status.
- Every change writes `booking.status_changed` (`before/after` `{status, hold_expires_at[, status_reason]}`,
  `extra.actor_kind`) in the same transaction; automatic ones use `actor_type = system`.

**API**
- `POST /v1/bookings/{id}/status` (`bookings.write`)
  `{"status":"option_hold","reason":"...","hold_expires_at":"2026-10-01T12:00:00Z","override":false}` → `{"data":booking}`.
  `409 invalid_transition` (no such edge for the actor), `422 guard_failed` with
  `{"error":{"code":"guard_failed","details":{"guards":["no_capacity"]}}}`, `403` for `override:true` without
  `bookings.override`, `400` for a non-RFC3339 `hold_expires_at`. `POST /{id}/confirm` is `{"status":"confirmed"}`.
- Booking JSON adds `status_changed_at`, `status_reason`, `hold_expires_at` (RFC3339 or `null`), `subtotal_amt`,
  `tax_amt`, `fee_amt` and `allowed_transitions: [{"status","requires_reason","requires_override"}]` (override
  edges only for callers holding `bookings.override`).
- `GET /{id}/readiness` adds `readiness_ok` and `confirm_guards`; confirming no longer requires readiness.
- Line items: `{"kind":"item|tax|fee","category":"package|hotel|room|transport|flight|extras","label",...}`;
  `category` is required for `item` and empty for tax/fee (legacy `{"kind":"hotel"}` is read as item/hotel).
  `total = subtotal − discount + tax + fees`; the discount applies to the item subtotal and cannot exceed it.
  Line JSON adds `category`. Lines, participants deletion and PATCH totals are editable only in draft/quoted.
- Setting or changing `discount_amt` (create or PATCH) needs `bookings.discount` (manager, GM) → otherwise `403`.

**Jobs** (scheduler zone `SCHEDULER_TZ`, all idempotent, keyset batches of 200)
- `booking.hold_expiry` `*/5 * * * *` — lapsed holds → draft, seats released, high-priority follow-up task for the owner.
- `booking.travelled_sweep` `15 0 * * *` — confirmed-family bookings on/after `depart_date` → travelled; the audit
  records `collected_amt`, `balance_amt`, `readiness_ok` at departure.
- `booking.recompute_sweep` `45 * * * *` — reconciles derived statuses the payment service changes without an event.
- `booking.recompute` `{"booking_id":"..."}` — on-demand single recompute. In-process, `payment.recorded`,
  participant, checklist and readiness-override changes recompute immediately.

- Migration: `00026_epic21_booking_lifecycle.sql` — 9-value status CHECK; `hold_expires_at`, `status_changed_at`,
  `status_reason`, `ready_forced`, `tax_amt`, `fee_amt`; confirmed bookings with collections become `partially_paid`
  (audited as system); `capacity_sold` resynced from seat statuses (audited); line `kind` → `category`, new
  `kind` item/tax/fee. Down is lossy (quoted/option_hold → draft, partially_paid/ready/travelled → confirmed,
  tax/fee lines → extras).
- Deferred: automatic `travelled → completed` after `return_date`, hold extension endpoint, recompute on document
  review (covered by the hourly sweep).

## Epic 22 — Automation, events & realtime

| Task | Status |
|------|--------|
| T-279 Cron scheduler (`SCHEDULER_TZ`, per-job `SCHEDULE_<JOB>` override / `off`) | Done |
| T-280 Real worker handlers (expiry reminders, webhook retry, scheduled reports, AI daily summary) | Done |
| T-281 Transactional outbox + dispatcher with backoff, dead letters | Done |
| T-282 Event catalog (`GET /v1/events/catalog`) | Done |
| T-283 Subscriptions (in-process bus + durable outbox subscribers) | Done |
| T-284 Notification triggers, SLA A (warning) / B (breach) as % of the policy window | Done |
| T-285 Escalation rules per kind (branch override, enable/disable) | Done |
| T-286 Automatic tasks with `source_rule` provenance | Done |
| T-288 Server-sent events `GET /v1/stream` | Done |

**Scheduler** (worker, `SCHEDULER_TZ`, every job idempotent via the `scheduled_job_runs` ledger)
- The default table lives in `internal/app/worker_schedule.go`. Override one job with
  `SCHEDULE_<JOB>` (job name upper-cased, `.`/`-` → `_`), e.g. `SCHEDULE_INBOX_SLA_SWEEP="*/2 * * * *"`,
  or disable it with `SCHEDULE_AI_SUMMARY_DAILY=off`. An unknown key fails startup.
- `finance.payment_overdue` `35 * * * *` — one alert per payment schedule once it is
  `thresholds.payment_overdue_hours` (default 12) past due; looks back 30 days, skips cancelled/draft bookings.
- Money in alert bodies is formatted from integer minor units (`shared.FormatMinor`, no floats).

**Outbox / events**
- Durable events are written to `outbox_events` in the business transaction and delivered by the worker
  dispatcher; after delivery it announces `invalidate` (topic = event name) on the `wcc_events` channel.
- Notification changes are announced by the `notifications_realtime` trigger.
- Ops: `GET /v1/ops/outbox`, `GET /v1/ops/outbox/dead` (`ops.read`), `POST /v1/ops/outbox/{id}/requeue` (`users.write`).
  `automation.outbox_purge` `30 3 * * *` removes delivered rows.

**Settings** (`settings.read` / `settings.write`)
- `GET|PUT /v1/settings/sla` — `[{"channel","first_response_seconds"}]`.
- `GET /v1/settings/escalation` · `PUT /v1/settings/escalation/{kind}` `{"escalate_after_seconds","escalate_to_roles","enabled"}`
  · `DELETE /v1/settings/escalation/{kind}` (back to the default rule).
- `GET|PUT /v1/settings/thresholds` — hours fields 1–720, `sla_warn_pct` 10–100 below `sla_breach_pct` 50–300,
  `visa_follow_up_days` 1–90.

**Notifications**
- `GET /v1/notifications?kind=...` filters by kind; `GET /v1/notifications/summary` groups active notifications
  per kind: `{"kind","severity","open","acknowledged","occurrences","latest_at","title","href"}`.
- Reports: `GET|POST /v1/reports/schedules`, `DELETE /v1/reports/schedules/{id}`, `GET /v1/reports/runs`,
  `GET /v1/reports/runs/{id}/download` (`reports.export`).

**Realtime** — `GET /v1/stream` (authenticated, `text/event-stream`)
- Each API replica `LISTEN`s on Postgres and fans signals out to its connected clients, filtered by the caller's
  visibility. Events: `notification` (recipient only) and `invalidate` `{"topic":"payment.reversed",...}`.
- Payloads carry ids only; clients refetch through the normal API, so permissions are never bypassed.
- `expired` is sent when the access token lapses (client reconnects with a fresh token); `503` when the hub is full.
  Streams close on graceful shutdown. Headers: `Cache-Control: no-store, no-transform`, `X-Accel-Buffering: no`.
- The FE opens one stream per tab through the BFF proxy (`/api/proxy/stream`), polls only while it is down and
  refreshes affected screens silently on `invalidate`.

- Migration: `00029_epic22_automation.sql` — `outbox_events`, `scheduled_job_runs`, `report_schedules`,
  `report_runs`, task provenance, SLA A/B columns, branch timezone, passport expiry, webhook retry bookkeeping,
  notification realtime trigger.

## Go-Live Backlog (Epic 19–26)

Monzer.pdf %100 uyum ve canlıya çıkış için 110 task (T-236–T-345), sprint sırası ve kabul kriterleri: [`docs/GO_LIVE_BACKLOG.md`](docs/GO_LIVE_BACKLOG.md).
