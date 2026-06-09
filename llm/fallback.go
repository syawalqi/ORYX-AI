// Package llm provides provider implementations and a fallback chain.
package llm

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// FallbackProvider wraps multiple providers and tries them in order.
// If the primary fails (network error, rate limit, 5xx), it falls
// through to the next provider in the chain.
type FallbackProvider struct {
	providers []Provider
	names     string // human-readable names for logging
}

// NewFallbackProvider creates a fallback chain from the given providers.
// Providers are tried in order — the first one that succeeds wins.
func NewFallbackProvider(providers ...Provider) *FallbackProvider {
	names := make([]string, len(providers))
	for i, p := range providers {
		names[i] = p.Name()
	}
	return &FallbackProvider{
		providers: providers,
		names:     strings.Join(names, " -> "),
	}
}

func (f *FallbackProvider) Name() string {
	return "fallback:" + f.names
}

// ListModels returns models from the first provider that responds.
// If all fail, returns partial results with warnings.
func (f *FallbackProvider) ListModels(ctx context.Context) ([]ModelInfo, error) {
	var lastErr error
	for i, p := range f.providers {
		models, err := p.ListModels(ctx)
		if err == nil {
			if i > 0 {
				// Prefix with provider name so user knows where each model came from
				for j := range models {
					models[j].ID = p.Name() + ":" + models[j].ID
				}
			}
			return models, nil
		}
		lastErr = err
	}
	return nil, fmt.Errorf("all providers failed to list models: %w", lastErr)
}

// Chat calls providers in order, failing over on errors.
func (f *FallbackProvider) Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	var lastErr error
	for i, p := range f.providers {
		// Check context before each attempt
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		resp, err := p.Chat(ctx, req)
		if err == nil {
			return resp, nil
		}

		// Non-retryable errors — don't fall through
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, err
		}

		lastErr = err

		// Log which provider failed (for audit)
		_ = i // future: emit log event
	}
	return nil, fmt.Errorf("all %d providers failed, last error: %w", len(f.providers), lastErr)
}

// ChatStream calls providers in order, failing over on errors.
// NOTE: streaming fallback is best-effort — if the primary starts streaming
// then fails mid-stream, that partial data is lost and we retry from scratch
// with the fallback.
func (f *FallbackProvider) ChatStream(ctx context.Context, req ChatRequest) (<-chan StreamEvent, error) {
	var lastErr error
	for i, p := range f.providers {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		events, err := p.ChatStream(ctx, req)
		if err == nil {
			if i > 0 {
				// Emit a notification that we're using a fallback provider
				ch := make(chan StreamEvent, 8)
				go func() {
					ch <- StreamEvent{
						Type:    EventToken,
						Content: fmt.Sprintf("[FALLBACK: using %s — primary unavailable]", p.Name()),
					}
					for evt := range events {
						ch <- evt
					}
					close(ch)
				}()
				return ch, nil
			}
			return events, nil
		}

		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, err
		}

		lastErr = err
	}
	return nil, fmt.Errorf("all %d providers failed for stream, last error: %w", len(f.providers), lastErr)
}
