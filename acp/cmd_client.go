package acp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/sirupsen/logrus"

	"github.com/samuelncui/acp2larkbot/config"
)

// CmdClient speaks the real Agent Client Protocol (https://agentclientprotocol.com)
// over the agent process' stdio. Each line on stdin/stdout is a JSON-RPC 2.0 message.
// Implemented surface for acp2larkbot:
//   - initialize        (handshake)
//   - session/new       (open)
//   - session/prompt    (send + stream via session/update)
type CmdClient struct {
	cfg config.AgentConfig

	startMu sync.Mutex
	started bool
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	scanner *bufio.Scanner
	writeMu sync.Mutex

	nextID atomic.Int64

	mu      sync.Mutex
	pending map[int64]chan acpResponse
	streams map[string]chan Event
	tools   map[string]string
	dead    chan struct{}
	deadErr error
}

type acpResponse struct {
	result json.RawMessage
	err    *JSONRPCError
}

type acpEnvelope struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *int64          `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *JSONRPCError   `json:"error,omitempty"`
}

type acpInitializeRequest struct {
	ProtocolVersion    int                  `json:"protocolVersion"`
	ClientInfo         acpImplementation    `json:"clientInfo"`
	ClientCapabilities acpClientCapabilites `json:"clientCapabilities"`
}

type acpImplementation struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type acpClientCapabilites struct {
	FS       acpFSCapabilites `json:"fs"`
	Terminal bool             `json:"terminal"`
}

type acpFSCapabilites struct {
	ReadTextFile  bool `json:"readTextFile"`
	WriteTextFile bool `json:"writeTextFile"`
}

type acpNewSessionParams struct {
	Cwd        string `json:"cwd"`
	McpServers []any  `json:"mcpServers"`
}

type acpNewSessionResult struct {
	SessionID string `json:"sessionId"`
}

type acpPromptParams struct {
	SessionID string            `json:"sessionId"`
	Prompt    []acpContentBlock `json:"prompt"`
}

type acpContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

type acpPromptResult struct {
	StopReason string `json:"stopReason"`
}

type acpSessionNotification struct {
	SessionID string          `json:"sessionId"`
	Update    json.RawMessage `json:"update"`
}

type acpSessionUpdateBase struct {
	SessionUpdate string `json:"sessionUpdate"`
}

type acpAgentMessageChunk struct {
	SessionUpdate string          `json:"sessionUpdate"`
	Content       acpContentBlock `json:"content"`
}

type acpToolCall struct {
	SessionUpdate string           `json:"sessionUpdate"`
	Title         string           `json:"title"`
	Status        string           `json:"status"`
	ToolCallID    string           `json:"toolCallId"`
	RawInput      acpToolRawInput  `json:"rawInput"`
	RawOutput     acpToolRawOutput `json:"rawOutput"`
	Content       []acpToolContent `json:"content"`
}

type acpToolRawInput struct {
	Command     string `json:"Command"`
	Description string `json:"Description"`
}

type acpToolRawOutput struct {
	Output acpToolOutput `json:"Output"`
}

type acpToolOutput struct {
	Stdout string `json:"stdout"`
	Stderr string `json:"stderr"`
}

type acpToolContent struct {
	Type    string              `json:"type"`
	Content acpToolContentBlock `json:"content"`
}

type acpToolContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

func NewCmdClient(cfg config.AgentConfig) *CmdClient {
	return &CmdClient{
		cfg:     cfg,
		pending: map[int64]chan acpResponse{},
		streams: map[string]chan Event{},
		tools:   map[string]string{},
		dead:    make(chan struct{}),
	}
}

func (c *CmdClient) OpenSession(ctx context.Context, req OpenSessionRequest) (*Session, error) {
	if err := c.ensureStarted(ctx); err != nil {
		return nil, err
	}
	cwd := req.CWD
	if cwd == "" {
		cwd = c.cfg.CWD
	}
	if cwd == "" {
		return nil, fmt.Errorf("cmd ACP open session: cwd is required")
	}
	var result acpNewSessionResult
	if err := c.call(ctx, "session/new", acpNewSessionParams{Cwd: cwd, McpServers: []any{}}, &result); err != nil {
		return nil, err
	}
	if result.SessionID == "" {
		return nil, fmt.Errorf("cmd ACP returned empty session id")
	}
	return &Session{ID: result.SessionID}, nil
}

func (c *CmdClient) Send(ctx context.Context, req SendRequest) (<-chan Event, error) {
	if err := c.ensureStarted(ctx); err != nil {
		return nil, err
	}
	if req.SessionID == "" {
		return nil, fmt.Errorf("cmd ACP send: session id is required")
	}
	events := make(chan Event, 1024)
	c.mu.Lock()
	if _, exists := c.streams[req.SessionID]; exists {
		c.mu.Unlock()
		close(events)
		return nil, fmt.Errorf("cmd ACP session %q already has an in-flight prompt", req.SessionID)
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

func (c *CmdClient) runPrompt(ctx context.Context, req SendRequest, params acpPromptParams, events chan<- Event) {
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

func (c *CmdClient) CloseSession(ctx context.Context, sessionID string) error {
	// Real ACP has no mandatory close method; sessions are owned by the agent.
	return nil
}

func (c *CmdClient) Close() error {
	c.startMu.Lock()
	defer c.startMu.Unlock()
	if !c.started {
		return nil
	}
	c.shutdown(errors.New("cmd ACP client closed"))
	if c.stdin != nil {
		_ = c.stdin.Close()
	}
	if c.cmd != nil && c.cmd.Process != nil {
		_ = c.cmd.Process.Kill()
		_ = c.cmd.Wait()
	}
	c.started = false
	return nil
}

func (c *CmdClient) ensureStarted(ctx context.Context) error {
	c.startMu.Lock()
	defer c.startMu.Unlock()
	if c.started {
		return nil
	}
	if err := c.spawn(ctx); err != nil {
		return err
	}
	go c.readLoop()
	if err := c.initialize(ctx); err != nil {
		c.shutdown(err)
		return err
	}
	c.started = true
	return nil
}

func (c *CmdClient) spawn(ctx context.Context) error {
	if _, err := exec.LookPath(c.cfg.Command); err != nil {
		return fmt.Errorf("find cmd ACP command %q failed, %w", c.cfg.Command, err)
	}
	cmd := exec.Command(c.cfg.Command, c.cfg.Args...)
	cmd.Dir = c.cfg.CWD
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("open cmd ACP stdin failed, %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("open cmd ACP stdout failed, %w", err)
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start cmd ACP process failed, %w", err)
	}
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	c.cmd = cmd
	c.stdin = stdin
	c.scanner = scanner
	return nil
}

func (c *CmdClient) initialize(ctx context.Context) error {
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

func (c *CmdClient) call(ctx context.Context, method string, params any, result any) error {
	id := c.nextID.Add(1)
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
	case <-c.dead:
		if c.deadErr != nil {
			return c.deadErr
		}
		return errors.New("cmd ACP transport closed")
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

func (c *CmdClient) writeMessage(env acpEnvelope) error {
	if env.JSONRPC == "" {
		env.JSONRPC = "2.0"
	}
	bs, err := json.Marshal(env)
	if err != nil {
		return err
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if _, err := c.stdin.Write(append(bs, '\n')); err != nil {
		return err
	}
	return nil
}

func (c *CmdClient) readLoop() {
	for c.scanner.Scan() {
		line := append([]byte(nil), c.scanner.Bytes()...)
		if len(line) == 0 {
			continue
		}
		c.handleLine(line)
	}
	err := c.scanner.Err()
	if err == nil {
		err = io.EOF
	}
	c.shutdown(err)
}

func (c *CmdClient) handleLine(line []byte) {
	var env acpEnvelope
	if err := json.Unmarshal(line, &env); err != nil {
		return
	}
	if env.Method != "" && env.ID == nil {
		c.handleNotification(env)
		return
	}
	if env.Method != "" && env.ID != nil {
		// Server -> client request (e.g. fs/read_text_file). Reply method-not-found
		// since acp2larkbot does not advertise these capabilities.
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

func (c *CmdClient) handleNotification(env acpEnvelope) {
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
			logrus.WithFields(logrus.Fields{"session_id": note.SessionID, "len": len(chunk.Content.Text)}).Debug("acp message chunk")
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

func (c *CmdClient) summarizeProcessUpdate(updateType string, raw json.RawMessage) string {
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

func summarizeProcessUpdate(updateType string, raw json.RawMessage) string {
	var update acpToolCall
	if err := json.Unmarshal(raw, &update); err != nil {
		return ""
	}
	name := strings.TrimSpace(update.Title)
	if name == "" {
		name = strings.TrimSpace(updateType)
	}
	switch updateType {
	case "tool_call":
		command := strings.TrimSpace(update.RawInput.Command)
		if command == "" {
			command = strings.TrimSpace(update.RawInput.Description)
		}
		if command == "" {
			return fmt.Sprintf("\n\n[Process] Starting %s", name)
		}
		return fmt.Sprintf("\n\n[Process] Starting %s: %s", name, command)
	case "tool_call_update":
		output := strings.TrimSpace(update.RawOutput.Output.Stdout)
		if output == "" {
			output = strings.TrimSpace(update.RawOutput.Output.Stderr)
		}
		if output == "" {
			output = strings.TrimSpace(toolContentText(update.Content))
		}
		if output == "" {
			return fmt.Sprintf("\n[Process] %s completed", name)
		}
		return fmt.Sprintf("\n[Process] %s output:\n%s", name, trimProcessOutput(output))
	default:
		text := strings.TrimSpace(toolContentText(update.Content))
		if text == "" {
			return ""
		}
		return fmt.Sprintf("\n[Process:%s] %s", updateType, trimProcessOutput(text))
	}
}

func toolContentText(items []acpToolContent) string {
	parts := make([]string, 0, len(items))
	for _, item := range items {
		if item.Type != "content" || item.Content.Type != "text" {
			continue
		}
		text := strings.TrimSpace(item.Content.Text)
		if text == "" {
			continue
		}
		parts = append(parts, text)
	}
	return strings.Join(parts, "\n")
}

func trimProcessOutput(text string) string {
	const maxRunes = 500
	runes := []rune(text)
	if len(runes) <= maxRunes {
		return text
	}
	return string(runes[:maxRunes]) + "\n...[truncated]"
}

func (c *CmdClient) replyMethodNotFound(env acpEnvelope) {
	_ = c.writeMessage(acpEnvelope{
		JSONRPC: "2.0",
		ID:      env.ID,
		Error:   &JSONRPCError{Code: -32601, Message: "method not found"},
	})
}

func (c *CmdClient) shutdown(err error) {
	c.mu.Lock()
	select {
	case <-c.dead:
		c.mu.Unlock()
		return
	default:
	}
	c.deadErr = err
	close(c.dead)
	pending := c.pending
	streams := c.streams
	c.pending = map[int64]chan acpResponse{}
	c.streams = map[string]chan Event{}
	c.tools = map[string]string{}
	c.mu.Unlock()
	for id, ch := range pending {
		ch <- acpResponse{err: &JSONRPCError{Code: -32000, Message: err.Error()}}
		_ = id
	}
	for _, ch := range streams {
		select {
		case ch <- Event{Type: EventError, Err: err.Error()}:
		default:
		}
	}
}

func mustMarshal(v any) json.RawMessage {
	bs, err := json.Marshal(v)
	if err != nil {
		return json.RawMessage(`null`)
	}
	return bs
}
