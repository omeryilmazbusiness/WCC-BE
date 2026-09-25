# Acceptance gate checklist (Epic 17 / T-217)

Cross-cutting Definition of Done against the WODI PDF commercial journey.
Sign each row after evidence is attached (test output, screenshot, or runbook link).

## Backend

| ID | Criterion | Evidence | Sign-off |
|----|-----------|----------|----------|
| T-208 | Journey contract: inbound → lead → booking → payment → docs → target → report | `go test ./internal/domain/hardening/...` | [ ] |
| T-209 | Idempotency keys normalized; task/payment replay safe | `hardening` + `app/task` reactor tests | [ ] |
| T-210 | RBAC + AI leakage matrix | `TestAIPermissionLeakage`, `TestRolePermissionMatrixCoverage` | [ ] |
| T-211 | Backup/restore + secrets procedure documented & staged once | `docs/BACKUP_RESTORE.md`, `docs/SECRETS.md` | [ ] |
| T-212 | List/dashboard indexes + pagination clamps | `00020_epic17_hardening.sql`, `paging_hardening_test.go` | [ ] |

## Frontend

| ID | Criterion | Evidence | Sign-off |
|----|-----------|----------|----------|
| T-213 | RTL/LTR visual QA: Manager, Employee, Inbox, Booking | `e2e/epic17-acceptance.spec.ts` RTL cases | [ ] |
| T-214 | Mobile usability: inbox / booking / tasks | Playwright mobile project + CSS `touch-target` | [ ] |
| T-215 | UI acceptance scenarios from PDF gate | epic17 Playwright suite | [ ] |
| T-216 | Empty / loading / error / permission-denied polish | `LoadingState`, `PermissionDenied` + boards | [ ] |

## Final

| ID | Criterion | Sign-off |
|----|-----------|----------|
| T-217 | All rows above checked; no P0/P1 open on PDF scenarios | [ ] |

**Owner:** _____________ **Date:** _____________
