# Configuration Design

This document describes how configuration works in the current implementation.

## Loading Model

Configuration is loaded from YAML with `configor`.

Current behavior:

- unknown YAML keys are rejected
- defaults are applied after load
- final validation is strict and required for startup

## Environment Variables

The config loader uses the prefix `ACP2LARKBOT`.

The code explicitly supports env binding for these Lark fields:

- `ACP2LARKBOT_LARK_APP_ID`
- `ACP2LARKBOT_LARK_APP_SECRET`
- `ACP2LARKBOT_LARK_DOMAIN`

## Defaults

The loader currently applies these defaults when fields are omitted:

- `log.level: info`
- `lark.domain: feishu`
- `lark.connection_mode: websocket`
- `lark.ignore.self_app_id: <lark.app_id>`
- `unknown_chat.behavior: reply_error`
- `unknown_chat.message: This chat is not enabled for acp2larkbot.`
- `state.type: bolt`
- `commands.prefix: /`
- `chats[].queue.max_pending: 5`
- `chats[].queue.on_full: reject`
- `chats[].agent: <acp.default_agent>`
- `chats[].session.scope: sender`
- `acp.agents[*].protocol.max_inflight: 1`

## Required Top-Level Sections

The current validator requires these top-level sections to be present and valid in practice:

- `lark`
- `unknown_chat`
- `dedupe`
- `state`
- `acp`
- `chats`

`commands` is only enforced when `commands.enabled` is true.

## Lark Validation

Current validation rules include:

- `lark.app_id` is required
- `lark.app_secret` is required
- `lark.connection_mode` must be `websocket`
- `lark.ignore_self_messages` must be `true`
- `lark.ignore.message_id_ttl` must be positive
- `lark.ignore.max_message_ids` must be positive
- `lark.trigger.message_types` currently only supports `text`
- `lark.trigger.ignore_update_events` must be `true`
- `lark.trigger.ignore_card_events` must be `true`

## Commands Validation

When commands are enabled, the config must provide rules for:

- `cancel`
- `status`
- `reset`

Also, `commands.rules.reset.require_confirm` must be `true`.

## ACP Agent Validation

`acp.default_agent` must point to a key in `acp.agents`.

Two agent types are supported:

- `cmd`
- `network`

### cmd agent

Current required and enforced behavior:

- `command` is required
- `cwd` must be absolute
- shell execution is rejected in phase 1
  - `shell: true` is rejected
  - `command: sh` is rejected
  - any `-lc` argument is rejected

`acp2larkbot` does not own sandbox policy. It only starts the local ACP server
process and forwards ACP messages. Tool execution, filesystem/network access
control, and sandbox policy must be implemented by the ACP server / agent.

The old `acp.agents.<id>.sandbox.*` config shape is no longer accepted by
`acp2larkbot`.

### network agent

The current implementation supports ACP over websocket only.

Required validation:

- `protocol.transport: websocket`
- `protocol.binding: acp.websocket`
- `protocol.max_inflight: 1`
- `url` is required
- `url` must use `wss://`
- `url` must appear in `endpoint_allowlist`
- `tls.insecure_skip_verify` must be `false`
- `request.require_request_id` must be `true`
- heartbeat and idempotency durations must be positive

## Chat Validation

Each chat must satisfy:

- unique `chat.id`
- `chat.id` must not look like a user ID such as `ou_xxx`
- referenced `agent` must exist
- `queue.max_pending` must be within range
- `queue.on_full` must be `reject` or `drop_oldest`

## Session Validation

Supported session strategies:

- `static`
- `auto_create`
- `ephemeral`

Supported session scopes:

- `chat`
- `sender`

Unsupported in current code:

- `thread`

Additional rules:

- group chat with `scope: chat` requires `allow_shared_session: true`
- `ttl` and `idle_timeout` must be positive
- `idle_timeout` must not exceed `ttl`
- `static` requires `session.id`
- `auto_create` requires `session.prefix`

## Authz Validation

Current validation only enforces:

- `authz.access.default` must be `deny` or `allow`

## Fields Present But Not Fully Used at Runtime

The schema includes several fields that are not fully implemented in the current code path. Examples include:

- `lark.require_mention`
- `lark.ignore.sender_types` as a configurable list
- `commands.audit`
- `log.audit.*`
- `commands.rules.status.redact`
- `commands.rules.reset.confirm_text`
- `auth.refresh_command`
- `reconnect.*`
- `session_recovery.*`

These fields may still be validated or stored, but they should not be documented as active runtime behavior unless the code path clearly uses them.
