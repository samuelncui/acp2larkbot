package acp

import (
	"context"
	"encoding/json"
	"io"
	"testing"
)

type memoryTransport struct {
	reads  chan []byte
	writes chan JSONRPCRequest
}

func newMemoryTransport() *memoryTransport {
	return &memoryTransport{reads: make(chan []byte, 8), writes: make(chan JSONRPCRequest, 8)}
}

func (t *memoryTransport) Read(ctx context.Context) ([]byte, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case bs, ok := <-t.reads:
		if !ok {
			return nil, io.EOF
		}
		return bs, nil
	}
}

func (t *memoryTransport) Write(ctx context.Context, req JSONRPCRequest) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case t.writes <- req:
		return nil
	}
}

func (t *memoryTransport) Close() error { return nil }

func TestRPCClientDispatchesStreamEventsByRequestID(t *testing.T) {
	transport := newMemoryTransport()
	client := newRPCClient(transport)
	ctx := context.Background()

	events, err := client.stream(ctx, "req-1", MethodMessageSend, SendParams{RequestID: "req-1"})
	if err != nil {
		t.Fatalf("stream failed: %v", err)
	}
	write := <-transport.writes
	if write.ID != "req-1" || write.Method != MethodMessageSend {
		t.Fatalf("unexpected write: %+v", write)
	}

	transport.reads <- mustJSON(t, JSONRPCEvent{JSONRPC: "2.0", Method: MethodMessageEvent, Params: mustRaw(t, MessageEventParams{RequestID: "req-1", Event: string(EventDelta), Delta: "hello"})})
	transport.reads <- mustJSON(t, JSONRPCEvent{JSONRPC: "2.0", Method: MethodMessageEvent, Params: mustRaw(t, MessageEventParams{RequestID: "req-1", Event: string(EventFinish), Final: "done"})})

	first := <-events
	if first.Type != EventDelta || first.Delta != "hello" {
		t.Fatalf("unexpected first event: %+v", first)
	}
	second := <-events
	if second.Type != EventFinish || second.Final != "done" {
		t.Fatalf("unexpected second event: %+v", second)
	}
	if _, ok := <-events; ok {
		t.Fatal("events channel should be closed after finish")
	}
}

func TestSendParamsUsesACPJSONFieldNames(t *testing.T) {
	bs, err := json.Marshal(SendParams{
		RequestID: "req-1",
		SessionID: "sess-1",
		Message:   Message{Type: "text", Content: "hello"},
	})
	if err != nil {
		t.Fatalf("marshal SendParams failed: %v", err)
	}
	want := `{"request_id":"req-1","session_id":"sess-1","message":{"type":"text","content":"hello"}}`
	if string(bs) != want {
		t.Fatalf("SendParams JSON = %s, want %s", bs, want)
	}
}

func mustRaw(t *testing.T, value any) json.RawMessage {
	t.Helper()
	bs, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal raw failed: %v", err)
	}
	return bs
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	bs, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal JSON failed: %v", err)
	}
	return bs
}
