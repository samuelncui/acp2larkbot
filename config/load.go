package config

import (
	"fmt"
	"io/fs"
	"testing/fstest"

	"github.com/jinzhu/configor"
)

const envPrefix = "ACP2LARKBOT"

func LoadFile(path string) (*Config, error) {
	var cfg Config
	if err := newConfigor(nil).Load(&cfg, path); err != nil {
		return nil, fmt.Errorf("load config %q failed, %w", path, err)
	}
	return finishLoad(&cfg)
}

func Load(b []byte) (*Config, error) {
	var cfg Config
	fsys := fstest.MapFS{"config.yaml": {Data: b}}
	if err := newConfigor(fsys).Load(&cfg, "config.yaml"); err != nil {
		return nil, fmt.Errorf("decode config failed, %w", err)
	}
	return finishLoad(&cfg)
}

func finishLoad(cfg *Config) (*Config, error) {
	setDefaults(cfg)
	if err := Validate(cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

func newConfigor(fsys fs.FS) *configor.Configor {
	cfg := &configor.Config{
		ENVPrefix:            envPrefix,
		ErrorOnUnmatchedKeys: true,
		Silent:               true,
	}
	if fsys != nil {
		cfg.FS = fsys
	}
	return configor.New(cfg)
}

func setDefaults(cfg *Config) {
	if cfg.Log.Level == "" {
		cfg.Log.Level = "info"
	}
	if cfg.Log.File.Enabled {
		if cfg.Log.File.Path == "" {
			cfg.Log.File.Path = ".local/logs/acp2larkbot.log"
		}
		if cfg.Log.File.MaxSizeMB == 0 {
			cfg.Log.File.MaxSizeMB = 10
		}
		if cfg.Log.File.MaxBackups == 0 {
			cfg.Log.File.MaxBackups = 5
		}
	}
	if cfg.Lark.Domain == "" {
		cfg.Lark.Domain = "lark"
	}
	if cfg.Lark.ConnectionMode == "" {
		cfg.Lark.ConnectionMode = "websocket"
	}
	if cfg.Lark.Ignore.SelfAppID == "" {
		cfg.Lark.Ignore.SelfAppID = cfg.Lark.AppID
	}
	if len(cfg.Lark.Ignore.SenderTypes) == 0 {
		cfg.Lark.Ignore.SenderTypes = []string{"bot"}
	}
	if cfg.UnknownChat.Behavior == "" {
		cfg.UnknownChat.Behavior = UnknownReplyError
	}
	if cfg.UnknownChat.Message == "" {
		cfg.UnknownChat.Message = "acp2larkbot is not enabled for this chat."
	}
	if cfg.State.Type == "" {
		cfg.State.Type = "bolt"
	}
	if cfg.Commands.Prefix == "" {
		cfg.Commands.Prefix = "/"
	}
	for i := range cfg.Chats {
		if cfg.Chats[i].Queue.MaxPending == 0 {
			cfg.Chats[i].Queue.MaxPending = 5
		}
		if cfg.Chats[i].Queue.OnFull == "" {
			cfg.Chats[i].Queue.OnFull = QueueReject
		}
		if cfg.Chats[i].Agent == "" {
			cfg.Chats[i].Agent = cfg.ACP.DefaultAgent
		}
		if cfg.Chats[i].Session.Scope == "" {
			cfg.Chats[i].Session.Scope = SessionScopeSender
		}
	}
	for id, agent := range cfg.ACP.Agents {
		if agent.Protocol.MaxInFlight == 0 {
			agent.Protocol.MaxInFlight = 1
		}
		cfg.ACP.Agents[id] = agent
	}
}
