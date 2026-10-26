# Railway production

WCC is three processes plus Postgres, Redis, and S3-compatible storage. Railway does
not run that as a single service.

```
wodi (Railway project)
├── Postgres          plugin
├── Redis             plugin
├── api               this repo, start /app/api, pre-deploy migrate
├── worker            this repo, start /app/worker
└── web               WCC-FE repo, Next.js standalone
```

## 1. Create the project and plugins

1. [railway.app](https://railway.app) → New Project → Empty.
2. Add **PostgreSQL**.
3. Add **Redis**.
4. Create a Cloudflare R2 (or AWS S3) bucket named `wodi-crm`. Railway has no object store.

## 2. API service (this repo)

1. New Service → GitHub → `WCC-BE`.
2. Settings → Config-as-code: `railway.toml` (already in the repo).
3. Settings → Networking → Generate domain (webhooks and public health).
4. Variables → add the API list below. Link Postgres and Redis so `DATABASE_URL` and `REDIS_URL` appear.
   Prefer the **private** plugin URLs.
5. First deploy runs `/app/migrate -command=up` then `/app/api`.
6. Confirm `https://<api-domain>/healthz` and `/readyz` return 200.

Worker is a **second service from the same repo**:

1. New Service → GitHub → `WCC-BE` again.
2. Config-as-code: `railway.worker.toml`, or set start command `/app/worker` and **remove** the HTTP health check.
3. Share the same variables as the API (Raw Editor → copy). Do not run migrate here.

## 3. Frontend service (WCC-FE)

1. New Service → GitHub → `WCC-FE`.
2. Config-as-code: `railway.toml`.
3. Generate a public domain.
4. Add the web variables below. `NEXT_PUBLIC_*` is baked in at **build** time — change them, then Redeploy.
5. Confirm `https://<web-domain>/api/health` returns `{"status":"ok"}`.

## 4. API variables

| Key | Value |
| --- | --- |
| `APP_ENV` | `production` (also auto if `RAILWAY_ENVIRONMENT` is set) |
| `APP_NAME` | `wodi-crm-be` |
| `LOG_LEVEL` | `info` |
| `LOG_FORMAT` | `json` |
| `DATABASE_URL` | from Postgres plugin (private) |
| `REDIS_URL` | from Redis plugin (private) |
| `CORS_ORIGINS` | `https://<web-domain>` (no trailing slash) |
| `JWT_ACCESS_SECRET` | `openssl rand -base64 48` (≥ 32 chars) |
| `JWT_ACCESS_KEY_ID` | `a1` |
| `JWT_ISSUER` | `wodi-crm-be` |
| `JWT_AUDIENCE` | `wodi-crm-api` |
| `ENCRYPTION_KEY` | `openssl rand -base64 32` |
| `ENCRYPTION_KEY_ID` | `k1` |
| `ENCRYPTION_BLIND_INDEX_KEY` | `openssl rand -base64 32` (set once, never rotate casually) |
| `S3_ENDPOINT` | R2/S3 endpoint, e.g. `https://<account>.r2.cloudflarestorage.com` |
| `S3_REGION` | `auto` (R2) or the AWS region |
| `S3_BUCKET` | `wodi-crm` |
| `S3_ACCESS_KEY` | access key |
| `S3_SECRET_KEY` | secret key |
| `S3_USE_SSL` | `true` |
| `S3_PUBLIC_URL` | public/CDN URL for downloads |
| `SENTRY_DSN` | `wodi-crm-be` DSN |
| `SCHEDULER_TZ` | `Asia/Riyadh` |

Do **not** set `HTTP_ADDR`. The process listens on Railway’s `PORT`.

Optional: `TRAVELPAYOUTS_TOKEN`, `LIRASCOPE_API_KEY`, `PAYMENT_LINK_URL_TEMPLATE`.

## 5. Web variables

| Key | Value |
| --- | --- |
| `SESSION_SECRET` | `openssl rand -base64 48` (≥ 32 chars) |
| `API_BASE_URL` | `http://${{api.RAILWAY_PRIVATE_DOMAIN}}:${{api.PORT}}/v1` |
| `NEXT_PUBLIC_API_BASE_URL` | same as `API_BASE_URL` (fallback only; the browser uses `/api/proxy`) |
| `NEXT_PUBLIC_DEMO_MODE` | `false` |
| `NEXT_PUBLIC_SENTRY_DSN` | `wodi-crm-fe` DSN |
| `NEXT_PUBLIC_SENTRY_ENVIRONMENT` | `production` |
| `SENTRY_AUTH_TOKEN` | source-map upload (`project:releases`, `org:read`) |
| `SENTRY_ORG` | `wodi` |
| `SENTRY_PROJECT` | `wodi-crm-fe` |

`API_BASE_URL` service names must match the Railway service name (`api` above).

## 6. After the first successful deploy

1. Open the web domain and sign in with the seeded GM (`gm@wodi.local` / `ChangeMe123!`).
2. Change that password immediately, then create a real admin.
3. Point custom DNS (optional) at both public domains and add the new HTTPS origin to `CORS_ORIGINS`.
4. WhatsApp / Gmail webhooks use the **public** API host: `https://<api-domain>/v1/webhooks/<provider>`.

## 7. What this repo already does on Railway

- Bind `$PORT` when `HTTP_ADDR` is empty.
- Treat a missing `APP_ENV` as `production` when `RAILWAY_ENVIRONMENT` is set (so local secrets fail closed).
- Tag Sentry releases from `RAILWAY_GIT_COMMIT_SHA`.
- Trust Railway’s private network for `X-Forwarded-For` when `TRUSTED_PROXIES` is unset.
- Run goose migrations as a pre-deploy command on the API service.
