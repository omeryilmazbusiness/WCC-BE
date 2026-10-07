# WODI AI assistant — rules & protocol

Version: `2026-10.2` (served by `GET /v1/ai/assistant/protocol`; the Settings → AI protocol
screen renders that payload, so this document and the product never drift).

## 1. Rules (non-negotiable)

| id | rule |
| --- | --- |
| `read_only` | The assistant never creates, changes or deletes records and never runs workflows. |
| `no_send` | Drafts are returned to the user; nothing is ever sent to a customer automatically. |
| `own_scope` | Answers use only data the asking user may already see (role permissions + branch scope). |
| `no_invented_numbers` | Figures come from the server-computed facts; the model may only explain them. |
| `minimal_data` | Only the facts a capability needs are sent to the model — aggregates, no passport or payment card data. |
| `faq_first` | Questions about screens are answered from Help & FAQ in the browser first; the model is used only when Help & FAQ has no good match or the user asks for AI. |
| `fixed_capabilities` | Only the capabilities below exist; anything else is declined politely without calling the model. |
| `never_blocked` | When the daily AI quota is used up, or AI is not set up, answers continue in essential mode (from data, without the model). |
| `audited` | Every model call is recorded in AI runs (capability, model, input hash, status). |
| `user_language` | Replies are written in the user's interface language. |

## 2. Capabilities (closed list)

| id | needs | data sent to the model | output |
| --- | --- | --- | --- |
| `ops_summary` | `dashboard.read` | open leads, overdue tasks, unpaid bookings, missing documents, attention titles | short briefing |
| `lead_focus` | `leads.read` + `dashboard.read` | open leads + attention titles | prioritised list |
| `revenue_status` | `targets.read` | active target label, status, collected amount | target pace explanation |
| `message_draft` | `ai.write` | the user's request only | customer message draft (never sent) |
| `app_help` | `ai.read` | the user's question + a 1-line screen map | how-to answer (only after an FAQ miss) |
| `greeting` | `ai.read` | — (no model) | fixed reply |
| `out_of_scope` | — | — (no model) | polite refusal listing what is possible |

## 3. Answer pipeline (cheapest first)

1. **Browser — Help & FAQ** (0 tokens): screen/how-to questions are matched against the FAQ
   catalogue; a strong match is answered instantly and labelled "From Help & FAQ". "Ask AI
   instead" or Regenerate skips this step.
2. **Validate**: prompt ≤ 4000 chars, role holds `ai.read`.
3. **Route** (0 tokens): deterministic keyword router (EN/AR/TR) picks one capability.
4. **Policy** (0 tokens): greeting / out-of-scope / missing permission are answered by rules.
5. **Facts**: only the facts of the chosen capability are loaded (server side, scoped),
   memoised per user + branch for 30 s so a burst of questions reads the dashboard once;
   repeated attention titles are dropped.
6. **Essential mode** (0 tokens): AI not set up, the user's daily quota is used, or the
   branch's provider failed within the last minute (circuit breaker) → a template answer
   from the same facts, instantly.
7. **Cache** (0 tokens): same branch + capability + language + facts + question within
   10 minutes → previous answer, quota untouched.
8. **Model**: tiny per-capability system prompt (~50 tokens), compact facts (short keys),
   question; output capped per capability (220–400). History per capability: data answers
   none (the facts are the context, and the answer stays cacheable across conversations),
   drafts the last 4 turns, help the last 2 — markdown stripped, ≤ 1200 chars. Reasoning
   models are asked for low reasoning (`reasoning_effort: low`, Gemini thinking budget 0 /
   level low); a model that rejects it is retried once without, so it never costs the answer.
9. **Fallback**: a provider failure or a 15 s timeout falls back to the essential answer and
   opens the branch's breaker for 60 s — the user is never left waiting or without a reply.

The browser sends at most the last 8 messages and never sends FAQ or rule replies back as
context.

## 4. Daily quota

- Per user per calendar day (company time zone); default `AI_ASSISTANT_DAILY_QUOTA=40` model
  answers. FAQ, rule, cache and essential answers are free.
- Over the quota the assistant keeps answering in essential mode and says so; the counter
  resets at midnight.
- Flood guard (not the quota): more than `AI_ASSISTANT_BURST_PER_MINUTE=20` messages a minute
  from one user answers `429 rate_limited` until the minute passes.

## 5. HTTP

- `GET  /v1/ai/assistant/protocol` (`ai.read`) — version, rules, capabilities with
  `allowed` for the caller, limits, quota, AI configured.
- `POST /v1/ai/assistant/chat` (`ai.read`) — `{messages:[{role,content}], context:{locale,screen}}`
  → `{reply, capability, source: ai|cache|data|rule, notice?, quota}`.
