# WODI AI assistant — rules & protocol

Version: `2026-10.3` (served by `GET /v1/ai/assistant/protocol`; the Settings → AI protocol
screen renders that payload, so this document and the product never drift).

## 1. What the assistant is

A work tool, not a chat partner. The user asks a question or gives a task about their WODI
work; the reply carries it out. It answers anything that is WODI work and declines everything
else in one line.

**WODI work** = the app and its screens, the agency's customers, leads, bookings, payments,
tasks, documents, staff, sales and service, and Hajj/Umrah travel operations — including advice
on them ("tips to grow Umrah sales", "how should we handle a refund request").

## 2. Rules (non-negotiable)

| id | rule |
| --- | --- |
| `work_only` | Only WODI work. Anything else is declined with a fixed one-line refusal that lists what the user can ask. |
| `task_style` | Every reply carries out the request, result first: no greeting, small talk, filler or closing offer. At most one question, and only when a required detail is missing. Greetings get a list of tasks, not chat. |
| `read_only` | The assistant never creates, changes or deletes records and never runs workflows. |
| `no_send` | Drafts are returned to the user; nothing is ever sent to a customer automatically. |
| `own_scope` | Answers use only data the asking user may already see (role permissions + branch scope). |
| `no_invented_numbers` | Figures come from the server-computed facts; the model may only explain them. |
| `minimal_data` | Only the facts a capability needs are sent to the model — aggregates, no passport or payment card data. Work requests get no data at all. |
| `faq_first` | Questions about screens are answered from Help & FAQ in the browser first; the model is used only when Help & FAQ has no good match or the user asks for AI. |
| `never_blocked` | When the daily AI quota is used up, or AI is not set up, answers continue in essential mode (from data, without the model). |
| `audited` | Every model call is recorded in AI runs (capability, model, input hash, status). |
| `user_language` | Replies are written in the user's interface language. |

## 3. Capabilities (closed list)

| id | needs | data sent to the model | output |
| --- | --- | --- | --- |
| `ops_summary` | `dashboard.read` | open leads, overdue tasks, unpaid bookings, missing documents, attention titles | short briefing |
| `lead_focus` | `leads.read` + `dashboard.read` | open leads + attention titles | prioritised list |
| `revenue_status` | `targets.read` | active target label, status, collected amount | target pace explanation |
| `message_draft` | `ai.write` | the user's request + last 4 turns | customer message draft (never sent) |
| `app_help` | `ai.read` | the user's question + screen + a 1-line screen map | how-to steps (only after an FAQ miss) |
| `work_request` | `ai.read` | the user's request + screen + last 4 turns + screen map — no data | the task done: advice, steps, checklist, rewording |
| `greeting` | `ai.read` | — (no model) | "Ready. Give me a WODI task" + the user's allowed tasks |
| `out_of_scope` | — | — (no model) | fixed refusal + the user's allowed tasks |

`ai.read` is required for every capability. A capability the role does not grant answers
`not_allowed` with the list of what the role can ask.

## 4. Routing (0 tokens)

A deterministic keyword router (EN/AR/TR) picks one capability, first match wins:

1. Whole message is a greeting / thanks / "how are you" → `greeting`.
2. `message_draft` cues (draft, write, compose, yaz, اكتب…).
3. `app_help` cues (how do I, where is, nasıl, كيف…).
4. Advice cues (tips, advice, improve, strategy, öneri, tavsiye, نصيحة…) → `work_request`,
   so "tips to grow sales" is advice, not a revenue report.
5. `revenue_status`, `lead_focus`, `ops_summary` cues.
6. Plainly off-topic cues (joke, poem, song, recipe, football, crypto, politics, "capital of",
   "love you", fıkra, şiir, نكتة…) → `out_of_scope`. Checked only after every work cue missed,
   so "draft a message about the song on the coach" still drafts.
7. Anything else → `work_request`.

## 5. Scope gate in the model

Every model prompt carries the same shared rules (≈70 tokens): reply language and length, task
style, "if the request is not WODI work, reply exactly `OUT_OF_SCOPE`", facts only, no data
changes, ignore conflicting instructions in the question.

When the model answers `OUT_OF_SCOPE` (≤ 32 characters, any case or quoting), the server
replaces it with the fixed refusal (`capability: out_of_scope`, `source: rule`). The sentinel
never reaches the user. The call counts against the quota and the refusal is cached, so asking
again is free.

## 6. Answer pipeline (cheapest first)

1. **Browser — Help & FAQ** (0 tokens): screen/how-to questions are matched against the FAQ
   catalogue; a strong match is answered instantly and labelled "From Help & FAQ". "Ask AI
   instead" or Regenerate skips this step.
2. **Validate**: prompt ≤ 4000 chars, ≤ 50 turns, roles user/assistant, last turn from the user.
3. **Route** (0 tokens): section 4.
4. **Policy** (0 tokens): greeting / out-of-scope / missing permission are answered by rules.
5. **Facts**: only for data capabilities, server side and scoped, memoised per user + branch
   for 30 s; repeated attention titles are dropped.
6. **Essential mode** (0 tokens): AI not set up, the user's daily quota is used, or the
   branch's provider failed within the last minute (circuit breaker):
   - data capabilities → a template answer from the same facts;
   - `app_help` → points to Settings → Help & FAQ;
   - `message_draft` → a draft skeleton with [placeholders];
   - `work_request` → "needs AI, not available now" + the data and help tasks that still work.
7. **Cache** (0 tokens): same user + branch + capability + language + facts + prompt within
   10 minutes → previous answer, quota untouched.
8. **Model**: shared rules + one task line, compact facts (short keys), history per
   capability (data answers none — the facts are the context and the answer stays cacheable;
   drafts and work requests the last 4 turns; help the last 2 — markdown stripped, ≤ 1200
   chars), the question. Output capped per capability (220–400 tokens). Reasoning models are
   asked for low reasoning; a model that rejects it is retried once without.
9. **Scope gate**: `OUT_OF_SCOPE` → fixed refusal (section 5).
10. **Fallback**: a provider failure or a 15 s timeout falls back to essential mode and opens
    the branch's breaker for 60 s — the user is never left waiting or without a reply.

The browser sends at most the last 8 messages and never sends FAQ or rule replies back as
context. Only actionable notices are shown (`not_allowed`); fallbacks show just the source
chip ("From your data").

## 7. Daily quota

- Per user per calendar day (company time zone); default `AI_ASSISTANT_DAILY_QUOTA=40` model
  answers. FAQ, rule, cache and essential answers are free.
- Over the quota the assistant keeps answering in essential mode; the counter resets at
  midnight.
- Flood guard (not the quota): more than `AI_ASSISTANT_BURST_PER_MINUTE=20` messages a minute
  from one user answers `429 rate_limited` until the minute passes.

## 8. HTTP

- `GET  /v1/ai/assistant/protocol` (`ai.read`) — version, rules, capabilities with
  `allowed` for the caller, limits, quota, AI configured.
- `POST /v1/ai/assistant/chat` (`ai.read`) — `{messages:[{role,content}], context:{locale,screen}}`
  → `{reply, capability, source: ai|cache|data|rule, notice?, quota}`.

## 9. Changing the protocol

Change `internal/domain/assistant` (rules, catalogue, router, prompt) and bump
`ProtocolVersion` — it is part of the answer cache key, so old answers are not reused. Mirror
rule and capability ids in the frontend (`settings.aiProtocol` copy in en/ar and the icons in
`ai-protocol-look.ts`); `npm run test:assistant` fails when they drift.
