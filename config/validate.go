package config

import (
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
	"time"
)

func Validate(cfg *Config) error {
	if err := validateLog(cfg.Log); err != nil {
		return err
	}
	if err := validateLark(cfg); err != nil {
		return err
	}
	if err := validateState(cfg); err != nil {
		return err
	}
	if err := validateCommands(cfg); err != nil {
		return err
	}
	if err := validateAgents(cfg); err != nil {
		return err
	}
	if err := validateChats(cfg); err != nil {
		return err
	}
	if err := validateAuthz(cfg); err != nil {
		return err
	}
	return nil
}

func validateLog(cfg LogConfig) error {
	switch cfg.Level {
	case "debug", "info", "warn", "error":
	default:
		return fmt.Errorf("log.level must be one of debug/info/warn/error, got %q", cfg.Level)
	}
	if cfg.RedactMessage {
		return fmt.Errorf("log.redact_message is reserved but not implemented")
	}
	if cfg.Audit.Enabled {
		return fmt.Errorf("log.audit is reserved but not implemented")
	}
	if !cfg.File.Enabled {
		return nil
	}
	if cfg.File.Path == "" {
		return fmt.Errorf("log.file.path is required when log.file.enabled is true")
	}
	if cfg.File.MaxSizeMB <= 0 || cfg.File.MaxSizeMB > 1024 {
		return fmt.Errorf("log.file.max_size_mb out of range")
	}
	if cfg.File.MaxBackups < 0 || cfg.File.MaxBackups > 1000 {
		return fmt.Errorf("log.file.max_backups out of range")
	}
	return nil
}

func validateLark(cfg *Config) error {
	if cfg.Lark.AppID == "" {
		return fmt.Errorf("lark.app_id is required")
	}
	if cfg.Lark.AppSecret == "" {
		return fmt.Errorf("lark.app_secret is required")
	}
	if cfg.Lark.ConnectionMode != "websocket" {
		return fmt.Errorf("lark.connection_mode must be websocket")
	}
	if !cfg.Lark.IgnoreSelfMessages {
		return fmt.Errorf("lark.ignore_self_messages must be true")
	}
	if cfg.Lark.Ignore.MessageIDTTL.Duration <= 0 || cfg.Lark.Ignore.MaxMessageIDs <= 0 {
		return fmt.Errorf("lark.ignore message_id_ttl and max_message_ids must be positive")
	}
	for _, typ := range cfg.Lark.Trigger.MessageTypes {
		if typ != "text" {
			return fmt.Errorf("unsupported trigger message type %q", typ)
		}
	}
	if !cfg.Lark.Trigger.IgnoreUpdateEvents || !cfg.Lark.Trigger.IgnoreCardEvents {
		return fmt.Errorf("lark trigger must ignore update and card events")
	}
	return validateStreaming(cfg.Lark.Streaming)
}

func validateStreaming(s StreamingConfig) error {
	if !s.Enabled {
		return nil
	}
	switch s.Mode {
	case "", StreamingModeText, StreamingModeCard, StreamingModeCardStreaming:
	default:
		return fmt.Errorf("streaming.mode must be one of text/card/card_streaming, got %q", s.Mode)
	}
	if s.UpdateInterval.Duration < 500*time.Millisecond {
		return fmt.Errorf("streaming.update_interval must be at least 500ms")
	}
	if s.MaxUpdatesPerMessage <= 0 || s.MaxUpdatesPerMessage > 1000 {
		return fmt.Errorf("streaming.max_updates_per_message out of range")
	}
	if s.MaxStreamDuration.Duration <= 0 || s.MaxStreamDuration.Duration > 24*time.Hour {
		return fmt.Errorf("streaming.max_stream_duration out of range")
	}
	if s.MaxFinalChars <= 0 || s.MaxFinalChars > 200000 {
		return fmt.Errorf("streaming.max_final_chars out of range")
	}
	if s.FallbackMaxMessages < 0 || s.FallbackMaxMessages > 100 {
		return fmt.Errorf("streaming.fallback_max_messages out of range")
	}
	if s.RateLimit.Global.Limit > 0 && s.RateLimit.PerChat.Limit > 0 && s.RateLimit.Global.PerSecond() < s.RateLimit.PerChat.PerSecond() {
		return fmt.Errorf("streaming global rate limit must not be smaller than per-chat rate")
	}
	return nil
}

func validateState(cfg *Config) error {
	if cfg.State.Type != "bolt" {
		return fmt.Errorf("state.type must be bolt")
	}
	if cfg.State.Path == "" {
		return fmt.Errorf("state.path is required")
	}
	if cfg.Dedupe.Enabled && cfg.Dedupe.TTL.Duration <= 0 {
		return fmt.Errorf("dedupe.ttl must be positive")
	}
	if cfg.UnknownChat.Behavior != UnknownReplyError && cfg.UnknownChat.Behavior != UnknownIgnore {
		return fmt.Errorf("unknown_chat.behavior must be reply_error or ignore")
	}
	if cfg.UnknownChat.RateLimitInterval.Duration <= 0 {
		return fmt.Errorf("unknown_chat.rate_limit_interval must be positive")
	}
	return nil
}

func validateCommands(cfg *Config) error {
	if !cfg.Commands.Enabled {
		return nil
	}
	if cfg.Commands.Audit {
		return fmt.Errorf("commands.audit is reserved but not implemented")
	}
	for _, name := range []string{CommandCancel, CommandStatus, CommandReset} {
		if _, ok := cfg.Commands.Rules[name]; !ok {
			return fmt.Errorf("commands.rules.%s is required", name)
		}
	}
	if !cfg.Commands.Rules[CommandReset].RequireConfirm {
		return fmt.Errorf("commands.rules.reset.require_confirm must be true")
	}
	if cfg.Commands.Rules[CommandReset].ConfirmText == "" {
		return fmt.Errorf("commands.rules.reset.confirm_text is required")
	}
	return nil
}

func validateAgents(cfg *Config) error {
	if len(cfg.ACP.Agents) == 0 {
		return fmt.Errorf("acp.agents is required")
	}
	if cfg.ACP.DefaultAgent == "" {
		return fmt.Errorf("acp.default_agent is required")
	}
	if _, ok := cfg.ACP.Agents[cfg.ACP.DefaultAgent]; !ok {
		return fmt.Errorf("acp.default_agent %q not found", cfg.ACP.DefaultAgent)
	}
	for id, agent := range cfg.ACP.Agents {
		switch agent.Type {
		case AgentTypeCmd:
			if err := validateCmdAgent(id, agent); err != nil {
				return err
			}
		case AgentTypeNetwork:
			if err := validateNetworkAgent(id, agent); err != nil {
				return err
			}
		default:
			return fmt.Errorf("agent %q has unsupported type %q", id, agent.Type)
		}
	}
	return nil
}

func validateCmdAgent(id string, agent AgentConfig) error {
	if agent.Command == "" {
		return fmt.Errorf("cmd agent %q command is required", id)
	}
	if !filepath.IsAbs(agent.CWD) {
		return fmt.Errorf("cmd agent %q cwd must be absolute", id)
	}
	if agent.Shell || agent.Command == "sh" || contains(agent.Args, "-lc") {
		return fmt.Errorf("cmd agent %q shell mode is high risk and not enabled in phase 1", id)
	}
	if agent.Restart.Enabled {
		return fmt.Errorf("cmd agent %q restart is reserved but not implemented", id)
	}
	if agent.SessionRecovery.Enabled {
		return fmt.Errorf("cmd agent %q session_recovery is reserved but not implemented", id)
	}
	return nil
}

func validateNetworkAgent(id string, agent AgentConfig) error {
	if agent.Protocol.Transport != "websocket" || agent.Protocol.Binding != "acp.websocket" {
		return fmt.Errorf("network agent %q must use acp.websocket", id)
	}
	if agent.Protocol.MaxInFlight != 1 {
		return fmt.Errorf("network agent %q max_inflight must be 1 unless multiplex is negotiated", id)
	}
	if agent.URL == "" {
		return fmt.Errorf("network agent %q url is required", id)
	}
	u, err := url.Parse(agent.URL)
	if err != nil {
		return fmt.Errorf("network agent %q parse url failed, %w", id, err)
	}
	if u.Scheme != "wss" && u.Scheme != "ws" {
		return fmt.Errorf("network agent %q url must use wss or ws", id)
	}
	if u.Scheme == "ws" && u.Hostname() != "127.0.0.1" && u.Hostname() != "localhost" {
		return fmt.Errorf("network agent %q ws scheme only allowed for localhost", id)
	}
	if !contains(agent.EndpointAllowlist, agent.URL) {
		return fmt.Errorf("network agent %q url must be in endpoint_allowlist", id)
	}
	if agent.TLS.InsecureSkipVerify && u.Scheme != "ws" {
		return fmt.Errorf("network agent %q tls.insecure_skip_verify must be false", id)
	}
	if !agent.Request.RequireRequestID {
		return fmt.Errorf("network agent %q request.require_request_id must be true", id)
	}
	if agent.Reconnect.Enabled {
		return fmt.Errorf("network agent %q reconnect is reserved but not implemented", id)
	}
	if agent.SessionRecovery.Enabled {
		return fmt.Errorf("network agent %q session_recovery is reserved but not implemented", id)
	}
	if agent.Heartbeat.Interval.Duration <= 0 || agent.Heartbeat.Timeout.Duration <= 0 || agent.Request.IdempotencyTTL.Duration <= 0 {
		return fmt.Errorf("network agent %q heartbeat and idempotency durations must be positive", id)
	}
	return nil
}

func validateChats(cfg *Config) error {
	found := map[string]struct{}{}
	staticSessions := map[string]string{}
	for _, chat := range cfg.Chats {
		if chat.ID == "" {
			return fmt.Errorf("chat.id is required")
		}
		if _, ok := found[chat.ID]; ok {
			return fmt.Errorf("duplicate chat.id %q", chat.ID)
		}
		found[chat.ID] = struct{}{}
		if strings.HasPrefix(chat.ID, "ou_") {
			return fmt.Errorf("chat %q id must be chat_id, not user id", chat.ID)
		}
		if _, ok := cfg.ACP.Agents[chat.Agent]; !ok {
			return fmt.Errorf("chat %q references missing agent %q", chat.ID, chat.Agent)
		}
		if chat.Queue.MaxPending <= 0 || chat.Queue.MaxPending > 100 {
			return fmt.Errorf("chat %q queue.max_pending out of range", chat.ID)
		}
		if chat.Queue.OnFull != QueueReject && chat.Queue.OnFull != QueueDropOldest {
			return fmt.Errorf("chat %q queue.on_full unsupported", chat.ID)
		}
		if err := validateSession(chat, staticSessions); err != nil {
			return err
		}
	}
	return nil
}

func validateSession(chat ChatConfig, staticSessions map[string]string) error {
	if !oneOf(chat.Session.Strategy, SessionStatic, SessionAutoCreate, SessionEphemeral) {
		return fmt.Errorf("chat %q has unsupported session strategy %q", chat.ID, chat.Session.Strategy)
	}
	if chat.Session.Scope == SessionScopeThread {
		return fmt.Errorf("chat %q session.scope=thread is reserved and not implemented", chat.ID)
	}
	if chat.Session.Scope != SessionScopeChat && chat.Session.Scope != SessionScopeSender {
		return fmt.Errorf("chat %q has unsupported session scope %q", chat.ID, chat.Session.Scope)
	}
	if chat.Type == ChatTypeGroup && chat.Session.Scope == SessionScopeChat && !chat.Session.AllowSharedSession {
		return fmt.Errorf("group chat %q shared chat scope requires allow_shared_session=true", chat.ID)
	}
	if chat.Session.TTL.Duration <= 0 || chat.Session.IdleTimeout.Duration <= 0 || chat.Session.IdleTimeout.Duration > chat.Session.TTL.Duration {
		return fmt.Errorf("chat %q session ttl/idle_timeout invalid", chat.ID)
	}
	if chat.Session.Strategy == SessionStatic {
		if chat.Session.ID == "" {
			return fmt.Errorf("chat %q static session requires id", chat.ID)
		}
		key := chat.Agent + "\x00" + chat.CWD + "\x00" + chat.Session.Scope + "\x00" + chat.Session.ID
		if prev, ok := staticSessions[key]; ok {
			return fmt.Errorf("static session %q reused by chats %q and %q", chat.Session.ID, prev, chat.ID)
		}
		staticSessions[key] = chat.ID
	}
	if chat.Session.Strategy == SessionAutoCreate && chat.Session.Prefix == "" {
		return fmt.Errorf("chat %q auto_create session requires prefix", chat.ID)
	}
	return nil
}

func validateAuthz(cfg *Config) error {
	if cfg.Authz.Access.Default != "deny" && cfg.Authz.Access.Default != "allow" {
		return fmt.Errorf("authz.access.default must be deny or allow")
	}
	return nil
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func oneOf(value string, allowed ...string) bool {
	for _, item := range allowed {
		if value == item {
			return true
		}
	}
	return false
}
