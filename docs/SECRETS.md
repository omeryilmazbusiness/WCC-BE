# Secrets store verification (Epic 17 / T-211)

BYO API keys and channel credentials must live outside the application binary and
outside SQL backups. This checklist verifies the secrets contract.

## Secret inventory

| Secret | Env / config key | Used by | Exposed to clients? |
|--------|------------------|---------|---------------------|
| Postgres URL | `DATABASE_URL` | API, migrate | No |
| JWT signing key | `AUTH_JWT_SECRET` / config | TokenService | No (tokens only) |
| Redis / Asynq | queue URL | workers | No |
| MinIO keys | storage access/secret | documents | No |
| AI provider key | `ai_settings.config_json` (per branch) | AI adapters | **Never raw** — UI gets `key_hint` only |
| Channel tokens | integration account config | WhatsApp/IG/email | No |
| File-sync OAuth (future) | `file_sync_connections.config_json` | filesync stubs | No |

## Rules (DoD)

1. **No secrets in git** — `.env`, credential JSON, dumps blocked by `.gitignore`.
2. **AI keys** stored per-branch encrypted-at-rest when a secret manager is wired;
   until then Postgres JSON is acceptable only in locked-down VPC + restricted RBAC (`ai.setup`).
3. **API responses** must never echo raw `api_key` / OAuth tokens (mask / omit).
4. **Rotate**: JWT secret and AI keys rotatable without schema change.
5. **Backup**: DB dumps exclude nothing by default — treat dumps as **sensitive**;
   store encrypted; secrets manager remains source of truth for cloud credentials.

## Verification steps

```bash
# 1) Confirm JWT secret is non-default in non-dev
# 2) Complete AI setup as GM → GET /v1/ai/setup returns key_hint only
# 3) Employee token calling POST /v1/ai/setup → 403
# 4) Grep built binary / logs for plaintext provider keys after a sync → none
```

Sign-off: attach evidence (screenshots / log redaction) to T-217 acceptance pack.
