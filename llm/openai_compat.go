package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// OpenAICompatibleProvider implements the Provider interface for any
// OpenAI-compatible API (OpenRouter, DeepSeek, OpenAI, etc.).
type OpenAICompatibleProvider struct {
	apiKey  string
	baseURL string
	client  *http.Client
	name    string
}

func NewOpenAICompatibleProvider(name, apiKey, baseURL string) *OpenAICompatibleProvider {
	return &OpenAICompatibleProvider{
		apiKey:  apiKey,
		baseURL: strings.TrimRight(baseURL, "/"),
		client:  &http.Client{},
		name:    name,
	}
}

func (p *OpenAICompatibleProvider) Name() string { return p.name }

type openaiReq struct {
	Model       string              `json:"model"`
	Messages    []openaiMsg         `json:"messages"`
	Tools       []ToolDef           `json:"tools,omitempty"`
	MaxTokens   int                 `json:"max_tokens,omitempty"`
	Temperature float64             `json:"temperature,omitempty"`
	Stream      bool                `json:"stream"`
	StreamOptions *streamOptions    `json:"stream_options,omitempty"`
}

type streamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

type openaiMsg struct {
	Role       string     `json:"role"`
	Content    string     `json:"content"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
}

type openaiChoice struct {
	Delta struct {
		Content      string     `json:"content,omitempty"`
		Reasoning    string     `json:"reasoning,omitempty"`
		ToolCalls    []ToolCall `json:"tool_calls,omitempty"`
	} `json:"delta"`
	FinishReason string `json:"finish_reason"`
}

type openaiStreamResp struct {
	Choices []openaiChoice `json:"choices"`
	Usage   *Usage         `json:"usage,omitempty"`
	Error   *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error,omitempty"`
}

func (p *OpenAICompatibleProvider) Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	body := p.buildReq(req, false)
	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.baseURL+"/chat/completions", bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("http: %w", err)
	}
	defer resp.Body.Close()

	respData, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("API error %d: %s", resp.StatusCode, string(respData))
	}

	var result struct {
		Choices []struct {
			Message struct {
				Content   string     `json:"content"`
				ToolCalls []ToolCall `json:"tool_calls"`
			} `json:"message"`
		} `json:"choices"`
		Usage Usage `json:"usage"`
	}
	if err := json.Unmarshal(respData, &result); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	if len(result.Choices) == 0 {
		return nil, fmt.Errorf("no choices in response")
	}

	return &ChatResponse{
		Content:   result.Choices[0].Message.Content,
		ToolCalls: result.Choices[0].Message.ToolCalls,
		Usage:     result.Usage,
	}, nil
}

func (p *OpenAICompatibleProvider) ChatStream(ctx context.Context, req ChatRequest) (<-chan StreamEvent, error) {
	body := p.buildReq(req, true)
	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.baseURL+"/chat/completions", bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
	httpReq.Header.Set("Accept", "text/event-stream")

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("http: %w", err)
	}

	if resp.StatusCode >= 400 {
		resp.Body.Close()
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API error %d: %s", resp.StatusCode, string(bodyBytes))
	}

	ch := make(chan StreamEvent, 64)
	go p.readStream(resp.Body, ch)
	return ch, nil
}

func (p *OpenAICompatibleProvider) buildReq(req ChatRequest, stream bool) openaiReq {
	msgs := make([]openaiMsg, len(req.Messages))
	for i, m := range req.Messages {
		msgs[i] = openaiMsg{
			Role:       string(m.Role),
			Content:    m.Content,
			ToolCallID: m.ToolCallID,
			ToolCalls:  m.ToolCalls,
		}
		if len(m.ToolCalls) > 0 {
			msgs[i].ToolCalls = m.ToolCalls
		}
	}

	r := openaiReq{
		Model:       req.Model,
		Messages:    msgs,
		MaxTokens:   req.MaxTokens,
		Temperature: req.Temperature,
		Stream:      stream,
	}

	if len(req.Tools) > 0 {
		r.Tools = req.Tools
	}

	if stream {
		r.StreamOptions = &streamOptions{IncludeUsage: true}
	}

	return r
}

func (p *OpenAICompatibleProvider) readStream(body io.ReadCloser, ch chan<- StreamEvent) {
	defer body.Close()
	defer close(ch)

	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), 256*1024)

	seenUsage := false

	for scanner.Scan() {
		line := scanner.Text()

		if line == "" {
			continue
		}

		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			continue
		}

		var evt openaiStreamResp
		if err := json.Unmarshal([]byte(data), &evt); err != nil {
			continue
		}

		if evt.Error != nil {
			ch <- StreamEvent{Type: EventError, Error: fmt.Errorf("API: %s: %s", evt.Error.Type, evt.Error.Message)}
			return
		}

		if evt.Usage != nil && !seenUsage {
			seenUsage = true
			ch <- StreamEvent{Type: EventDone, Usage: evt.Usage}
		}

		for _, choice := range evt.Choices {
			if choice.Delta.Content != "" {
				ch <- StreamEvent{Type: EventToken, Content: choice.Delta.Content}
			}
			if choice.Delta.Reasoning != "" {
				ch <- StreamEvent{Type: EventReasoning, Content: choice.Delta.Reasoning}
			}
			if len(choice.Delta.ToolCalls) > 0 {
				for _, tc := range choice.Delta.ToolCalls {
					call := tc
					ch <- StreamEvent{Type: EventToolCall, ToolCallDelta: &call}
				}
			}
			if choice.FinishReason == "stop" || choice.FinishReason == "tool_calls" {
				// Signal completion — usage may follow
			}
		}
	}

	if err := scanner.Err(); err != nil {
		ch <- StreamEvent{Type: EventError, Error: fmt.Errorf("stream read: %w", err)}
	}
}

func (p *OpenAICompatibleProvider) ListModels(ctx context.Context) ([]ModelInfo, error) {
	httpReq, err := http.NewRequestWithContext(ctx, "GET", p.baseURL+"/models", nil)
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	models := make([]ModelInfo, len(result.Data))
	for i, d := range result.Data {
		models[i] = ModelInfo{ID: d.ID}
	}
	return models, nil
}
