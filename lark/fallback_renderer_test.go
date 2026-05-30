package lark

import (
	"context"
	"errors"
	"testing"
)

func TestFallbackRendererUsesFallbackWhenPrimaryStartFails(t *testing.T) {
	primary := &recordingRenderer{startErr: errors.New("cardkit unavailable")}
	fallback := &recordingRenderer{}
	r := NewFallbackRenderer(primary, fallback)

	handle, err := r.Start(context.Background(), StartRenderRequest{ChatID: "oc_test"})
	if err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	if fallback.starts != 1 {
		t.Fatalf("fallback starts = %d, want 1", fallback.starts)
	}
	if err := r.Append(context.Background(), handle, "hello"); err != nil {
		t.Fatalf("Append returned error: %v", err)
	}
	if fallback.appends != 1 {
		t.Fatalf("fallback appends = %d, want 1", fallback.appends)
	}
	if err := r.Finish(context.Background(), handle, "done"); err != nil {
		t.Fatalf("Finish returned error: %v", err)
	}
	if fallback.finishes != 1 {
		t.Fatalf("fallback finishes = %d, want 1", fallback.finishes)
	}
}

type recordingRenderer struct {
	startErr error
	starts   int
	appends  int
	finishes int
}

func (r *recordingRenderer) Start(ctx context.Context, req StartRenderRequest) (*RenderHandle, error) {
	r.starts++
	if r.startErr != nil {
		return nil, r.startErr
	}
	return &RenderHandle{MessageID: "msg_test"}, nil
}

func (r *recordingRenderer) Append(ctx context.Context, handle *RenderHandle, delta string) error {
	r.appends++
	return nil
}

func (r *recordingRenderer) AppendProcess(ctx context.Context, handle *RenderHandle, delta string) error {
	return nil
}

func (r *recordingRenderer) Finish(ctx context.Context, handle *RenderHandle, final string) error {
	r.finishes++
	return nil
}

func (r *recordingRenderer) Fail(ctx context.Context, handle *RenderHandle, err error) error {
	return nil
}
