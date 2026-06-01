# acp2larkbot

A Go service that bridges a **Lark (飞书) bot** to an **ACP agent backend**.

You send a message in a Lark chat → the bot forwards it to an ACP agent → the agent's response streams back to Lark in real time.

## Quick Install

```bash
curl -fsSL https://raw.githubusercontent.com/samuelncui/acp2larkbot/main/install.sh | bash
```

Or pick a binary manually from [Releases](https://github.com/samuelncui/acp2larkbot/releases).

## Prerequisites

- Go 1.22+ (if building from source)
- A Lark app with websocket event access
- Lark `app_id` and `app_secret`
- An ACP backend (local executable or remote websocket endpoint)

## Install

### One-liner (recommended)

```bash
curl -fsSL https://raw.githubusercontent.com/samuelncui/acp2larkbot/main/install.sh | bash
```

Installs to `/usr/local/bin/acp2larkbot` (requires sudo). Set `BINDIR=~/.local/bin` to install elsewhere.

### From source

```bash
go install github.com/samuelncui/acp2larkbot/cmd/acp2larkbot@latest
```

### Manual download

Go to [Releases](https://github.com/samuelncui/acp2larkbot/releases), download the tar.gz for your platform, extract and put the binary in your PATH.

## Quick Start

1. Create a config file `config.yaml`:

```yaml
lark:
  app_id: ${ACP2LARKBOT_LARK_APP_ID}
  app_secret: ${ACP2LARKBOT_LARK_APP_SECRET}

unknown_chat:
  behavior: reply_error
  message: 当前会话未启用 acp2larkbot。

dedupe:
  enabled: true
  ttl: 24h

state:
  type: bolt
  path: ./state/acp2larkbot.db

acp:
  default_agent: local
  agents:
    local:
      type: cmd
      command: /path/to/your-acp-server
      cwd: /path/to/workspace

chats:
  - id: oc_xxxxxxxxxxxx
    agent: local
```

2. Validate:

```bash
acp2larkbot -config ./config.yaml -validate-only
# → config ok
```

3. Run:

```bash
ACP2LARKBOT_LARK_APP_ID=cli_xxx \
ACP2LARKBOT_LARK_APP_SECRET=xxx \
acp2larkbot -config ./config.yaml
```

## Configuration

The config file is YAML. Sensitive fields support `${ENV_VAR}` interpolation.

### Required top-level sections

| Section | Purpose |
|----------|---------|
| `lark` | Lark app credentials and event handling |
| `unknown_chat` | Behavior when an unlisted chat sends a message |
| `dedupe` | Duplicate message detection |
| `state` | Local state storage (bbolt) |
| `acp` | ACP backend agent definitions |
| `chats` | Allowed chats and their agent assignments |

### Lark

```yaml
lark:
  app_id: ${ACP2LARKBOT_LARK_APP_ID}   # required
  app_secret: ${ACP2LARKBOT_LARK_APP_SECRET}  # required
  domain: feishu                        # default: feishu
  connection_mode: websocket            # only websocket supported
  ignore_self_messages: true            # must be true
  trigger:
    message_types: [text]               # only text supported
    ignore_update_events: true          # must be true
    ignore_card_events: true            # must be true
    require_mention_in_group: false
  streaming:
    enabled: true
    mode: text                          # text | card_streaming
    update_interval: 1s
    min_update_chars: 16
    max_update_chars: 8000
    max_updates_per_message: 60
    max_final_chars: 20000
    fallback: append_messages
    fallback_max_messages: 3
    truncate_notice: "\n\n[truncated]"
```

### ACP Agents

Two types are supported:

**`cmd`** — local executable speaking ACP over stdio:

```yaml
acp:
  default_agent: my-agent
  agents:
    my-agent:
      type: cmd
      command: /absolute/path/to/acp-server
      args: []
      cwd: /absolute/path/to/workspace
      timeouts:
        start: 30s
        request: 10m
        idle: 30m
```

**`network`** — remote ACP websocket endpoint:

```yaml
acp:
  agents:
    remote:
      type: network
      protocol:
        transport: websocket
        binding: acp.websocket
        max_inflight: 1
      url: wss://your-acp.example.com/acp
      endpoint_allowlist:
        - wss://your-acp.example.com/acp
      auth:
        type: bearer
        token: ${ACP_TOKEN}
      tls:
        insecure_skip_verify: false
        server_name: your-acp.example.com
      timeouts:
        connect: 10s
        request: 10m
```

### Chats

```yaml
chats:
  - id: oc_xxxxxxxxxxxx     # chat ID from Lark
    agent: my-agent          # references acp.agents key
    queue:
      max_pending: 5
      on_full: reject        # reject | drop_oldest
    session:
      strategy: auto_create  # static | auto_create | ephemeral
      scope: sender          # chat | sender
      prefix: lark-auto
      ttl: 168h
      idle_timeout: 24h
```

### Authorization (optional)

```yaml
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
```

### Commands (optional)

```yaml
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
```

## Built-in Commands

When `commands.enabled: true`, users in allowed chats can use:

| Command | Who | What |
|---------|-----|------|
| `/cancel` | requester or admin | Cancel the running request |
| `/status` | any allowed user | Show chat queue status |
| `/reset RESET` | admin only | Safety check (does not delete sessions) |

## Session Strategies

| Strategy | Behavior |
|----------|----------|
| `static` | Use a fixed session ID from config |
| `auto_create` | Create session on first use, reuse until expiry (persisted in bbolt) |
| `ephemeral` | New session per request, closed after completion |

## Streaming Modes

| Mode | Behavior |
|------|----------|
| `text` | Update the Lark message text in place |
| `card_streaming` | Use CardKit streaming cards for richer output |

## State

The bot stores three kinds of data in a local bbolt file:

- Session records (for `auto_create`)
- Dedupe keys
- Rate-limit timestamps for unknown-chat replies

`/reset` does not delete persisted sessions in the current implementation.

## Operational Notes

- Single instance per Lark app. Running multiple instances against the same bot will break deduplication and ordering.
- The state file is local. Back it up if sessions are important.
- No distributed coordination. No multi-instance support.

## Build from Source

```bash
git clone https://github.com/samuelncui/acp2larkbot.git
cd acp2larkbot
go build ./cmd/acp2larkbot
```

## Run Tests

```bash
go test ./...
```

Some live Lark tests skip automatically unless Lark credentials are provided in the environment.

## Architecture Docs

Detailed design notes per module:

- [.agent/documents/app.md](.agent/documents/app.md) — Application lifecycle and event flow
- [.agent/documents/config.md](.agent/documents/config.md) — Config loading, defaults, validation rules
- [.agent/documents/lark.md](.agent/documents/lark.md) — Lark websocket integration
- [.agent/documents/router.md](.agent/documents/router.md) — Message routing and commands
- [.agent/documents/session-worker-state.md](.agent/documents/session-worker-state.md) — Session and worker design
- [.agent/documents/acp.md](.agent/documents/acp.md) — ACP client protocol

## License

BSD 2-Clause
