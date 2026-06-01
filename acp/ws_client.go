package acp

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/sirupsen/logrus"

	"github.com/samuelncui/acp2larkbot/config"
)

// WSClient speaks the Agent Client Protocol over websocket,
// using the exact same wire format as CmdClient (stdio).
// This allows websocat to bridge ws ↔ hermes acp seamlessly.
//
// Protocol: JSON-RPC 2.0 with int64 IDs
//
//	initialize → session/new → session/prompt
//	Notifications: session/update (agent_message_chunk, tool_call, tool_call_update)
type WSClient struct {
	cfg config.AgentConfig

	startMu sync.Mutex
	started bool
	conn    *websocket.Conn
	rpc     *rpcClient
	writeMu sync.Mutex

	nextID int64

	mu      sync.Mutex
	pending map[int64]chan acpResponse
	streams map[string]chan Event
	tools   map[string]string
}

func NewWSClient(cfg config.AgentConfig) *WSClient {
	return &WSClient{
		cfg:     cfg,
		pending: map[int64]chan acpResponse{},
		streams: map[string]chan Event{},
		tools:   map[string]string{},
	}
}

func (c *WSClient) OpenSession(ctx context.Context, req OpenSessionRequest) (*Session, error) {
	if err := c.ensureStarted(ctx); err != nil {
		return nil, err
	}
	cwd := req.CWD
	if cwd == "" {
		cwd = c.cfg.CWD
	}
	if cwd == "" {
		return nil, fmt.Errorf("ws ACP open session: cwd is required")
	}
	var result acpNewSessionResult
	if err := c.call(ctx, "session/new", acpNewSessionParams{Cwd: cwd, McpServers: []any{}}, &result); err != nil {
		return nil, err
	}
	if result.SessionID == "" {
		return nil, fmt.Errorf("ws ACP returned empty session id")
	}
	return &Session{ID: result.SessionID}, nil
}

func (c *WSClient) Send(ctx context.Context, req SendRequest) (<-chan Event, error) {
	if err := c.ensureStarted(ctx); err != nil {
		return nil, err
	}
	if req.SessionID == "" {
		return nil, fmt.Errorf("ws ACP send: session id is required")
	}
	events := make(chan Event, 1024)
	c.mu.Lock()
	if _, exists := c.streams[req.SessionID]; exists {
		c.mu.Unlock()
		close(events)
		return nil, fmt.Errorf("ws ACP session %q already has an in-flight prompt", req.SessionID)
	}
	c.streams[req.SessionID] = events
	c.mu.Unlock()

	params := acpPromptParams{
		SessionID: req.SessionID,
		Prompt:    []acpContentBlock{{Type: "text", Text: req.Message.Content}},
	}
	go c.runPrompt(ctx, req, params, events)
	return events, nil
}

func (c *WSClient) runPrompt(ctx context.Context, req SendRequest, params acpPromptParams, events chan<- Event) {
	defer func() {
		c.mu.Lock()
		delete(c.streams, req.SessionID)
		c.mu.Unlock()
		close(events)
	}()
	var result acpPromptResult
	err := c.call(ctx, "session/prompt", params, &result)
	if err != nil {
		events <- Event{RequestID: req.RequestID, Type: EventError, Err: err.Error()}
		return
	}
	events <- Event{RequestID: req.RequestID, Type: EventFinish, Final: ""}
}

func (c *WSClient) CloseSession(ctx context.Context, sessionID string) error {
	// ACP has no mandatory close method; sessions are owned by the agent.
	return nil
}

func (c *WSClient) Close() error {
	c.startMu.Lock()
	defer c.startMu.Unlock()
	if !c.started {
		return nil
	}
	if c.conn != nil {
		_ = c.conn.Close()
	}
	c.started = false
	return nil
}

func (c *WSClient) ensureStarted(ctx context.Context) error {
	c.startMu.Lock()
	defer c.startMu.Unlock()
	if c.started {
		return nil
	}
	if err := c.dial(ctx); err != nil {
		return err
	}
	go c.readLoop()
	if err := c.initialize(ctx); err != nil {
		_ = c.conn.Close()
		c.conn = nil
		return err
	}
	c.started = true
	return nil
}

func (c *WSClient) dial(ctx context.Context) error {
	dialer := websocket.Dialer{
		TLSClientConfig:  c.tlsConfig(),
		HandshakeTimeout: c.cfg.Timeouts.Connect.Duration,
	}
	header := http.Header{}
	if c.cfg.Auth.Token != "" {
		header.Set("Authorization", "Bearer "+c.cfg.Auth.Token)
	}
	conn, _, err := dialer.DialContext(ctx, c.cfg.URL, header)
	if err != nil {
		return fmt.Errorf("dial ws ACP %q failed, %w", c.cfg.URL, err)
	}
	c.conn = conn
	if c.cfg.Heartbeat.Interval.Duration > 0 {
		go c.pingLoop(ctx, conn, c.cfg.Heartbeat.Interval.Duration)
	}
	return nil
}

func (c *WSClient) tlsConfig() *tls.Config {
	tlsCfg := &tls.Config{ServerName: c.cfg.TLS.ServerName, MinVersion: tls.VersionTLS12}
	tlsCfg.InsecureSkipVerify = c.cfg.TLS.InsecureSkipVerify
	if c.cfg.TLS.CAFile == "" {
		return tlsCfg
	}
	bs, err := os.ReadFile(c.cfg.TLS.CAFile)
	if err != nil {
		return tlsCfg
	}
	pool := x509.NewCertPool()
	if pool.AppendCertsFromPEM(bs) {
		tlsCfg.RootCAs = pool
	}
	return tlsCfg
}

func (c *WSClient) initialize(ctx context.Context) error {
	params := acpInitializeRequest{
		ProtocolVersion: 1,
		ClientInfo:      acpImplementation{Name: "acp2larkbot", Version: "0.1.0"},
		ClientCapabilities: acpClientCapabilites{
			FS:       acpFSCapabilites{ReadTextFile: false, WriteTextFile: false},
			Terminal: false,
		},
	}
	var result json.RawMessage
	return c.call(ctx, "initialize", params, &result)
}

func (c *WSClient) call(ctx context.Context, method string, params any, result any) error {
	id := c.nextID
	c.nextID++
	resp := make(chan acpResponse, 1)
	c.mu.Lock()
	c.pending[id] = resp
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
	}()
	if err := c.writeMessage(acpEnvelope{JSONRPC: "2.0", ID: &id, Method: method, Params: mustMarshal(params)}); err != nil {
		return fmt.Errorf("write %s failed, %w", method, err)
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case r := <-resp:
		if r.err != nil {
			return fmt.Errorf("%s failed, code=%d message=%q", method, r.err.Code, r.err.Message)
		}
		if result == nil || len(r.result) == 0 {
			return nil
		}
		if err := json.Unmarshal(r.result, result); err != nil {
			return fmt.Errorf("decode %s result failed, %w", method, err)
		}
		return nil
	}
}

func (c *WSClient) writeMessage(env acpEnvelope) error {
	if env.JSONRPC == "" {
		env.JSONRPC = "2.0"
	}
	bs, err := json.Marshal(env)
	if err != nil {
		return err
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return c.conn.WriteMessage(websocket.TextMessage, append(bs, '\n'))
}

func (c *WSClient) readLoop() {
	for {
		_, line, err := c.conn.ReadMessage()
		if err != nil {
			c.shutdown(err)
			return
		}
		if len(line) == 0 {
			continue
		}
		c.handleLine(line)
	}
}

func (c *WSClient) handleLine(line []byte) {
	var env acpEnvelope
	if err := json.Unmarshal(line, &env); err != nil {
		return
	}
	if env.Method != "" && env.ID == nil {
		c.handleNotification(env)
		return
	}
	if env.Method != "" && env.ID != nil {
		// Server -> client request: reply method-not-found
		c.replyMethodNotFound(env)
		return
	}
	if env.ID == nil {
		return
	}
	c.mu.Lock()
	resp := c.pending[*env.ID]
	delete(c.pending, *env.ID)
	c.mu.Unlock()
	if resp == nil {
		return
	}
	resp <- acpResponse{result: env.Result, err: env.Error}
}

func (c *WSClient) handleNotification(env acpEnvelope) {
	if env.Method != "session/update" {
		return
	}
	var note acpSessionNotification
	if err := json.Unmarshal(env.Params, &note); err != nil {
		return
	}
	c.mu.Lock()
	stream := c.streams[note.SessionID]
	c.mu.Unlock()
	if stream == nil {
		return
	}
	var base acpSessionUpdateBase
	if err := json.Unmarshal(note.Update, &base); err != nil {
		return
	}
	switch base.SessionUpdate {
	case "agent_message_chunk":
		var chunk acpAgentMessageChunk
		if err := json.Unmarshal(note.Update, &chunk); err != nil {
			return
		}
		if chunk.Content.Type == "text" && chunk.Content.Text != "" {
			logrus.WithFields(logrus.Fields{"session_id": note.SessionID, "len": len(chunk.Content.Text), "text": chunk.Content.Text}).Debug("acp message chunk")
			stream <- Event{Type: EventDelta, Delta: chunk.Content.Text}
		}
	case "tool_call", "tool_call_update":
		if text := c.summarizeProcessUpdate(base.SessionUpdate, note.Update); text != "" {
			stream <- Event{Type: EventProcess, Process: text}
		}
	default:
		if text := c.summarizeProcessUpdate(base.SessionUpdate, note.Update); text != "" {
			stream <- Event{Type: EventProcess, Process: text}
			return
		}
		logrus.WithFields(logrus.Fields{"session_id": note.SessionID, "update": base.SessionUpdate}).Debug("acp session update ignored")
	}
}

func (c *WSClient) summarizeProcessUpdate(updateType string, raw json.RawMessage) string {
	var update acpToolCall
	if err := json.Unmarshal(raw, &update); err != nil {
		return summarizeProcessUpdate(updateType, raw)
	}
	if updateType == "tool_call" && update.ToolCallID != "" && strings.TrimSpace(update.Title) != "" {
		c.mu.Lock()
		c.tools[update.ToolCallID] = strings.TrimSpace(update.Title)
		c.mu.Unlock()
	}
	if updateType == "tool_call_update" && strings.TrimSpace(update.Title) == "" && update.ToolCallID != "" {
		c.mu.Lock()
		update.Title = c.tools[update.ToolCallID]
		c.mu.Unlock()
		raw = mustMarshal(update)
	}
	return summarizeProcessUpdate(updateType, raw)
}

func (c *WSClient) replyMethodNotFound(env acpEnvelope) {
	resp := acpEnvelope{
		JSONRPC: "2.0",
		ID:      env.ID,
		Error:   &JSONRPCError{Code: -32601, Message: "method not found"},
	}
	_ = c.writeMessage(resp)
}

func (c *WSClient) shutdown(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for id, resp := range c.pending {
		select {
		case resp <- acpResponse{err: &JSONRPCError{Code: -32000, Message: err.Error()}}:
		default:
		}
		delete(c.pending, id)
	}
	for _, stream := range c.streams {
		stream <- Event{Type: EventError, Err: err.Error()}
		close(stream)
	}
	c.streams = map[string]chan Event{}
}

func (c *WSClient) pingLoop(ctx context.Context, conn *websocket.Conn, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(5*time.Second))
		}
	}
}

// unused import guard
var _ = logrus.WithField
