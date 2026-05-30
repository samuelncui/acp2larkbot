package lark

import (
	"context"
	"fmt"
	"sync"

	"github.com/sirupsen/logrus"
)

type FallbackRenderer struct {
	primary  Renderer
	fallback Renderer

	mu      sync.Mutex
	renders map[*RenderHandle]Renderer
}

func NewFallbackRenderer(primary Renderer, fallback Renderer) *FallbackRenderer {
	return &FallbackRenderer{primary: primary, fallback: fallback, renders: map[*RenderHandle]Renderer{}}
}

func (r *FallbackRenderer) Start(ctx context.Context, req StartRenderRequest) (*RenderHandle, error) {
	handle, err := r.primary.Start(ctx, req)
	if err == nil {
		r.remember(handle, r.primary)
		return handle, nil
	}
	logrus.WithError(err).Warn("primary renderer start failed, falling back")
	handle, fallbackErr := r.fallback.Start(ctx, req)
	if fallbackErr != nil {
		return nil, fmt.Errorf("primary renderer start failed: %w; fallback renderer start failed: %w", err, fallbackErr)
	}
	r.remember(handle, r.fallback)
	return handle, nil
}

func (r *FallbackRenderer) Append(ctx context.Context, handle *RenderHandle, delta string) error {
	return r.renderer(handle).Append(ctx, handle, delta)
}

func (r *FallbackRenderer) AppendProcess(ctx context.Context, handle *RenderHandle, delta string) error {
	return r.renderer(handle).AppendProcess(ctx, handle, delta)
}

func (r *FallbackRenderer) Finish(ctx context.Context, handle *RenderHandle, final string) error {
	defer r.forget(handle)
	return r.renderer(handle).Finish(ctx, handle, final)
}

func (r *FallbackRenderer) Fail(ctx context.Context, handle *RenderHandle, err error) error {
	defer r.forget(handle)
	return r.renderer(handle).Fail(ctx, handle, err)
}

func (r *FallbackRenderer) remember(handle *RenderHandle, renderer Renderer) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.renders[handle] = renderer
}

func (r *FallbackRenderer) forget(handle *RenderHandle) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.renders, handle)
}

func (r *FallbackRenderer) renderer(handle *RenderHandle) Renderer {
	r.mu.Lock()
	defer r.mu.Unlock()
	if renderer := r.renders[handle]; renderer != nil {
		return renderer
	}
	return r.primary
}
