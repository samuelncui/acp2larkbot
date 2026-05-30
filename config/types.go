package config

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	AgentTypeCmd     = "cmd"
	AgentTypeNetwork = "network"

	SessionStatic     = "static"
	SessionAutoCreate = "auto_create"
	SessionEphemeral  = "ephemeral"

	SessionScopeChat   = "chat"
	SessionScopeSender = "sender"
	SessionScopeThread = "thread"

	ChatTypeGroup  = "group"
	ChatTypeDirect = "direct"

	QueueReject     = "reject"
	QueueDropOldest = "drop_oldest"

	UnknownReplyError = "reply_error"
	UnknownIgnore     = "ignore"

	CommandCancel = "cancel"
	CommandStatus = "status"
	CommandReset  = "reset"

	StreamingModeText          = "text"
	StreamingModeCard          = "card"
	StreamingModeCardStreaming = "card_streaming"
)

type Duration struct {
	time.Duration
}

func (d *Duration) UnmarshalYAML(value *yaml.Node) error {
	var s string
	if err := value.Decode(&s); err != nil {
		return fmt.Errorf("duration must be string")
	}
	parsed, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("parse duration %q failed, %w", s, err)
	}
	d.Duration = parsed
	return nil
}

type Rate struct {
	Limit  int
	Window time.Duration
}

func (r *Rate) UnmarshalYAML(value *yaml.Node) error {
	var s string
	if err := value.Decode(&s); err != nil {
		return fmt.Errorf("rate must be string")
	}
	parsed, err := ParseRate(s)
	if err != nil {
		return err
	}
	*r = parsed
	return nil
}

func ParseRate(s string) (Rate, error) {
	parts := strings.Split(s, "/")
	if len(parts) != 2 {
		return Rate{}, fmt.Errorf("rate %q must use <n>/<unit>", s)
	}
	limit, err := strconv.Atoi(parts[0])
	if err != nil {
		return Rate{}, fmt.Errorf("parse rate limit %q failed, %w", parts[0], err)
	}
	if limit <= 0 {
		return Rate{}, fmt.Errorf("rate limit must be positive")
	}

	var window time.Duration
	switch parts[1] {
	case "s", "sec", "second":
		window = time.Second
	case "m", "min", "minute":
		window = time.Minute
	case "h", "hour":
		window = time.Hour
	default:
		return Rate{}, fmt.Errorf("unsupported rate window %q", parts[1])
	}
	return Rate{Limit: limit, Window: window}, nil
}

func (r Rate) PerSecond() float64 {
	if r.Window <= 0 {
		return 0
	}
	return float64(r.Limit) / r.Window.Seconds()
}
