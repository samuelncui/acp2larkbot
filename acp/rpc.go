package acp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
)

type rpcTransport interface {
	Read(ctx context.Context) ([]byte, error)
	Write(ctx context.Context, req JSONRPCRequest) error
	Close() error
}

type rpcClient struct {
	transport rpcTransport
	mu        sync.Mutex
	pending   map[string]*pendingRequest
	readOnce  sync.Once
	readErr   chan error
}

type pendingRequest struct {
	mu     sync.Mutex
	closed bool
	done   chan struct{}
	resp   chan JSONRPCResponse
	events chan Event
}

func (p *pendingRequest) sendEvent(event Event) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed || p.events == nil {
		return false
	}
	p.events <- event
	return true
}

func (p *pendingRequest) closeEvents() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed || p.events == nil {
		return
	}
	p.closed = true
	close(p.events)
	if p.done != nil {
		close(p.done)
	}
}

func newRPCClient(transport rpcTransport) *rpcClient {
	return &rpcClient{
		transport: transport,
		pending:   map[string]*pendingRequest{},
		readErr:   make(chan error, 1),
	}
}

func (c *rpcClient) request(ctx context.Context, id string, method string, params any, result any) error {
	pending := &pendingRequest{resp: make(chan JSONRPCResponse, 1)}
	if err := c.register(id, pending); err != nil {
		return err
	}
	defer c.unregister(id)

	if err := c.transport.Write(ctx, JSONRPCRequest{JSONRPC: "2.0", ID: id, Method: method, Params: params}); err != nil {
		return fmt.Errorf("write JSON-RPC request %q failed, %w", method, err)
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-c.readErr:
		return err
	case resp := <-pending.resp:
		if resp.Error != nil {
			return fmt.Errorf("JSON-RPC request %q failed, code=%d message=%q", method, resp.Error.Code, resp.Error.Message)
		}
		if result == nil || len(resp.Result) == 0 {
			return nil
		}
		if err := json.Unmarshal(resp.Result, result); err != nil {
			return fmt.Errorf("decode JSON-RPC response %q failed, %w", method, err)
		}
		return nil
	}
}

func (c *rpcClient) stream(ctx context.Context, id string, method string, params any) (<-chan Event, error) {
	events := make(chan Event, 16)
	pending := &pendingRequest{resp: make(chan JSONRPCResponse, 1), events: events, done: make(chan struct{})}
	if err := c.register(id, pending); err != nil {
		return nil, err
	}
	if err := c.transport.Write(ctx, JSONRPCRequest{JSONRPC: "2.0", ID: id, Method: method, Params: params}); err != nil {
		c.unregister(id)
		close(events)
		return nil, fmt.Errorf("write JSON-RPC stream request %q failed, %w", method, err)
	}
	go func() {
		select {
		case <-ctx.Done():
		case <-pending.done:
			return
		}
		if c.unregister(id) != nil {
			pending.sendEvent(Event{RequestID: id, Type: EventError, Err: ctx.Err().Error()})
			pending.closeEvents()
		}
	}()
	return events, nil
}

func (c *rpcClient) close() error {
	return c.transport.Close()
}

func (c *rpcClient) register(id string, pending *pendingRequest) error {
	if id == "" {
		return errors.New("JSON-RPC request id is empty")
	}
	c.readOnce.Do(func() { go c.readLoop() })
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.pending[id]; ok {
		return fmt.Errorf("duplicate JSON-RPC request id %q", id)
	}
	c.pending[id] = pending
	return nil
}

func (c *rpcClient) unregister(id string) *pendingRequest {
	c.mu.Lock()
	pending := c.pending[id]
	delete(c.pending, id)
	c.mu.Unlock()
	return pending
}

func (c *rpcClient) readLoop() {
	for {
		bs, err := c.transport.Read(context.Background())
		if err != nil {
			c.failAll(fmt.Errorf("read JSON-RPC message failed, %w", err))
			return
		}
		if err := c.handleMessage(bs); err != nil {
			c.failAll(err)
			return
		}
	}
}

func (c *rpcClient) handleMessage(bs []byte) error {
	var envelope struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      string          `json:"id"`
		Method  string          `json:"method"`
		Params  json.RawMessage `json:"params"`
		Result  json.RawMessage `json:"result"`
		Error   *JSONRPCError   `json:"error"`
	}
	if err := json.Unmarshal(bs, &envelope); err != nil {
		return fmt.Errorf("decode JSON-RPC envelope failed, %w", err)
	}
	if envelope.Method == MethodMessageEvent {
		return c.handleEvent(envelope.Params)
	}
	if envelope.ID == "" {
		return nil
	}
	c.mu.Lock()
	pending := c.pending[envelope.ID]
	c.mu.Unlock()
	if pending == nil {
		return nil
	}
	pending.resp <- JSONRPCResponse{JSONRPC: envelope.JSONRPC, ID: envelope.ID, Result: envelope.Result, Error: envelope.Error}
	return nil
}

func (c *rpcClient) handleEvent(raw json.RawMessage) error {
	var params MessageEventParams
	if err := json.Unmarshal(raw, &params); err != nil {
		return fmt.Errorf("decode ACP message event failed, %w", err)
	}
	c.mu.Lock()
	pending := c.pending[params.RequestID]
	c.mu.Unlock()
	if pending == nil || pending.events == nil {
		return nil
	}
	event := Event{RequestID: params.RequestID, Type: EventType(params.Event), Delta: params.Delta, Final: params.Final, Err: params.Error}
	pending.sendEvent(event)
	if event.Type == EventFinish || event.Type == EventError {
		c.unregister(params.RequestID)
		pending.closeEvents()
	}
	return nil
}

func (c *rpcClient) failAll(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for id, pending := range c.pending {
		select {
		case pending.resp <- JSONRPCResponse{ID: id, Error: &JSONRPCError{Code: -32000, Message: err.Error()}}:
		default:
		}
		if pending.events != nil {
			pending.sendEvent(Event{RequestID: id, Type: EventError, Err: err.Error()})
			pending.closeEvents()
		}
		delete(c.pending, id)
	}
	select {
	case c.readErr <- err:
	default:
	}
}
