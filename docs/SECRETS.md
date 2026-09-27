# Secrets store verification (Epic 17 / T-211)

BYO API keys and channel credentials must live outside the application binary and
outside SQL backups. This checklist verifies the secrets contract.

## Secret inventory

| Secret | Env / config key | Used by | Exposed to clients? |
|--------|------------------|---------|---------------------|
| Postgres URL | `DATABASE_URL` | API, migrate | No |
| JWT signing keys | `JWT_ACCESS_SECRET` + `JWT_ACCESS_KEY_ID`, retired: `JWT_PREVIOUS_ACCESS_SECRETS` | TokenSigner | No (tokens only) |
| Redis / Asynq | queue URL | workers | No |
| MinIO keys | storage access/secret | documents | No |
| Field encryption keys | `ENCRYPTION_KEY` + `ENCRYPTION_KEY_ID`, retired: `ENCRYPTION_PREVIOUS_KEYS` | secrets, passports, MFA | No |
| Blind index key | `ENCRYPTION_BLIND_INDEX_KEY` | passport dedupe/search, verify-token lookup | No |
| AI provider key | `ai_settings.secrets_enc` (per branch) | AI adapters | **Never raw** — UI gets `key_hint` only |
| Channel tokens | `integration_accounts.secrets_enc`; verify token as `verify_token_hash` | WhatsApp/IG/email | Verify token once on connect, then hint only |
| External integration secrets | `external_integrations.secrets_enc` | extint adapters | `secret_hints` only (`••••1234`) |
| File-sync OAuth | `file_sync_connections.secrets_enc` | filesync | `secret_hints` only |
| Passport numbers | `customers` / `booking_participants.passport_enc` (+ `passport_hash`, `passport_last4`) | customer, booking, rooming, import/export | Masked; full value only via audited reveal (`pii.read`) |

## Rules (DoD)

1. **No secrets in git** — `.env`, credential JSON, dumps blocked by `.gitignore`.
2. **Encrypted at rest** (Epic 20 / T-259): config keys containing `secret`, `token`, `password`,
   `api_key`, `private_key` or `credential` are split out of `config_json` and
   sealed into `secrets_enc` with AES-256-GCM. The ciphertext is bound (AAD) to table + row id + branch,
   so a value copied to another row or branch does not decrypt. Rows written before Epic 20 are read
   from `config_json` until the backfill (`POST /v1/ops/encrypt-backfill`) moves them.
3. **API responses** must never echo raw `api_key` / OAuth tokens (mask / omit).
4. **Rotate**: JWT secret and AI keys rotatable without schema change.
5. **Backup**: DB dumps exclude nothing by default — treat dumps as **sensitive**;
   store encrypted; secrets manager remains source of truth for cloud credentials.
6. **Key rotation**: set a new `ENCRYPTION_KEY` + `ENCRYPTION_KEY_ID`, move the old pair into
   `ENCRYPTION_PREVIOUS_KEYS`, deploy, run `POST /v1/ops/encrypt-backfill` (re-seals every value under the
   active key), then drop the old key. Keep `ENCRYPTION_BLIND_INDEX_KEY` fixed (unset, it derives from the
   active key and changes on rotation). After changing it, run the backfill with `{"rehash":true}`;
   passport dedupe/search misses rows until the rehash finishes.

## Verification steps

```bash
# 1) Confirm JWT secret is non-default in non-dev
# 2) Complete AI setup as GM → GET /v1/ai/setup returns key_hint only
# 3) Employee token calling POST /v1/ai/setup → 403
# 4) Grep built binary / logs for plaintext provider keys after a sync → none
# 5) After backfill: no plaintext left
#    SELECT count(*) FROM ai_settings WHERE config_json ? 'api_key';                  -- 0
#    SELECT count(*) FROM customers WHERE passport_no <> '';                          -- 0
#    SELECT count(*) FROM booking_participants WHERE passport_no <> '';               -- 0
```

Sign-off: attach evidence (screenshots / log redaction) to T-217 acceptance pack.
