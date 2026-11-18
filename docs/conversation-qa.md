# Garow question and final reply audit

Set `CONVERSATION_LOG_ENABLED=true` to record verified exchanges. All conversation reading routes require a root browser session and return only the retained question/answer projection. On this deployment retention is 30 days from each user submission. An unanswered question has an empty final; a request with no verified question does not create a turn.

A chat is scoped to the user, API key and client source. The playground persists a random chat ID with its messages and attaches a submission ID to an explicit send. Its two audit headers are removed before relay. Responses clients can supply an explicit conversation ID. Codex's `thread-id` identifies a thread; its hyphenated `session-id` can be for cache affinity and does not establish chat identity. See [Codex transport](https://github.com/openai/codex/blob/main/codex-rs/core/src/client.rs) and [header construction](https://github.com/openai/codex/blob/main/codex-rs/codex-api/src/requests/headers.rs). Model names, request IDs, timestamps, cache keys and similar text are not chat IDs.

Unattributed turns are separately readable and searchable. A verified unique parent response can connect chats; a returned tool ID can only attach to the turn that issued it. The recorder accepts visible terminal Responses output or a single Chat Completions choice with a normal stop. Broken streams, commentary, reasoning and pending calls are not final replies. Unmarked plain text cannot prove who typed it.

Only the QA turn, receipt and edge tables are used. Receipts deduplicate requests and edges connect continuations, even when one user submission takes many calls. Capture never writes a raw payload, a replay ledger row, or a conversation event to the general audit log. Only the conversation audit page exposes the retained exchanges (root browser session, no-store). Login and operational auditing are independent.

There is no startup migration that discards an installation's existing history. For a Garow .11 upgrade, restore-check a restricted backup, verify all retained legacy requests already have QA receipts, deploy the QA-only writer, then retire the old tables in a separate, verified maintenance transaction. Roll back the application image if needed; never overwrite current business or QA data with a snapshot.

Regression matrix: `go test ./service ./controller -run 'TestQA|TestConversationQA'`. Point `TEST_MYSQL_DSN` and `TEST_POSTGRES_DSN` only at empty disposable test databases for the full fresh/upgrade matrix.
