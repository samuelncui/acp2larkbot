package acp

import (
	"fmt"

	"github.com/samuelncui/acp2larkbot/config"
)

type Registry struct {
	clients map[string]Client
}

type closeClient interface {
	Close() error
}

func NewRegistry(cfg config.ACPConfig) (*Registry, error) {
	clients := map[string]Client{}
	for id, agent := range cfg.Agents {
		switch agent.Type {
		case config.AgentTypeCmd:
			clients[id] = NewCmdClient(agent)
		case config.AgentTypeNetwork:
			clients[id] = NewWSClient(agent)
		default:
			return nil, fmt.Errorf("unsupported agent type %q", agent.Type)
		}
	}
	return &Registry{clients: clients}, nil
}

func (r *Registry) Get(agentID string) (Client, bool) {
	client, ok := r.clients[agentID]
	return client, ok
}

func (r *Registry) Close() error {
	var firstErr error
	for id, client := range r.clients {
		closer, ok := client.(closeClient)
		if !ok {
			continue
		}
		if err := closer.Close(); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("close ACP client %q failed, %w", id, err)
		}
	}
	return firstErr
}
