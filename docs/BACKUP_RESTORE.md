# Backup & restore procedure (Epic 17 / T-211)

Production Definition of Done requires a verified backup/restore path for PostgreSQL
and object storage (MinIO/S3). Application secrets are **not** stored in the DB dump.

## What to back up

| Asset | Location | Tool |
|-------|----------|------|
| Primary DB | Postgres (`DATABASE_URL`) | `pg_dump` / managed snapshot |
| Object storage | MinIO bucket for documents | `mc mirror` or S3 versioning |
| Migrations state | `goose_db_version` table | included in DB dump |
| Secrets | Env / secret manager (see `docs/SECRETS.md`) | **never** in SQL dumps |

## Backup (logical)

```bash
# Timestamped custom-format dump (recommended)
pg_dump "$DATABASE_URL" -Fc -f "wodi-$(date -u +%Y%m%dT%H%M%SZ).dump"

# Optional: plain SQL for small environments
pg_dump "$DATABASE_URL" --no-owner --no-acl -f "wodi-plain.sql"
```

Object storage:

```bash
mc mirror myminio/wodi-docs "./backup/docs-$(date -u +%Y%m%d)"
```

Retention: keep at least **7 daily** + **4 weekly** snapshots (adjust to SLA).

## Restore (staging verification)

1. Provision empty Postgres + run migrations: `go run ./cmd/migrate up`
2. Restore dump:
   ```bash
   pg_restore --clean --if-exists -d "$DATABASE_URL" wodi-YYYYMMDD.dump
   ```
3. Re-point MinIO / restore objects.
4. Smoke: `GET /healthz`, `GET /readyz`, login as GM, open `/manager`, `/inbox`, one booking.
5. Record restore RTO/RPO in the ops runbook.

## Verification checklist

- [ ] Dump completes without error; size > 0
- [ ] Restore on **non-prod** succeeds
- [ ] `goose status` matches expected latest migration
- [ ] AI / channel / payment **secrets** still resolve from secret store (not from dump)
- [ ] Sample customer PII readable post-restore (proves data integrity)

## Notes

- Prefer PITR (WAL archiving) in managed Postgres for production RPO < 15m.
- Never commit dumps or `.env` files to git.
