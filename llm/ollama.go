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

// OllamaProvider implements the Provider interface for local Ollama instances.
// Provides zero-cost local inference using models like qwen3, llama, etc.
type OllamaProvider struct {
	baseURL string
	client  *http.Client
}

func NewOllamaProvider(baseURL string) *OllamaProvider {
	if baseURL == "" {
		baseURL = "http://localhost:11434"
	}
	return &OllamaProvider{
		baseURL: strings.TrimRight(baseURL, "/"),
		client:  &http.Client{},
	}
}

func (p *OllamaProvider) Name() string { return "ollama" }

type ollamaReq struct {
	Model    string      `json:"model"`
	Messages []ollamaMsg `json:"messages"`
	Tools    []ToolDef   `json:"tools,omitempty"`
	Stream   bool        `json:"stream"`
	Options  *ollamaOpts `json:"options,omitempty"`
}

type ollamaOpts struct {
	Temperature float64 `json:"temperature,omitempty"`
	NumPredict  int     `json:"num_predict,omitempty"`
}

type ollamaMsg struct {
	Role       string     `json:"role"`
	Content    string     `json:"content"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
}

type ollamaResp struct {
	Model      string     `json:"model"`
	Message    ollamaMsg  `json:"message"`
	Done       bool       `json:"done"`
	DoneReason string     `json:"done_reason"`
}

type ollamaToolCall struct {
	Function struct {
		Name      string `json:"name"`
		Arguments interface{} `json:"arguments"`
	} `json:"function"`
}

func (p *OllamaProvider) Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	body := p.buildReq(req, false)
	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.baseURL+"/api/chat", bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

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
		return nil, fmt.Errorf("Ollama error %d: %s", resp.StatusCode, string(respData))
	}

	var result ollamaResp
	if err := json.Unmarshal(respData, &result); err != nil {
		return nil, fmt.Errorf("parse: %w", err)
	}

	chatResp := &ChatResponse{
		Content: result.Message.Content,
	}

	// Convert Ollama tool calls to our format
	if len(result.Message.ToolCalls) > 0 {
		chatResp.ToolCalls = result.Message.ToolCalls
	}

	return chatResp, nil
}

func (p *OllamaProvider) ChatStream(ctx context.Context, req ChatRequest) (<-chan StreamEvent, error) {
	body := p.buildReq(req, true)
	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.baseURL+"/api/chat", bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("http: %w", err)
	}

	if resp.StatusCode >= 400 {
		resp.Body.Close()
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("Ollama error %d: %s", resp.StatusCode, string(bodyBytes))
	}

	ch := make(chan StreamEvent, 64)
	go func() {
		defer resp.Body.Close()
		defer close(ch)

		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			line := scanner.Text()
			if line == "" {
				continue
			}

			var chunk ollamaResp
			if err := json.Unmarshal([]byte(line), &chunk); err != nil {
				continue
			}

			if chunk.Message.Content != "" {
				ch <- StreamEvent{Type: EventToken, Content: chunk.Message.Content}
			}

			if len(chunk.Message.ToolCalls) > 0 {
				for _, tc := range chunk.Message.ToolCalls {
					call := tc
					ch <- StreamEvent{Type: EventToolCall, ToolCallDelta: &call}
				}
			}

			if chunk.Done {
				var usage Usage
				ch <- StreamEvent{Type: EventDone, Usage: &usage}
			}
		}
	}()

	return ch, nil
}

func (p *OllamaProvider) buildReq(req ChatRequest, stream bool) ollamaReq {
	msgs := make([]ollamaMsg, len(req.Messages))
	for i, m := range req.Messages {
		msgs[i] = ollamaMsg{
			Role:       string(m.Role),
			Content:    m.Content,
			ToolCallID: m.ToolCallID,
			ToolCalls:  m.ToolCalls,
		}
	}

	r := ollamaReq{
		Model:    req.Model,
		Messages: msgs,
		Stream:   stream,
	}

	if len(req.Tools) > 0 {
		r.Tools = req.Tools
	}

	r.Options = &ollamaOpts{
		Temperature: req.Temperature,
		NumPredict:  req.MaxTokens,
	}

	return r
}

func (p *OllamaProvider) ListModels(ctx context.Context) ([]ModelInfo, error) {
	httpReq, err := http.NewRequestWithContext(ctx, "GET", p.baseURL+"/api/tags", nil)
	if err != nil {
		return nil, err
	}

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	models := make([]ModelInfo, len(result.Models))
	for i, m := range result.Models {
		models[i] = ModelInfo{ID: m.Name}
	}
	return models, nil
}
