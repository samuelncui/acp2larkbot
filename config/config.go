package config

type Config struct {
	Log         LogConfig         `yaml:"log"`
	Lark        LarkConfig        `yaml:"lark"`
	Authz       AuthzConfig       `yaml:"authz"`
	UnknownChat UnknownChatConfig `yaml:"unknown_chat"`
	Dedupe      DedupeConfig      `yaml:"dedupe"`
	State       StateConfig       `yaml:"state"`
	Commands    CommandsConfig    `yaml:"commands"`
	ACP         ACPConfig         `yaml:"acp"`
	Chats       []ChatConfig      `yaml:"chats"`
}

type LogConfig struct {
	Level         string      `yaml:"level"`
	RedactMessage bool        `yaml:"redact_message"`
	File          LogFile     `yaml:"file"`
	Audit         AuditConfig `yaml:"audit"`
}

type LogFile struct {
	Enabled    bool   `yaml:"enabled"`
	Path       string `yaml:"path"`
	MaxSizeMB  int    `yaml:"max_size_mb"`
	MaxBackups int    `yaml:"max_backups"`
}

type AuditConfig struct {
	Enabled         bool `yaml:"enabled"`
	RedactUserID    bool `yaml:"redact_user_id"`
	RedactSessionID bool `yaml:"redact_session_id"`
}

type LarkConfig struct {
	AppID              string          `yaml:"app_id" env:"ACP2LARKBOT_LARK_APP_ID"`
	AppSecret          string          `yaml:"app_secret" env:"ACP2LARKBOT_LARK_APP_SECRET"`
	BotOpenID          string          `yaml:"bot_open_id" env:"ACP2LARKBOT_LARK_BOT_OPEN_ID"`
	BotUserID          string          `yaml:"bot_user_id" env:"ACP2LARKBOT_LARK_BOT_USER_ID"`
	Domain             string          `yaml:"domain" env:"ACP2LARKBOT_LARK_DOMAIN"`
	ConnectionMode     string          `yaml:"connection_mode"`
	RequireMention     bool            `yaml:"require_mention"`
	IgnoreSelfMessages bool            `yaml:"ignore_self_messages"`
	Ignore             LarkIgnore      `yaml:"ignore"`
	Trigger            LarkTrigger     `yaml:"trigger"`
	Streaming          StreamingConfig `yaml:"streaming"`
}

type LarkIgnore struct {
	SenderTypes   []string `yaml:"sender_types"`
	SelfAppID     string   `yaml:"self_app_id"`
	MessageIDTTL  Duration `yaml:"message_id_ttl"`
	MaxMessageIDs int      `yaml:"max_message_ids"`
}

type LarkTrigger struct {
	MessageTypes          []string `yaml:"message_types"`
	IgnoreUpdateEvents    bool     `yaml:"ignore_update_events"`
	IgnoreCardEvents      bool     `yaml:"ignore_card_events"`
	RequireMentionInGroup bool     `yaml:"require_mention_in_group"`
}

type StreamingConfig struct {
	Enabled              bool          `yaml:"enabled"`
	Mode                 string        `yaml:"mode"`
	UpdateInterval       Duration      `yaml:"update_interval"`
	MinUpdateChars       int           `yaml:"min_update_chars"`
	MaxUpdateChars       int           `yaml:"max_update_chars"`
	MaxUpdatesPerMessage int           `yaml:"max_updates_per_message"`
	MaxStreamDuration    Duration      `yaml:"max_stream_duration"`
	MaxFinalChars        int           `yaml:"max_final_chars"`
	Fallback             string        `yaml:"fallback"`
	FallbackMaxMessages  int           `yaml:"fallback_max_messages"`
	TruncateNotice       string        `yaml:"truncate_notice"`
	Retry                RetryConfig   `yaml:"retry"`
	RateLimit            RateLimitPair `yaml:"rate_limit"`
}

type RetryConfig struct {
	MaxRetries    int      `yaml:"max_retries"`
	Backoff       Duration `yaml:"backoff"`
	MaxBackoff    Duration `yaml:"max_backoff"`
	RetryAfter429 bool     `yaml:"retry_after_429"`
}

type RateLimitPair struct {
	PerChat Rate `yaml:"per_chat"`
	Global  Rate `yaml:"global"`
}

type AuthzConfig struct {
	Admins []string     `yaml:"admins"`
	Access AccessConfig `yaml:"access"`
}

type AccessConfig struct {
	Default string          `yaml:"default"`
	Users   []UserRole      `yaml:"users"`
	Chats   []ChatUserAllow `yaml:"chats"`
}

type UserRole struct {
	ID   string `yaml:"id"`
	Role string `yaml:"role"`
}

type ChatUserAllow struct {
	ID    string   `yaml:"id"`
	Users []string `yaml:"users"`
}

type UnknownChatConfig struct {
	Behavior          string   `yaml:"behavior"`
	Message           string   `yaml:"message"`
	IncludeChatID     bool     `yaml:"include_chat_id"`
	RateLimitInterval Duration `yaml:"rate_limit_interval"`
}

type DedupeConfig struct {
	Enabled bool     `yaml:"enabled"`
	TTL     Duration `yaml:"ttl"`
}

type StateConfig struct {
	Type string `yaml:"type"`
	Path string `yaml:"path"`
}

type CommandsConfig struct {
	Enabled bool                   `yaml:"enabled"`
	Prefix  string                 `yaml:"prefix"`
	Audit   bool                   `yaml:"audit"`
	Rules   map[string]CommandRule `yaml:"rules"`
}

type CommandRule struct {
	Roles                   []string `yaml:"roles"`
	OnlyRequestOwnerOrAdmin bool     `yaml:"only_request_owner_or_admin"`
	Redact                  bool     `yaml:"redact"`
	RequireConfirm          bool     `yaml:"require_confirm"`
	ConfirmText             string   `yaml:"confirm_text"`
}

type ACPConfig struct {
	DefaultAgent string                 `yaml:"default_agent"`
	Agents       map[string]AgentConfig `yaml:"agents"`
}

type AgentConfig struct {
	Type              string          `yaml:"type"`
	Command           string          `yaml:"command"`
	Shell             bool            `yaml:"shell"`
	Args              []string        `yaml:"args"`
	CWD               string          `yaml:"cwd"`
	Restart           RestartConfig   `yaml:"restart"`
	Timeouts          AgentTimeouts   `yaml:"timeouts"`
	Protocol          NetworkProtocol `yaml:"protocol"`
	URL               string          `yaml:"url"`
	EndpointAllowlist []string        `yaml:"endpoint_allowlist"`
	Auth              NetworkAuth     `yaml:"auth"`
	TLS               TLSConfig       `yaml:"tls"`
	Heartbeat         HeartbeatConfig `yaml:"heartbeat"`
	Reconnect         ReconnectConfig `yaml:"reconnect"`
	Request           NetworkRequest  `yaml:"request"`
	SessionRecovery   SessionRecovery `yaml:"session_recovery"`
}

type RestartConfig struct {
	Enabled    bool     `yaml:"enabled"`
	MaxRetries int      `yaml:"max_retries"`
	Backoff    Duration `yaml:"backoff"`
}

type AgentTimeouts struct {
	Start   Duration `yaml:"start"`
	Request Duration `yaml:"request"`
	Idle    Duration `yaml:"idle"`
	Connect Duration `yaml:"connect"`
}

type NetworkProtocol struct {
	Transport   string `yaml:"transport"`
	Binding     string `yaml:"binding"`
	MaxInFlight int    `yaml:"max_inflight"`
}

type NetworkAuth struct {
	Type           string         `yaml:"type"`
	Token          string         `yaml:"token"`
	RefreshCommand RefreshCommand `yaml:"refresh_command"`
}

type RefreshCommand struct {
	Command string   `yaml:"command"`
	Args    []string `yaml:"args"`
	Timeout Duration `yaml:"timeout"`
}

type TLSConfig struct {
	InsecureSkipVerify bool   `yaml:"insecure_skip_verify"`
	ServerName         string `yaml:"server_name"`
	CAFile             string `yaml:"ca_file"`
}

type HeartbeatConfig struct {
	Type     string   `yaml:"type"`
	Interval Duration `yaml:"interval"`
	Timeout  Duration `yaml:"timeout"`
}

type ReconnectConfig struct {
	Enabled    bool     `yaml:"enabled"`
	MaxRetries int      `yaml:"max_retries"`
	Backoff    Duration `yaml:"backoff"`
	MaxBackoff Duration `yaml:"max_backoff"`
}

type NetworkRequest struct {
	RequireRequestID bool     `yaml:"require_request_id"`
	IdempotencyTTL   Duration `yaml:"idempotency_ttl"`
}

type SessionRecovery struct {
	Enabled      bool     `yaml:"enabled"`
	ResumeWindow Duration `yaml:"resume_window"`
}

type ChatConfig struct {
	ID             string        `yaml:"id"`
	Type           string        `yaml:"type"`
	UserID         string        `yaml:"user_id"`
	Name           string        `yaml:"name"`
	Agent          string        `yaml:"agent"`
	CWD            string        `yaml:"cwd"`
	RequireMention *bool         `yaml:"require_mention"`
	Queue          QueueConfig   `yaml:"queue"`
	Session        SessionPolicy `yaml:"session"`
}

type QueueConfig struct {
	MaxPending int    `yaml:"max_pending"`
	OnFull     string `yaml:"on_full"`
}

type SessionPolicy struct {
	Strategy           string   `yaml:"strategy"`
	Scope              string   `yaml:"scope"`
	ID                 string   `yaml:"id"`
	Prefix             string   `yaml:"prefix"`
	AllowSharedSession bool     `yaml:"allow_shared_session"`
	TTL                Duration `yaml:"ttl"`
	IdleTimeout        Duration `yaml:"idle_timeout"`
}
