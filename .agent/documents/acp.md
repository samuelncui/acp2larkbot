# ACP Backend Design

This document describes the ACP-facing side of the current implementation.

## Registry

The ACP registry creates one client instance per configured agent.

Supported agent types today:

- `cmd`
- `network`

The worker selects the client by `chat.agent`.

## Common Client Interface

The shared client interface currently supports:

- `OpenSession`
- `Send`
- `CloseSession`

Worker execution model:

1. resolve a session
2. start rendering in Lark
3. send the user message to ACP
4. process a stream of ACP events
5. map ACP `delta`, `finish`, and `error` to renderer operations

## `cmd` Backend

The `cmd` client starts a local process and speaks JSON-RPC over stdio.

Current handshake and request surface:

- `initialize`
- `session/new`
- `session/prompt`
- `session/update` notifications

### Current behavior

- the process is started lazily on first use
- stdout is parsed line-by-line as JSON-RPC
- stderr is inherited by the parent process and goes to stderr
- `session/update` notifications with `agent_message_chunk` are translated into delta events
- completion of `session/prompt` produces a finish event

### Current limitations

- no child restart loop
- no explicit start timeout enforcement
- no explicit request timeout enforcement
- no graceful child shutdown integration from `app.Close`
- no implemented `CloseSession` RPC; the method currently returns nil

## ACP WebSocket Backend

The websocket backend speaks JSON-RPC over a `wss://` connection.

### Initialization contract

After connecting, the client sends `initialize` and expects a result that confirms:

- binding `acp.websocket`
- request ID echo support
- required methods:
  - `session.open`
  - `message.send`
  - `session.close`
- required events:
  - `delta`
  - `finish`
  - `error`

### Request dispatch

The RPC layer dispatches both responses and streaming events by `request_id`.

This is covered by unit tests.

### Current behavior

- optional bearer token is sent in `Authorization`
- optional websocket ping loop runs from `heartbeat.interval`
- stream events are forwarded until `finish` or `error`

### Current limitations

- no reconnect loop
- no token refresh command execution
- no session recovery
- no application-level heartbeat timeout handling
- CA file read failure falls back silently to default trust roots

## Event Mapping

The worker expects ACP events in this internal form:

- `delta`
- `finish`
- `error`

Renderer behavior is directly driven by these event types.

## Concurrency Model

For websocket ACP, the validator enforces `max_inflight: 1` unless multiplex is explicitly negotiated during initialize.

For cmd ACP, the client also effectively serializes prompt execution per session because it rejects a second in-flight prompt for the same session.
