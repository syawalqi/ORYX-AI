package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// AnthropicProvider implements the Provider interface for Anthropic's Messages API.
type AnthropicProvider struct {
	apiKey  string
	client  *http.Client
}

func NewAnthropicProvider(apiKey string) *AnthropicProvider {
	return &AnthropicProvider{
		apiKey: apiKey,
		client: &http.Client{},
	}
}

func (p *AnthropicProvider) Name() string { return "anthropic" }

type anthropicReq struct {
	Model       string      `json:"model"`
	Messages    []anthropicMsg `json:"messages"`
	System      string      `json:"system,omitempty"`
	MaxTokens   int         `json:"max_tokens"`
	Temperature float64     `json:"temperature,omitempty"`
	Tools       []ToolDef   `json:"tools,omitempty"`
	Stream      bool        `json:"stream"`
}

type anthropicMsg struct {
	Role    string         `json:"role"`
	Content []anthropicContent `json:"content"`
}

type anthropicContent struct {
	Type  string `json:"type"`
	Text  string `json:"text,omitempty"`
	ID    string `json:"id,omitempty"`
	Name  string `json:"name,omitempty"`
	Input any    `json:"input,omitempty"`
}

type anthropicResp struct {
	Content      []anthropicRespContent `json:"content"`
	StopReason   string                 `json:"stop_reason"`
	Usage        Usage                  `json:"usage"`
	Error        *struct{ Message string } `json:"error,omitempty"`
}

type anthropicRespContent struct {
	Type  string `json:"type"`
	Text  string `json:"text,omitempty"`
	ID    string `json:"id,omitempty"`
	Name  string `json:"name,omitempty"`
	Input any    `json:"input,omitempty"`
}

func convertToAnthropic(msgs []Message, systemPrompt string) (string, []anthropicMsg) {
	system := systemPrompt
	var apiMsgs []anthropicMsg
	for _, m := range msgs {
		if m.Role == RoleSystem {
			if system == "" {
				system = m.Content
			}
			continue
		}
		role := string(m.Role)
		if role == "tool" {
			role = "user" // Anthropic uses user role for tool results
		}

		content := []anthropicContent{{Type: "text", Text: m.Content}}

		// Handle tool use in assistant messages
		if len(m.ToolCalls) > 0 {
			for _, tc := range m.ToolCalls {
				var args any
				json.Unmarshal([]byte(tc.Function.Arguments), &args)
				content = append(content, anthropicContent{
					Type:  "tool_use",
					ID:    tc.ID,
					Name:  tc.Function.Name,
					Input: args,
				})
			}
		}

		apiMsgs = append(apiMsgs, anthropicMsg{Role: role, Content: content})
	}
	return system, apiMsgs
}

func convertFromAnthropic(resp *anthropicResp) (*ChatResponse, error) {
	var text string
	var toolCalls []ToolCall

	for _, c := range resp.Content {
		switch c.Type {
		case "text":
			text += c.Text
		case "tool_use":
			inputBytes, _ := json.Marshal(c.Input)
			toolCalls = append(toolCalls, ToolCall{
				ID:   c.ID,
				Type: "function",
				Function: ToolFunction{
					Name:      c.Name,
					Arguments: string(inputBytes),
				},
			})
		}
	}

	return &ChatResponse{
		Content:   text,
		ToolCalls: toolCalls,
		Usage:     resp.Usage,
	}, nil
}

func (p *AnthropicProvider) Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	system, apiMsgs := convertToAnthropic(req.Messages, "")

	apiReq := anthropicReq{
		Model:       req.Model,
		Messages:    apiMsgs,
		MaxTokens:   req.MaxTokens,
		Temperature: req.Temperature,
		Tools:       req.Tools,
		Stream:      false,
	}
	if system != "" {
		apiReq.System = system
	}

	data, _ := json.Marshal(apiReq)
	httpReq, _ := http.NewRequestWithContext(ctx, "POST",
		"https://api.anthropic.com/v1/messages",
		bytes.NewReader(data))
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", p.apiKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("anthropic request: %w", err)
	}
	defer resp.Body.Close()

	var apiResp anthropicResp
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, fmt.Errorf("anthropic decode: %w", err)
	}
	if apiResp.Error != nil {
		return nil, fmt.Errorf("anthropic error: %s", apiResp.Error.Message)
	}

	return convertFromAnthropic(&apiResp)
}

func (p *AnthropicProvider) ChatStream(ctx context.Context, req ChatRequest) (<-chan StreamEvent, error) {
	// For now, fall back to non-streaming for Anthropic
	// Full SSE streaming requires parsing Anthropic's event stream format
	resp, err := p.Chat(ctx, req)
	if err != nil {
		return nil, err
	}
	ch := make(chan StreamEvent, 4)
	go func() {
		defer close(ch)
		if resp.Content != "" {
			ch <- StreamEvent{Type: EventToken, Content: resp.Content}
		}
		for _, tc := range resp.ToolCalls {
			ch <- StreamEvent{Type: EventToolCall, ToolCallDelta: &ToolCall{
				ID:   tc.ID,
				Type: "function",
				Function: ToolFunction{
					Name:      tc.Function.Name,
					Arguments: tc.Function.Arguments,
				},
			}}
		}
		ch <- StreamEvent{Type: EventDone}
	}()
	return ch, nil
}

func (p *AnthropicProvider) ListModels(ctx context.Context) ([]ModelInfo, error) {
	return []ModelInfo{
		{ID: "claude-sonnet-4-20250514", Name: "Claude Sonnet 4"},
		{ID: "claude-opus-4-20250514", Name: "Claude Opus 4"},
		{ID: "claude-haiku-3-5-20241022", Name: "Claude Haiku 3.5"},
	}, nil
}
