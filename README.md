# acp2larkbot

`acp2larkbot` is a Go service that connects a Lark bot to an ACP backend.

It receives text messages from Lark over websocket, checks chat and user access rules, forwards the message to an ACP agent, and streams the agent response back to Lark.

## What the current implementation supports

- Lark websocket event intake
- text message handling
- chat allowlisting through `chats[]`
- user and command authorization
- built-in commands:
  - `/cancel`
  - `/status`
  - `/reset RESET`
- per-chat serial worker queues
- session strategies:
  - `static`
  - `auto_create`
  - `ephemeral`
- local state with bbolt
- ACP backends:
  - local `cmd`
  - remote `network.websocket`
- response streaming back to Lark as:
  - updated text messages, or
  - CardKit streaming cards

## What this README covers

This README is focused on **how to use the project today**.

Detailed design notes are split by module under `.agent/documents/`:

- `.agent/documents/app.md`
- `.agent/documents/config.md`
- `.agent/documents/lark.md`
- `.agent/documents/router.md`
- `.agent/documents/session-worker-state.md`
- `.agent/documents/acp.md`

## Requirements

- Go 1.22+
- a Lark app with websocket event access
- a valid Lark `app_id` and `app_secret`
- an ACP backend, either:
  - a local executable speaking ACP over stdio, or
  - a remote ACP websocket endpoint speaking `acp.websocket`

## Build

```bash
go build ./cmd/acp2larkbot
```

## Configuration workflow

1. Create a YAML config file.
2. Fill in Lark credentials.
3. Define at least one ACP agent.
4. Define at least one allowed chat in `chats[]`.
5. Validate the config.
6. Start the service.

## Minimal local `cmd` example

This example uses a local ACP server executable.

Replace placeholder values such as chat IDs, user IDs, paths, and command names with real values from your environment.

```yaml
log:
  level: info

lark:
  app_id: ${ACP2LARKBOT_LARK_APP_ID}
  app_secret: ${ACP2LARKBOT_LARK_APP_SECRET}
  connection_mode: websocket
  ignore_self_messages: true
  ignore:
    sender_types: [bot]
    self_app_id: ${ACP2LARKBOT_LARK_APP_ID}
    message_id_ttl: 24h
    max_message_ids: 10000
  trigger:
    message_types: [text]
    ignore_update_events: true
    ignore_card_events: true
    require_mention_in_group: false
  streaming:
    enabled: true
    mode: text
    update_interval: 1s
    min_update_chars: 16
    max_update_chars: 8000
    max_updates_per_message: 60
    max_final_chars: 20000
    fallback: append_messages
    fallback_max_messages: 3
    truncate_notice: "\n\n[truncated]"
    rate_limit:
      per_chat: 30/min
      global: 300/min

authz:
  admins:
    - ou_adminxxxxxxxx
  access:
    default: deny
    users:
      - id: ou_adminxxxxxxxx
        role: admin
      - id: ou_userxxxxxxxx
        role: user
    chats:
      - id: oc_chatxxxxxxxx
        users:
          - ou_adminxxxxxxxx
          - ou_userxxxxxxxx

unknown_chat:
  behavior: reply_error
  message: This chat is not enabled for acp2larkbot.
  include_chat_id: false
  rate_limit_interval: 10m

dedupe:
  enabled: true
  ttl: 24h

state:
  type: bolt
  path: ./state/acp2larkbot.db

commands:
  enabled: true
  prefix: /
  rules:
    cancel:
      roles: [user, admin]
      only_request_owner_or_admin: true
    status:
      roles: [user, admin]
      redact: true
    reset:
      roles: [admin]
      require_confirm: true
      confirm_text: RESET

acp:
  default_agent: local
  agents:
    local:
      type: cmd
      command: /absolute/path/to/your-acp-server
      args: []
      cwd: /absolute/path/to/workspace
      timeouts:
        start: 30s
        request: 10m
        idle: 30m

chats:
  - id: oc_chatxxxxxxxx
    type: group
    name: main-group
    agent: local
    cwd: /absolute/path/to/workspace
    queue:
      max_pending: 5
      on_full: reject
    session:
      strategy: auto_create
      scope: sender
      prefix: lark-auto
      allow_shared_session: false
      ttl: 168h
      idle_timeout: 24h
```

`acp2larkbot` only launches the local ACP server process and forwards ACP
messages. Tool execution, filesystem/network access control, and sandbox policy
belong to the ACP server / agent implementation.

Local `cmd` agents inherit the `acp2larkbot` process environment by default.
Do not run untrusted ACP server executables with bot credentials in the
environment.

## Remote ACP websocket example

Use this shape when your ACP backend is remote instead of local:

```yaml
acp:
  default_agent: remote
  agents:
    remote:
      type: network
      protocol:
        transport: websocket
        binding: acp.websocket
        max_inflight: 1
      url: wss://example.com/acp
      endpoint_allowlist:
        - wss://example.com/acp
      auth:
        type: bearer
        token: ${ACP_TOKEN}
      tls:
        insecure_skip_verify: false
        server_name: example.com
        ca_file: /absolute/path/to/ca.pem
      heartbeat:
        type: websocket_ping_pong
        interval: 30s
        timeout: 10s
      request:
        require_request_id: true
        idempotency_ttl: 24h
      timeouts:
        connect: 10s
        request: 10m
```

Note: the config schema contains additional fields such as retry, reconnect, token refresh, and session recovery, but they are not fully active runtime features in the current implementation.

## Validate configuration

Before starting the bot, validate the file:

```bash
go run ./cmd/acp2larkbot -config ./config.yaml -validate-only
```

Expected output:

```text
config ok
```

## Run

```bash
go run ./cmd/acp2larkbot -config ./config.yaml
```

Or run the compiled binary:

```bash
./acp2larkbot -config ./config.yaml
```

## How message handling works

At a high level:

1. Lark sends a websocket event.
2. The bot ignores self-generated or duplicate events.
3. The chat must exist in `chats[]`.
4. The sender must pass `authz` rules.
5. If the message is a built-in command, the bot handles it locally.
6. Otherwise the message is queued for the chat worker.
7. The worker resolves a session and sends the text to the ACP backend.
8. The response is streamed back to Lark.

## Built-in commands

### `/cancel`

Cancels the currently running request for the chat.

- the request owner can cancel it
- an admin can also cancel it

### `/status`

Shows whether the current chat is idle or running.

### `/reset RESET`

Admin-only command.

Important: in the current implementation, this command does **not** delete persisted auto-created session records. It only refuses reset while a request is still running and otherwise returns success.

## Session strategies

### `static`

Use a fixed ACP session ID from config.

### `auto_create`

Create a session on first use and persist the mapping in the local bbolt state file.

### `ephemeral`

Create a new session for each request and close it after the request finishes.

## Notes on rendering modes

The current codebase supports two renderer implementations:

- `mode: text` or `mode: card` -> standard text-update renderer
- `mode: card_streaming` -> CardKit streaming renderer

If you want real CardKit streaming behavior, use:

```yaml
lark:
  streaming:
    enabled: true
    mode: card_streaming
```

## Operational notes

- This project currently assumes a single running instance.
- The state file is local to the process.
- Worker queues are per process.
- Dedupe and self-message protection are local, not distributed.

## Run tests

```bash
go test ./...
```

Some live Lark tests are present in the repository but skip automatically unless the required Lark credentials are provided in the environment.
