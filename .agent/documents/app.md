# Application Overview

This document describes the **current implementation** of `acp2larkbot`. It is based on the repository code, not on aspirational behavior.

## Purpose

`acp2larkbot` is a single-process bridge that receives Lark messages over a websocket connection, applies routing and authorization, forwards user text to an ACP backend, and streams the backend response back to Lark.

## Entry Point

- The executable entry point is `cmd/acp2larkbot/main.go`.
- Supported flags:
  - `-config`: path to a YAML config file.
  - `-validate-only`: load and validate the config, then exit.

## Process Lifecycle

At startup the program:

1. Loads YAML configuration.
2. Applies defaults and strict validation.
3. Configures the global slog log level.
4. Builds the application object.
5. Starts reading Lark websocket events.
6. Stops on `SIGINT` or `SIGTERM`.

## Runtime Wiring

The app currently wires together these components:

- `state`: bbolt-backed local state store.
- `lark`: live websocket gateway and self-message filter.
- `router`: chat lookup, mention gate, command parsing.
- `authz`: user and command authorization.
- `session`: session resolution and in-flight runtime tracking.
- `worker`: one serial worker per chat.
- `acp`: backend client registry.

## Event Handling Flow

For each inbound Lark event, the application processes it in this order:

1. Self-message filter.
2. Dedupe check.
3. Chat allowlist lookup.
4. Trigger filtering and optional mention requirement.
5. Command parsing.
6. Authorization.
7. Either:
   - execute a built-in command, or
   - enqueue a worker job for ACP processing.

## Unknown Chat Behavior

Unknown chats are not processed as normal conversations.

- `unknown_chat.behavior: ignore` silently drops them.
- `unknown_chat.behavior: reply_error` sends a rate-limited error reply.

The rate limit is stored in bbolt, so it survives process restarts as long as the same state file is reused.

## Current Scope

The current implementation is intentionally narrow:

- single binary
- single process
- single local state file
- Lark websocket input only
- text message handling only
- built-in commands: `/cancel`, `/status`, `/reset`

## Important Current Limitations

The current code does **not** implement several behaviors that might appear in earlier design notes:

- no periodic session cleanup loop
- no persistent session reset in `/reset`
- no audit log pipeline beyond normal slog output
- no ACP child-process restart manager
- no ACP websocket reconnect/session recovery/auth refresh logic
- no distributed or multi-instance coordination

## Single-Instance Assumption

The current architecture assumes a single running instance per bot configuration because the following data is local-process or local-file scoped:

- worker queues
- self-message ignore set
- dedupe store
- auto-created session mappings

Running multiple replicas against the same Lark app would break ordering and deduplication guarantees.
