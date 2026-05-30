package acp

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/samuelncui/acp2larkbot/config"
)

const methodInitialize = "initialize"

type WSClient struct {
	cfg         config.AgentConfig
	rpc         *rpcClient
	initialized bool
	mu          sync.Mutex
}

type wsTransport struct {
	conn *websocket.Conn
	mu   sync.Mutex
}

func NewWSClient(cfg config.AgentConfig) *WSClient {
	return &WSClient{cfg: cfg}
}

func (c *WSClient) OpenSession(ctx context.Context, req OpenSessionRequest) (*Session, error) {
	rpc, err := c.ensureRPC(ctx)
	if err != nil {
		return nil, err
	}
	var result OpenSessionResult
	params := OpenSessionParams{RequestID: req.RequestID, Metadata: req.Metadata}
	if err := rpc.request(ctx, req.RequestID, MethodSessionOpen, params, &result); err != nil {
		return nil, err
	}
	if result.SessionID == "" {
		return nil, fmt.Errorf("websocket ACP returned empty session id")
	}
	return &Session{ID: result.SessionID}, nil
}

func (c *WSClient) Send(ctx context.Context, req SendRequest) (<-chan Event, error) {
	rpc, err := c.ensureRPC(ctx)
	if err != nil {
		return nil, err
	}
	return rpc.stream(ctx, req.RequestID, MethodMessageSend, SendParams{
		RequestID: req.RequestID,
		SessionID: req.SessionID,
		Message:   req.Message,
	})
}

func (c *WSClient) CloseSession(ctx context.Context, sessionID string) error {
	rpc, err := c.ensureRPC(ctx)
	if err != nil {
		return err
	}
	requestID := "close-" + sessionID
	return rpc.request(ctx, requestID, MethodSessionClose, CloseSessionParams{RequestID: requestID, SessionID: sessionID}, nil)
}

func (c *WSClient) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.rpc == nil {
		return nil
	}
	err := c.rpc.close()
	c.rpc = nil
	c.initialized = false
	return err
}

func (c *WSClient) ensureRPC(ctx context.Context) (*rpcClient, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.rpc != nil && c.initialized {
		return c.rpc, nil
	}
	transport, err := c.dial(ctx)
	if err != nil {
		return nil, err
	}
	c.rpc = newRPCClient(transport)
	if err := c.initialize(ctx, c.rpc); err != nil {
		_ = c.rpc.close()
		c.rpc = nil
		return nil, err
	}
	c.initialized = true
	return c.rpc, nil
}

func (c *WSClient) dial(ctx context.Context) (*wsTransport, error) {
	dialer := websocket.Dialer{TLSClientConfig: c.tlsConfig(), HandshakeTimeout: c.cfg.Timeouts.Connect.Duration}
	header := http.Header{}
	if c.cfg.Auth.Token != "" {
		header.Set("Authorization", "Bearer "+c.cfg.Auth.Token)
	}
	conn, _, err := dialer.DialContext(ctx, c.cfg.URL, header)
	if err != nil {
		return nil, fmt.Errorf("dial ACP websocket %q failed, %w", c.cfg.URL, err)
	}
	if c.cfg.Heartbeat.Interval.Duration > 0 {
		go pingLoop(ctx, conn, c.cfg.Heartbeat.Interval.Duration)
	}
	return &wsTransport{conn: conn}, nil
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

func (c *WSClient) initialize(ctx context.Context, rpc *rpcClient) error {
	params := InitializeParams{
		Binding: "acp.websocket",
		Methods: []string{MethodSessionOpen, MethodMessageSend, MethodSessionClose},
		Events:  []string{string(EventDelta), string(EventFinish), string(EventError)},
	}
	var result InitializeResult
	if err := rpc.request(ctx, "initialize", methodInitialize, params, &result); err != nil {
		return err
	}
	if result.Binding != "acp.websocket" {
		return fmt.Errorf("ACP websocket binding mismatch: %q", result.Binding)
	}
	if !result.RequestIDEcho {
		return fmt.Errorf("ACP websocket peer does not echo request_id")
	}
	if c.cfg.Protocol.MaxInFlight > 1 && !result.Multiplex {
		return fmt.Errorf("ACP websocket peer does not support multiplex max_inflight=%d", c.cfg.Protocol.MaxInFlight)
	}
	for _, method := range []string{MethodSessionOpen, MethodMessageSend, MethodSessionClose} {
		if !contains(result.Methods, method) {
			return fmt.Errorf("ACP websocket peer does not support method %q", method)
		}
	}
	for _, event := range []string{string(EventDelta), string(EventFinish), string(EventError)} {
		if !contains(result.Events, event) {
			return fmt.Errorf("ACP websocket peer does not support event %q", event)
		}
	}
	return nil
}

func (t *wsTransport) Read(ctx context.Context) ([]byte, error) {
	type result struct {
		bs  []byte
		err error
	}
	done := make(chan result, 1)
	go func() {
		_, bs, err := t.conn.ReadMessage()
		done <- result{bs: bs, err: err}
	}()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case result := <-done:
		return result.bs, result.err
	}
}

func (t *wsTransport) Write(ctx context.Context, req JSONRPCRequest) error {
	bs, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal websocket ACP request failed, %w", err)
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.conn.WriteMessage(websocket.TextMessage, bs)
}

func (t *wsTransport) Close() error {
	return t.conn.Close()
}

func pingLoop(ctx context.Context, conn *websocket.Conn, interval time.Duration) {
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

func contains(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}
