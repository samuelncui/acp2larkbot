package acp

import "encoding/json"

type JSONRPCRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      string `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type JSONRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      string          `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *JSONRPCError   `json:"error,omitempty"`
}

type JSONRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

type JSONRPCEvent struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type MessageEventParams struct {
	RequestID string `json:"request_id"`
	Event     string `json:"event"`
	Delta     string `json:"delta,omitempty"`
	Final     string `json:"final,omitempty"`
	Error     string `json:"error,omitempty"`
}

type OpenSessionParams struct {
	RequestID string            `json:"request_id"`
	Metadata  map[string]string `json:"metadata,omitempty"`
}

type OpenSessionResult struct {
	SessionID string `json:"session_id"`
}

type SendParams struct {
	RequestID string  `json:"request_id"`
	SessionID string  `json:"session_id"`
	Message   Message `json:"message"`
}

type CloseSessionParams struct {
	RequestID string `json:"request_id"`
	SessionID string `json:"session_id"`
}

type InitializeParams struct {
	Binding string   `json:"binding"`
	Methods []string `json:"methods"`
	Events  []string `json:"events"`
}

type InitializeResult struct {
	Binding         string   `json:"binding"`
	RequestIDEcho   bool     `json:"request_id_echo"`
	Multiplex       bool     `json:"multiplex"`
	Methods         []string `json:"methods"`
	Events          []string `json:"events"`
	SessionRecovery bool     `json:"session_recovery"`
	ApplicationPing bool     `json:"application_ping"`
}
