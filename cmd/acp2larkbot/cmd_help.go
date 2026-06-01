package main

import (
	"fmt"
)

func helpCmd(args []string) {
	topic := ""
	if len(args) > 0 {
		topic = args[0]
	}

	switch topic {
	case "lark":
		printLarkHelp()
	case "acp":
		printACPHelp()
	case "session":
		printSessionHelp()
	case "network":
		printNetworkHelp()
	default:
		printOverview()
	}
}

func printOverview() {
	fmt.Print(`acp2larkbot - Lark bot to ACP agent bridge

USAGE:
    acp2larkbot <command> [options]

COMMANDS:
    run      Start the bot service
    test     Validate configuration
    init     Interactive config wizard
    help     Show detailed help (this page)
    version  Show version

QUICK START:
    1. Create config:  acp2larkbot init -o config.yaml
    2. Validate:       acp2larkbot test -c config.yaml
    3. Run:            acp2larkbot run -c config.yaml

HELP TOPICS:
    acp2larkbot help lark      Lark app configuration
    acp2larkbot help acp       ACP backend configuration
    acp2larkbot help session   Session strategies
    acp2larkbot help network   Network ACP via websocket

ENVIRONMENT VARIABLES:
    ACP2LARKBOT_LARK_APP_ID       Lark app ID (overrides config)
    ACP2LARKBOT_LARK_APP_SECRET   Lark app secret (overrides config)
    ACP2LARKBOT_LARK_DOMAIN       Lark domain (lark/feishu)

CONFIG FILE:
    Default: config.yaml in current directory
    Override: -c /path/to/config.yaml
`)
}

func printLarkHelp() {
	fmt.Print(`LARK CONFIGURATION

Required fields:
    lark.app_id           Your Lark app ID (cli_xxxxx)
    lark.app_secret       Your Lark app secret

Optional fields:
    lark.domain           "lark" (default) or "feishu"
    lark.connection_mode  Must be "websocket"
    lark.require_mention  false (default)

Creating a Lark app:
    1. Go to https://open.larksuite.com/app
    2. Create a new app
    3. Enable "Bot" capability
    4. Enable "Events" > "Receive messages" (im.message.receive_v1)
    5. Set connection mode to "WebSocket"
    6. Copy app_id and app_secret to your config

Environment variables (override config):
    ACP2LARKBOT_LARK_APP_ID
    ACP2LARKBOT_LARK_APP_SECRET
    ACP2LARKBOT_LARK_DOMAIN
`)
}

func printACPHelp() {
	fmt.Print(`ACP BACKEND CONFIGURATION

Two backend types are supported:

1. CMD (local executable):
    acp:
      default_agent: local
      agents:
        local:
          type: cmd
          command: /path/to/acp-server
          cwd: /path/to/workspace
          args: []
          timeouts:
            start: 30s
            request: 10m
            idle: 30m

    The bot spawns the command and communicates via stdio (JSON-RPC 2.0).
    The command must implement the Agent Client Protocol.

2. NETWORK (remote websocket):
    acp:
      default_agent: remote
      agents:
        remote:
          type: network
          protocol:
            transport: websocket
            binding: acp.websocket
            max_inflight: 1
          url: wss://your-acp-server.com/acp
          endpoint_allowlist:
            - wss://your-acp-server.com/acp
          auth:
            type: bearer
            token: YOUR_TOKEN
          tls:
            insecure_skip_verify: false
          timeouts:
            connect: 10s
            request: 10m

    For local testing, ws:// is allowed for 127.0.0.1/localhost.
`)
}

func printSessionHelp() {
	fmt.Print(`SESSION STRATEGIES

Three strategies control how ACP sessions are managed:

1. static - Use a fixed session ID from config:
    session:
      strategy: static
      id: your-session-id

2. auto_create - Create session on first use, reuse until expiry:
    session:
      strategy: auto_create
      scope: sender        # or "chat"
      prefix: lark
      ttl: 168h            # 7 days
      idle_timeout: 24h    # 1 day

    Sessions are persisted in the local bbolt state file.

3. ephemeral - New session per request, closed after completion:
    session:
      strategy: ephemeral

Session scopes:
    sender  - One session per user (default)
    chat    - One session per chat (requires allow_shared_session=true for groups)
`)
}

func printNetworkHelp() {
	fmt.Print(`NETWORK ACP CONFIGURATION

Network ACP connects to a remote ACP server via websocket.
The bot speaks the Agent Client Protocol over JSON-RPC 2.0.

Basic config:
    acp:
      default_agent: remote
      agents:
        remote:
          type: network
          protocol:
            transport: websocket
            binding: acp.websocket
            max_inflight: 1
          url: wss://your-server.com/acp
          endpoint_allowlist:
            - wss://your-server.com/acp
          tls:
            insecure_skip_verify: false
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

Local testing with websocat:
    # Bridge hermes acp (stdio) to websocket:
    websocat -E -b ws-listen:127.0.0.1:18789 'cmd:hermes acp'

    # Then in config:
    acp:
      agents:
        hermes-ws:
          type: network
          protocol:
            transport: websocket
            binding: acp.websocket
            max_inflight: 1
          url: ws://127.0.0.1:18789
          endpoint_allowlist:
            - ws://127.0.0.1:18789
          cwd: /path/to/workspace

    Note: ws:// is only allowed for localhost.
`)
}
