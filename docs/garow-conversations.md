# Garow conversation records

This internal extension records authenticated Chat Completions and Responses
requests (JSON/SSE), playground chat requests, and each Responses WebSocket turn.
It records client-submitted text before upstream conversion and bytes successfully
written to the client. It does not inject prompts or call another model.

The audit category `conversation` contains only metadata. Gzip-compressed bodies
live in `conversation_logs` in the primary database, including when the audit
database is separate. Only a live root dashboard session may read bodies through
`GET /api/conversation/:request_id`. Ordinary users, non-root administrators and
personal access tokens cannot read them. The UI loads bodies only while the
detail dialog is open and discards its query cache on unmount.

Responses records additionally expose a conversation replay tab. Explicit
`response.id` / `previous_response_id` links are followed only within the same
user. Each selected turn shows its ancestor path; sibling branches remain
separate. Full client-supplied input history is rendered as a standalone snapshot
when there is no link. Text similarity and time proximity never join requests.
The replay shows instructions, user/assistant messages, tool calls/results and
returned reasoning summaries. It cannot recover encrypted/internal reasoning,
attachments, or upstream context that was never recorded. Chat Completions keeps
request details and does not receive this replay UI.

`GET /api/conversation/:request_id/thread` has the same root-session restriction.
It returns at most 50 related turns and an 8 MiB content budget. Missing/expired
or ambiguous parents, partial captures and omitted bodies are explicitly marked.
An upgrade backfills link metadata from existing recorded bodies without
altering the bodies or business accounts.

Configuration:

| Variable | Default | Bounds |
| --- | --- | --- |
| `CONVERSATION_LOG_ENABLED` | false | Set exactly `true` to capture |
| `CONVERSATION_LOG_MODE` | session | `session` (default) or `full` (legacy per-request rollback) |
| `CONVERSATION_LOG_PAYLOAD` | legacy | `legacy` dual-writes old bodies; `off` writes only the session model |
| `CONVERSATION_RETENTION_DAYS` | 30 | 1–90 days; falls back to `CONVERSATION_LOG_RETENTION_DAYS` |
| `CONVERSATION_LOG_RETENTION_DAYS` | 30 | 1–90 days (legacy alias) |
| `CONVERSATION_LOG_MAX_BYTES` | 2097152 | 64 KiB–8 MiB, legacy body capture each direction |
| `CONVERSATION_LOG_PARSE_MAX_BYTES` | 33554432 | 64 KiB–128 MiB, request parse budget (session mode) |
| `CONVERSATION_RESPONSE_SAFETY_BYTES` | 16777216 | 1–64 MiB, response byte safety cap (session mode) |
| `CONVERSATION_TEXT_MAX_BYTES` | 65536 | 1 KiB–1 MiB, stored per-message text cap |
| `CONVERSATION_LOG_MAX_INFLIGHT` | 128 | 8–1024 concurrent capture slots |
| `CONVERSATION_LOG_QUEUE_SIZE` | 256 | 16–4096 asynchronous write queue |

Expired records disappear from queries immediately and are deleted hourly.
Existing authentication audit retention is unchanged. Database backups may keep
expired contents until their own rotation completes. Gzip is compression, not
encryption; database and backup access must remain restricted.

There are 128 concurrent capture slots and a 256-item asynchronous write queue
(both configurable). Overload, process termination or database failures may lose
records without blocking inference; graceful shutdown drains the queue so planned
restarts do not lose captured turns. Server logs contain failure counts and
request IDs, never the captured bodies. This is operational request history, not
a zero-loss audit system or a conversation memory service.

Attachments, binary payloads, authentication fields and encrypted reasoning are
omitted. Free text and tool arguments may still contain business-sensitive text
provided by clients; this is not a general-purpose DLP filter. Truncation and
interruption are shown explicitly. Only the bounded captured portion is retained.
An oversized request is represented by an omission marker.

Native Claude/Gemini, compaction, realtime/audio/image/task endpoints and traffic
forwarded by a separate Nginx upstream are outside this version's scope. An ID
reference to earlier upstream context cannot reconstruct that context. Captures
become visible after a request/turn ends; historical bodies cannot be recovered.

## Session audit model (default)

`CONVERSATION_LOG_MODE=session` converges per-request records into one timeline
per conversation. Three tables in the primary database:

- `conversation_sessions` — metadata skeleton (user, token, model, source,
  counts, `updated_at`). No bodies.
- `conversation_messages` — append-only visible utterances. Each user message
  and each assistant text is stored once; `request_id` provides provenance.
  Tool/skill/command execution details and reasoning are never stored; only a
  per-request `tool_calls` count remains for reading-view dividers.
- `conversation_logs` — the request ledger. In session mode `payload` is NULL
  once `CONVERSATION_LOG_PAYLOAD=off`; metadata, outcome and counts remain.

Session identity is deterministic, never text similarity or time proximity:
explicit client identifiers (`prompt_cache_key`, `session_id`, `conversation_id`,
`metadata.user_id`), then Responses `previous_response_id` links, then a
positional user-message hash prefix chain within the same user+token. A diverging
history (compaction/edit/branch) starts a child session linked to its parent;
old messages are never rewritten. User-role items that look like injected IDE
context are stored but labelled `kind=context` and collapsed in the UI.

Request parsing is separated from storage: the body is parsed up to
`CONVERSATION_LOG_PARSE_MAX_BYTES` but only extracted text is kept, so oversized
requests no longer lose the user message. Responses are parsed incrementally
(SSE/WS line events), keeping memory O(visible text); reasoning deltas never
enter the text budget. A turn whose request side is unrecoverable but whose
assistant text survives is kept in a per-user `source=residual` catch-all session
rather than dropped.

Root-session-only endpoints (PAT denied, `no-store`):
`GET /api/conversations/sessions`, `GET /api/conversations/sessions/:id`
(paginated messages + ledger; opening writes a `conversation.session_view` audit
event), `GET /api/conversations/messages/search` (escaped LIKE),
`GET /api/conversations/requests/:request_id` (ledger metadata). The legacy
`/api/conversation/:request_id[/thread]` body endpoints remain for retained legacy
payloads during the dual-write transition.

On startup a mutex-serialized, idempotent backfill maps retained legacy payload
rows into the session model (state markers `linked`/`unassigned`/`error`); it never
rewrites legacy bodies and resumes safely after interruption. Retention cleanup
cascades sessions → messages and expires the ledger by `created_at`.

## Validation

`go test ./controller -run '^TestConversation' -count=1 -v` uses real SQLite,
MySQL and PostgreSQL when `TEST_MYSQL_DSN`, `TEST_POSTGRES_DSN`,
`TEST_LOG_MYSQL_DSN` and `TEST_LOG_POSTGRES_DSN` point to **dedicated empty test
databases**. The test deletes only its own conversation/audit tables. Never use
production DSNs. It verifies fresh and repeated migrations, existing audit data,
uniqueness, separate log databases, cleanup, authorization, SSE and WebSocket
turn isolation. Tested engines: SQLite 3.50.4, MySQL 5.7.44, PostgreSQL 17.11.
ClickHouse uses its native mutation syntax for cleanup; it was not part of this
release's live database matrix.

Related regressions:

```sh
go test ./model ./middleware ./controller ./service ./relay \
  -run 'TestConversation|TestResponsesWebSocket|TestAudit' -count=1
cd web
bun run typecheck
bun run test src/features/usage-logs/audit/__tests__ src/features/home/__tests__
bun run build
```

The internal homepage composes existing PublicLayout, Button, Dialog, CopyButton,
DetailRow/DetailSection and audit table components. TeamHome provides new business
content and semantic layout; it does not replace generic controls. Shared header
and footer additions are optional, preserving other callers' defaults and the
upstream attribution and licensing.
