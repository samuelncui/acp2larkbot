package config

import "testing"

func TestLoadRejectsThreadScope(t *testing.T) {
	_, err := Load([]byte(validConfig(`scope: thread`)))
	if err == nil {
		t.Fatal("expected thread scope to fail validation")
	}
}

func TestLoadRejectsDirectUserIDAsChatID(t *testing.T) {
	cfg := validConfig("scope: sender")
	cfg = replace(cfg, "  - id: oc_xxxxxxxxxxxxxxxxx\n    type: group", "  - id: ou_user\n    type: group")
	_, err := Load([]byte(cfg))
	if err == nil {
		t.Fatal("expected user id as chat id to fail validation")
	}
}

func TestLoadAcceptsEphemeral(t *testing.T) {
	cfg := validConfig("scope: sender")
	cfg = replace(cfg, "strategy: auto_create", "strategy: ephemeral")
	_, err := Load([]byte(cfg))
	if err != nil {
		t.Fatalf("load config failed, %v", err)
	}
}

func TestLoadReadsLarkCredentialsFromConfigorEnv(t *testing.T) {
	t.Setenv("ACP2LARKBOT_LARK_APP_ID", "env_app")
	t.Setenv("ACP2LARKBOT_LARK_APP_SECRET", "env_secret")
	t.Setenv("ACP2LARKBOT_LARK_DOMAIN", "https://open.larksuite.com")
	cfg := validConfig("scope: sender")
	cfg = replace(cfg, "  app_id: app\n  app_secret: secret", "")
	loaded, err := Load([]byte(cfg))
	if err != nil {
		t.Fatalf("load config failed, %v", err)
	}
	if loaded.Lark.AppID != "env_app" {
		t.Fatalf("AppID = %q", loaded.Lark.AppID)
	}
	if loaded.Lark.AppSecret != "env_secret" {
		t.Fatalf("AppSecret = %q", loaded.Lark.AppSecret)
	}
	if loaded.Lark.Domain != "https://open.larksuite.com" {
		t.Fatalf("Domain = %q", loaded.Lark.Domain)
	}
}

func TestLoadLogFileDefaults(t *testing.T) {
	cfg := validConfig("scope: sender")
	cfg = replace(cfg, "log:\n  level: info", "log:\n  level: info\n  file:\n    enabled: true")
	loaded, err := Load([]byte(cfg))
	if err != nil {
		t.Fatalf("load config failed, %v", err)
	}
	if loaded.Log.File.Path == "" {
		t.Fatal("expected default log file path")
	}
	if loaded.Log.File.MaxSizeMB != 10 {
		t.Fatalf("MaxSizeMB = %d, want 10", loaded.Log.File.MaxSizeMB)
	}
	if loaded.Log.File.MaxBackups != 5 {
		t.Fatalf("MaxBackups = %d, want 5", loaded.Log.File.MaxBackups)
	}
}

func TestLoadRejectsLegacySandboxConfig(t *testing.T) {
	cfg := validConfig("scope: sender")
	cfg = replace(cfg, "      cwd: /workspace\n      timeouts:", "      cwd: /workspace\n      sandbox:\n        enabled: true\n      timeouts:")
	_, err := Load([]byte(cfg))
	if err == nil {
		t.Fatal("expected legacy sandbox config to fail validation")
	}
}

func TestLoadRejectsReservedRuntimeConfigs(t *testing.T) {
	tests := []struct {
		name string
		old  string
		new  string
	}{
		{name: "log audit", old: "log:\n  level: info", new: "log:\n  level: info\n  audit:\n    enabled: true"},
		{name: "command audit", old: "commands:\n  enabled: true", new: "commands:\n  enabled: true\n  audit: true"},
		{name: "cmd restart", old: "      cwd: /workspace\n      timeouts:", new: "      cwd: /workspace\n      restart:\n        enabled: true\n      timeouts:"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Load([]byte(replace(validConfig("scope: sender"), tt.old, tt.new)))
			if err == nil {
				t.Fatal("expected reserved config to fail validation")
			}
		})
	}
}

func validConfig(scopeLine string) string {
	return `
log:
  level: info
lark:
  app_id: app
  app_secret: secret
  connection_mode: websocket
  ignore_self_messages: true
  ignore:
    sender_types: [bot]
    self_app_id: app
    message_id_ttl: 24h
    max_message_ids: 100
  trigger:
    message_types: [text]
    ignore_update_events: true
    ignore_card_events: true
  streaming:
    enabled: true
    mode: card
    update_interval: 1s
    min_update_chars: 1
    max_update_chars: 1000
    max_updates_per_message: 10
    max_stream_duration: 10m
    max_final_chars: 1000
    fallback: append_messages
    fallback_max_messages: 3
    truncate_notice: truncated
    retry:
      max_retries: 3
      backoff: 1s
      max_backoff: 10s
      retry_after_429: true
    rate_limit:
      per_chat: 10/min
      global: 100/min
authz:
  access:
    default: deny
    users:
      - id: ou_user
        role: user
    chats:
      - id: oc_xxxxxxxxxxxxxxxxx
        users: [ou_user]
unknown_chat:
  behavior: reply_error
  rate_limit_interval: 10m
dedupe:
  enabled: true
  ttl: 24h
state:
  type: bolt
  path: ./state/test.db
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
  default_agent: fake
  agents:
    fake:
      type: cmd
      command: fake-acp
      cwd: /workspace
      timeouts:
        start: 10s
        request: 10m
        idle: 30m
chats:
  - id: oc_xxxxxxxxxxxxxxxxx
    type: group
    agent: fake
    cwd: /workspace
    queue:
      max_pending: 5
      on_full: reject
    session:
      strategy: auto_create
      ` + scopeLine + `
      prefix: lark-auto
      allow_shared_session: false
      ttl: 168h
      idle_timeout: 24h
`
}

func replace(s, old, next string) string {
	for i := 0; i+len(old) <= len(s); i++ {
		if s[i:i+len(old)] == old {
			return s[:i] + next + s[i+len(old):]
		}
	}
	return s
}
