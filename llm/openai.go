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
	"time"
)

type OpenAIProvider struct {
	name    string
	baseURL string
	apiKey  string
	client  *http.Client
}

func NewOpenAIProvider(name, baseURL, apiKey string) *OpenAIProvider {
	return &OpenAIProvider{
		name:    name,
		baseURL: strings.TrimRight(baseURL, "/"),
		apiKey:  apiKey,
		client:  &http.Client{},
	}
}

func (p *OpenAIProvider) Name() string { return p.name }

type openAIReq struct {
	Model       string      `json:"model"`
	Messages    []Message   `json:"messages"`
	Tools       []ToolDef   `json:"tools,omitempty"`
	MaxTokens   int         `json:"max_tokens,omitempty"`
	Temperature float64     `json:"temperature,omitempty"`
	Stream      bool        `json:"stream"`
}

type openAIResp struct {
	Choices []struct {
		Message struct {
			Content          string     `json:"content"`
			ReasoningContent string     `json:"reasoning_content"`
			ToolCalls        []ToolCall `json:"tool_calls"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage Usage `json:"usage"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// tcStreamDelta captures streaming tool call deltas with their index for merging.
type tcStreamDelta struct {
	Index    int    `json:"index"`
	ID       string `json:"id,omitempty"`
	Type     string `json:"type,omitempty"`
	Function struct {
		Name      string `json:"name,omitempty"`
		Arguments string `json:"arguments,omitempty"`
	} `json:"function,omitempty"`
}

func (p *OpenAIProvider) authHeader() (string, string) {
	return "Authorization", "Bearer " + p.apiKey
}

func (p *OpenAIProvider) Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	body := openAIReq{
		Model:       req.Model,
		Messages:    req.Messages,
		MaxTokens:   req.MaxTokens,
		Temperature: req.Temperature,
		Stream:      false,
	}
	if len(req.Tools) > 0 {
		body.Tools = req.Tools
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/chat/completions", bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("request: %w", err)
	}
	hdr, val := p.authHeader()
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set(hdr, val)

	httpResp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("http: %w", err)
	}
	defer httpResp.Body.Close()

	respBody, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, fmt.Errorf("read: %w", err)
	}
	if httpResp.StatusCode != 200 {
		return nil, fmt.Errorf("API %d: %s", httpResp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	var result openAIResp
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("unmarshal: %w", err)
	}
	if result.Error != nil {
		return nil, fmt.Errorf("API error: %s", result.Error.Message)
	}
	if len(result.Choices) == 0 {
		return nil, fmt.Errorf("no choices")
	}

	msg := result.Choices[0].Message
	content := msg.Content
	if content == "" && msg.ReasoningContent != "" {
		content = msg.ReasoningContent
	}

	return &ChatResponse{
		Content:   content,
		ToolCalls: msg.ToolCalls,
		Usage:     result.Usage,
	}, nil
}

func (p *OpenAIProvider) ChatStream(ctx context.Context, req ChatRequest) (<-chan StreamEvent, error) {
	body := openAIReq{
		Model:       req.Model,
		Messages:    req.Messages,
		MaxTokens:   req.MaxTokens,
		Temperature: req.Temperature,
		Stream:      true,
	}
	if len(req.Tools) > 0 {
		body.Tools = req.Tools
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/chat/completions", bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("request: %w", err)
	}
	hdr, val := p.authHeader()
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set(hdr, val)

	// Separate client with timeout for streaming — prevents hang if server
	// never sends [DONE] or closes the connection after the response body.
	streamClient := &http.Client{
		Timeout: 180 * time.Second,
	}
	httpResp, err := streamClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("http: %w", err)
	}

	if httpResp.StatusCode != 200 {
		body, _ := io.ReadAll(httpResp.Body)
		httpResp.Body.Close()
		return nil, fmt.Errorf("API %d: %s", httpResp.StatusCode, strings.TrimSpace(string(body)))
	}

	events := make(chan StreamEvent, 64)
	go func() {
		defer httpResp.Body.Close()
		defer close(events)

		// Accumulate tool call deltas by index across chunks
		accToolCalls := make(map[int]*ToolCall)

		scanner := bufio.NewScanner(httpResp.Body)
		scanner.Buffer(make([]byte, 0, 64*1024), 256*1024)

		for scanner.Scan() {
			line := scanner.Text()
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			data := strings.TrimPrefix(line, "data: ")
			if data == "[DONE]" {
				events <- StreamEvent{Type: EventDone}
				return
			}

			var chunk struct {
				Choices []struct {
					Delta struct {
						Content          string          `json:"content"`
						ReasoningContent string          `json:"reasoning_content"`
						ToolCalls        []tcStreamDelta `json:"tool_calls"`
					} `json:"delta"`
					FinishReason *string `json:"finish_reason"`
				} `json:"choices"`
				Usage *Usage `json:"usage,omitempty"`
			}
			if err := json.Unmarshal([]byte(data), &chunk); err != nil {
				continue
			}
			if len(chunk.Choices) == 0 {
				continue
			}
			choice := chunk.Choices[0]
			if choice.Delta.ReasoningContent != "" {
				events <- StreamEvent{Type: EventReasoning, Content: choice.Delta.ReasoningContent}
			}
			if choice.Delta.Content != "" {
				events <- StreamEvent{Type: EventToken, Content: choice.Delta.Content}
			}
			// Accumulate tool call deltas by index — stream sends them piece by piece
			for _, tcd := range choice.Delta.ToolCalls {
				existing, ok := accToolCalls[tcd.Index]
				if !ok {
					existing = &ToolCall{}
					accToolCalls[tcd.Index] = existing
				}
				if tcd.ID != "" {
					existing.ID = tcd.ID
				}
				if tcd.Type != "" {
					existing.Type = tcd.Type
				}
				if tcd.Function.Name != "" {
					existing.Function.Name = tcd.Function.Name
				}
				if tcd.Function.Arguments != "" {
					existing.Function.Arguments += tcd.Function.Arguments
				}
			}
			if choice.FinishReason != nil {
				// Send accumulated complete tool calls before done
				for _, tc := range accToolCalls {
					events <- StreamEvent{Type: EventToolCall, ToolCallDelta: tc}
				}
				if chunk.Usage != nil {
					events <- StreamEvent{Type: EventDone, Usage: chunk.Usage}
				} else {
					events <- StreamEvent{Type: EventDone}
				}
				return
			}
		}
		if err := scanner.Err(); err != nil {
			events <- StreamEvent{Type: EventError, Error: err}
		}
	}()

	return events, nil
}

// ListModels calls GET /models and returns available models.
func (p *OpenAIProvider) ListModels(ctx context.Context) ([]ModelInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.baseURL+"/models", nil)
	if err != nil {
		return nil, fmt.Errorf("request: %w", err)
	}
	hdr, val := p.authHeader()
	req.Header.Set(hdr, val)

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var result struct {
		Data []struct {
			ID          string `json:"id"`
			Name        string `json:"name,omitempty"`
			Description string `json:"description,omitempty"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}

	models := make([]ModelInfo, len(result.Data))
	for i, m := range result.Data {
		models[i] = ModelInfo{
			ID:          m.ID,
			Name:        m.Name,
			Description: m.Description,
		}
	}
	return models, nil
}
