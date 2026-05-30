# Session, Worker, and State Design

This document describes how request execution state is managed in the current implementation.

## Per-Chat Worker Model

The worker manager creates one worker per configured chat on demand.

Each worker owns a buffered queue with capacity `chat.queue.max_pending`.

Queue overflow behavior:

- `reject`: reject the new job
- `drop_oldest`: discard one queued job, then accept the new one

Each worker processes jobs serially in a single goroutine.

This means messages for the same chat are handled one at a time.

## Runtime Request Tracking

The `session.Runtime` object tracks in-flight requests in memory.

It maintains two maps:

- by chat ID
- by request ID

Stored runtime information includes:

- request ID
- chat ID
- sender ID
- session ID
- start time
- cancel function

This runtime state is used by:

- `/cancel`
- `/status`
- worker cleanup on request completion

## Session Key Design

Persistent session lookup is keyed by a hash of:

- agent
- cwd
- chat ID
- session scope
- sender ID
- thread ID
- static session ID
- session prefix

This is the current isolation boundary used for `auto_create` sessions.

## Session Strategies

### `static`

The configured `session.id` is returned directly.

No persistent state lookup is required.

### `auto_create`

The manager looks up a session record by key.

- if the record exists and has not expired, it is reused
- `updated_at` and `idle_until` are refreshed on reuse
- if missing or expired, a new session is opened and stored

Persisted record fields include:

- session key
- chat ID
- sender ID
- thread ID
- agent ID
- cwd
- session ID
- created / updated / expires / idle timestamps

### `ephemeral`

The manager opens a new session for each request.

- it is not stored in bbolt
- the worker closes it after request completion

## State Store Responsibilities

The bbolt store currently persists three kinds of data:

- session records
- dedupe keys
- unknown-chat reply timestamps

## Cleanup Semantics

The store includes a `CleanupSessions` method that can delete expired or idle session records.

However, the current application does **not** start a background cleanup loop.

So session cleanup support exists at the store level, but it is not automatically scheduled today.

## Reset Semantics

The state store also includes `DeleteSession`, but the current `/reset` command does not call it.

In the current implementation, `/reset` is conservative and effectively acts as a runtime safety check instead of a persistent session deletion command.

## Single-Process Boundary

Current worker and runtime behavior is process-local.

As implemented today, these guarantees do not extend across multiple processes:

- per-chat serial execution
- in-flight cancellation
- dedupe
- self-message loop prevention
- auto-created session reuse ordering
