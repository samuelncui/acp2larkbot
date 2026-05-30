# Router, Authorization, and Commands

This document describes how routing and authorization work in the current codebase.

## Chat Routing

The router builds an in-memory map from configured `chats[]` entries.

Each incoming event is matched by `chat_id`.

- if the chat exists, the event continues
- if the chat does not exist, the app applies the `unknown_chat` policy

This makes `chats[]` the effective chat allowlist.

## Trigger Rules

The router currently only handles text messages.

If a chat is a group chat and mention requirement is enabled, the router only accepts the event when one of the event mentions equals `lark.app_id`.

Mention requirement is resolved from:

1. `chats[].require_mention`, if set
2. otherwise `lark.trigger.require_mention_in_group`

## Command Parsing

The router recognizes local commands only when all of the following are true:

- commands are enabled
- the message starts with `commands.prefix`
- the parsed command name exists in `commands.rules`

This means unrecognized slash commands are intentionally passed through to ACP as normal text input.

## Authorization Model

Authorization is currently simple and deterministic.

The evaluator checks:

1. sender ID must exist
2. global user role from `authz.access.users` or `authz.admins`
3. if no explicit user role exists and `authz.access.default == deny`, reject
4. if the current chat has a `authz.access.chats[]` restriction, sender must appear there
5. if the message is a built-in command, the sender role must appear in `commands.rules.<name>.roles`

The current decision object records:

- allowed / denied
- denial reason
- chat
- sender ID
- role
- command name

## Current Role Resolution

Current role resolution order:

1. search `authz.access.users`
2. search `authz.admins`
3. otherwise no explicit role

If the default access is not deny, the code falls back to role `user`.

## Chat-Level Access Rules

If a chat is listed under `authz.access.chats`, that list becomes an allowlist for that specific chat.

If a chat is not listed there, all users who passed the global rule are allowed.

## Built-in Commands

The current command set is:

- `/cancel`
- `/status`
- `/reset`

### `/cancel`

- cancels the currently running request for the chat
- allowed for the request owner
- also allowed for admins
- fails if no request is running

### `/status`

- reports whether the chat is idle or running
- if running, shows request ID and a shortened session ID prefix

### `/reset`

- admin only
- requires the exact confirmation text `RESET`
- currently does **not** delete persistent session mappings
- currently only refuses reset while a request is actively running

## Important Current Notes

Some configuration fields suggest more advanced command behavior than the current code implements.

Examples:

- `only_request_owner_or_admin` is not read by the command handler; owner/admin enforcement is hardcoded in runtime cancellation
- `status.redact` is not used
- `reset.confirm_text` is not used; the string `RESET` is hardcoded
- `commands.audit` does not currently drive a separate audit system
