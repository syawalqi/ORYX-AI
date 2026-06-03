package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/syawalqi/flare/executor"
	"github.com/syawalqi/flare/llm"
)

type ToolHandler func(ctx context.Context, args json.RawMessage) (string, error)

type Agent struct {
	provider     llm.Provider
	exec         *executor.Executor
	model        string
	maxTokens    int
	temperature  float64
	maxIter      int
	tools        []llm.ToolDef
	handlers     map[string]ToolHandler
}

func New(provider llm.Provider, exec *executor.Executor, model string, maxTokens int, temperature float64, maxIter int) *Agent {
	a := &Agent{
		provider:    provider,
		exec:        exec,
		model:       model,
		maxTokens:   maxTokens,
		temperature: temperature,
		maxIter:     maxIter,
		tools:       defaultTools(),
		handlers:    make(map[string]ToolHandler),
	}
	a.registerHandlers()
	return a
}

type ToolResult struct {
	ToolCallID string `json:"tool_call_id"`
	Name       string `json:"name"`
	Output     string `json:"output"`
}

type StreamResult struct {
	Token      string        // text token from LLM
	Reasoning  string        // reasoning/thinking token from LLM
	ToolCalls  []llm.ToolCall // tool calls being made (non-nil when LLM requests tools)
	ToolResult *ToolResult   // result of executing a tool
	Done       bool          // agent execution complete
	Err        error
}

func defaultTools() []llm.ToolDef {
	return []llm.ToolDef{
		{
			Type: "function",
			Function: llm.ToolFuncDef{
				Name:        "run_command",
				Description: "Execute a shell command on the server",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"command": map[string]interface{}{
							"type":        "string",
							"description": "The shell command to execute",
						},
					},
					"required": []string{"command"},
				},
			},
		},
		{
			Type: "function",
			Function: llm.ToolFuncDef{
				Name:        "read_file",
				Description: "Read the contents of a file",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"path": map[string]interface{}{
							"type":        "string",
							"description": "Absolute path to the file",
						},
					},
					"required": []string{"path"},
				},
			},
		},
		{
			Type: "function",
			Function: llm.ToolFuncDef{
				Name:        "write_file",
				Description: "Create or overwrite a file with content",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"path": map[string]interface{}{
							"type":        "string",
							"description": "Absolute path to the file",
						},
						"content": map[string]interface{}{
							"type":        "string",
							"description": "Content to write to the file",
						},
					},
					"required": []string{"path", "content"},
				},
			},
		},
		{
			Type: "function",
			Function: llm.ToolFuncDef{
				Name:        "service_action",
				Description: "Start, stop, restart, or reload a systemd service",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"action": map[string]interface{}{
							"type": "string",
							"enum": []string{"start", "stop", "restart", "reload", "status"},
						},
						"service": map[string]interface{}{
							"type":        "string",
							"description": "Systemd service name",
						},
					},
					"required": []string{"action", "service"},
				},
			},
		},
		{
			Type: "function",
			Function: llm.ToolFuncDef{
				Name:        "search_logs",
				Description: "Search systemd journal logs",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"unit": map[string]interface{}{
							"type":        "string",
							"description": "Service unit name (optional)",
						},
						"priority": map[string]interface{}{
							"type":        "string",
							"description": "Log priority: emerg, alert, crit, err, warning, info",
						},
						"lines": map[string]interface{}{
							"type":        "integer",
							"description": "Number of lines to return",
						},
					},
				},
			},
		},
	}
}

func (a *Agent) registerHandlers() {
	a.handlers["run_command"] = func(ctx context.Context, args json.RawMessage) (string, error) {
		var p struct {
			Command string `json:"command"`
		}
		if err := json.Unmarshal(args, &p); err != nil {
			return "", fmt.Errorf("invalid args: %w", err)
		}
		result, err := a.exec.Run(ctx, p.Command)
		if err != nil {
			return fmt.Sprintf("error: %v", err), nil
		}
		var b strings.Builder
		b.WriteString(fmt.Sprintf("exit code: %d\n", result.ExitCode))
		b.WriteString(fmt.Sprintf("duration: %s\n", result.Duration))
		if result.Stdout != "" {
			b.WriteString("stdout:\n" + result.Stdout)
		}
		if result.Stderr != "" {
			b.WriteString("\nstderr:\n" + result.Stderr)
		}
		return b.String(), nil
	}

	a.handlers["read_file"] = func(ctx context.Context, args json.RawMessage) (string, error) {
		var p struct {
			Path string `json:"path"`
		}
		if err := json.Unmarshal(args, &p); err != nil {
			return "", fmt.Errorf("invalid args: %w", err)
		}
		result, err := a.exec.Run(ctx, fmt.Sprintf("head -500 %s", escapePath(p.Path)))
		if err != nil {
			return fmt.Sprintf("error: %v", err), nil
		}
		return result.Stdout, nil
	}

	a.handlers["write_file"] = func(ctx context.Context, args json.RawMessage) (string, error) {
		var p struct {
			Path    string `json:"path"`
			Content string `json:"content"`
		}
		if err := json.Unmarshal(args, &p); err != nil {
			return "", fmt.Errorf("invalid args: %w", err)
		}
		escaped := strings.ReplaceAll(p.Content, `'`, `'\\''`)
		_, err := a.exec.Run(ctx, fmt.Sprintf("mkdir -p $(dirname %s) && cat > %s << 'ENDOFFILE'\n%s\nENDOFFILE", escapePath(p.Path), escapePath(p.Path), escaped))
		if err != nil {
			return fmt.Sprintf("error: %v", err), nil
		}
		return fmt.Sprintf("wrote %s (%d bytes)", p.Path, len(p.Content)), nil
	}

	a.handlers["service_action"] = func(ctx context.Context, args json.RawMessage) (string, error) {
		var p struct {
			Action  string `json:"action"`
			Service string `json:"service"`
		}
		if err := json.Unmarshal(args, &p); err != nil {
			return "", fmt.Errorf("invalid args: %w", err)
		}
		if p.Action == "status" {
			result, err := a.exec.Run(ctx, fmt.Sprintf("systemctl status %s --no-pager -l 2>&1 | head -30", p.Service))
			if err != nil {
				return fmt.Sprintf("error: %v", result.Stderr), nil
			}
			return result.Stdout, nil
		}
		result, err := a.exec.Run(ctx, fmt.Sprintf("systemctl %s %s 2>&1", p.Action, p.Service))
		if err != nil {
			return fmt.Sprintf("error: %v", result.Stderr), nil
		}
		return fmt.Sprintf("service %s %s: %s", p.Service, p.Action, result.Stdout), nil
	}

	a.handlers["search_logs"] = func(ctx context.Context, args json.RawMessage) (string, error) {
		var p struct {
			Unit     string `json:"unit"`
			Priority string `json:"priority"`
			Lines    int    `json:"lines"`
		}
		if err := json.Unmarshal(args, &p); err != nil {
			return "", fmt.Errorf("invalid args: %w", err)
		}
		if p.Lines <= 0 || p.Lines > 200 {
			p.Lines = 50
		}
		cmd := fmt.Sprintf("journalctl -n %d --no-pager", p.Lines)
		if p.Unit != "" {
			cmd += fmt.Sprintf(" -u %s", p.Unit)
		}
		if p.Priority != "" {
			cmd += fmt.Sprintf(" -p %s", p.Priority)
		}
		result, err := a.exec.Run(ctx, cmd)
		if err != nil {
			return fmt.Sprintf("error: %v", err), nil
		}
		return result.Stdout, nil
	}
}

func escapePath(path string) string {
	return "'" + strings.ReplaceAll(path, "'", "'\\''") + "'"
}

func (a *Agent) ModelName() string {
	return a.model
}

// SetModel updates the model name at runtime.
func (a *Agent) SetModel(model string) {
	a.model = model
}

// Run does a blocking agent loop. Deprecated: use RunStream for streaming output.
func (a *Agent) Run(ctx context.Context, systemPrompt string, messages []llm.Message) (*llm.ChatResponse, error) {
	fullMsgs := make([]llm.Message, 0)
	if systemPrompt != "" {
		fullMsgs = append(fullMsgs, llm.Message{Role: llm.RoleSystem, Content: systemPrompt})
	}
	fullMsgs = append(fullMsgs, messages...)

	for iter := 0; iter < a.maxIter; iter++ {
		resp, err := a.provider.Chat(ctx, llm.ChatRequest{
			Model:       a.model,
			Messages:    fullMsgs,
			Tools:       a.tools,
			MaxTokens:   a.maxTokens,
			Temperature: a.temperature,
		})
		if err != nil {
			return nil, fmt.Errorf("llm chat: %w", err)
		}

		if len(resp.ToolCalls) == 0 {
			return resp, nil
		}

		assistantMsg := llm.Message{
			Role:      llm.RoleAssistant,
			Content:   resp.Content,
			ToolCalls: resp.ToolCalls,
		}
		fullMsgs = append(fullMsgs, assistantMsg)

		for _, tc := range resp.ToolCalls {
			handler, ok := a.handlers[tc.Function.Name]
			if !ok {
				fullMsgs = append(fullMsgs, llm.Message{
					Role:       llm.RoleTool,
					Content:    fmt.Sprintf("unknown tool: %s", tc.Function.Name),
					ToolCallID: tc.ID,
				})
				continue
			}
			output, err := handler(ctx, json.RawMessage(tc.Function.Arguments))
			if err != nil {
				output = fmt.Sprintf("tool error: %v", err)
			}
			fullMsgs = append(fullMsgs, llm.Message{
				Role:       llm.RoleTool,
				Content:    output,
				ToolCallID: tc.ID,
			})
		}
	}

	return nil, fmt.Errorf("max iterations (%d) reached without final response", a.maxIter)
}

// RunStream executes the agent loop with streaming output for each LLM call.
// It returns a channel of StreamResult that the caller reads for live output.
// The channel is closed when the agent finishes.
func (a *Agent) RunStream(ctx context.Context, systemPrompt string, messages []llm.Message) (<-chan StreamResult, error) {
	ch := make(chan StreamResult, 128)

	fullMsgs := make([]llm.Message, 0)
	if systemPrompt != "" {
		fullMsgs = append(fullMsgs, llm.Message{Role: llm.RoleSystem, Content: systemPrompt})
	}
	fullMsgs = append(fullMsgs, messages...)

	go func() {
		defer close(ch)

		for iter := 0; iter < a.maxIter; iter++ {
			events, err := a.provider.ChatStream(ctx, llm.ChatRequest{
				Model:       a.model,
				Messages:    fullMsgs,
				Tools:       a.tools,
				MaxTokens:   a.maxTokens,
				Temperature: a.temperature,
			})
			if err != nil {
				ch <- StreamResult{Err: fmt.Errorf("llm stream: %w", err)}
				return
			}

			var content strings.Builder
			var toolCalls []llm.ToolCall

			for evt := range events {
				switch evt.Type {
				case llm.EventToken:
					content.WriteString(evt.Content)
					ch <- StreamResult{Token: evt.Content}
				case llm.EventReasoning:
					ch <- StreamResult{Reasoning: evt.Content}
				case llm.EventToolCall:
					if evt.ToolCallDelta != nil {
						tc := *evt.ToolCallDelta
						toolCalls = append(toolCalls, tc)
						ch <- StreamResult{
							ToolCalls: []llm.ToolCall{tc},
						}
					}
				case llm.EventError:
					ch <- StreamResult{Err: evt.Error}
					return
				case llm.EventDone:
					// stream complete for this LLM call
				}
			}

			if len(toolCalls) == 0 {
				// Final response
				ch <- StreamResult{Done: true}
				return
			}

			// Assistant message with tool calls
			fullMsgs = append(fullMsgs, llm.Message{
				Role:      llm.RoleAssistant,
				Content:   content.String(),
				ToolCalls: toolCalls,
			})

			// Execute each tool
			for _, tc := range toolCalls {
				handler, ok := a.handlers[tc.Function.Name]
				if !ok {
					output := fmt.Sprintf("unknown tool: %s", tc.Function.Name)
					fullMsgs = append(fullMsgs, llm.Message{
						Role:       llm.RoleTool,
						Content:    output,
						ToolCallID: tc.ID,
					})
					ch <- StreamResult{ToolResult: &ToolResult{
						ToolCallID: tc.ID,
						Name:       tc.Function.Name,
						Output:     output,
					}}
					continue
				}
				output, err := handler(ctx, json.RawMessage(tc.Function.Arguments))
				if err != nil {
					output = fmt.Sprintf("tool error: %v", err)
				}
				fullMsgs = append(fullMsgs, llm.Message{
					Role:       llm.RoleTool,
					Content:    output,
					ToolCallID: tc.ID,
				})
				ch <- StreamResult{ToolResult: &ToolResult{
					ToolCallID: tc.ID,
					Name:       tc.Function.Name,
					Output:     output,
				}}
			}
			// Continue loop — LLM will see tool results and produce final response
		}

		ch <- StreamResult{Err: fmt.Errorf("max iterations (%d) reached", a.maxIter)}
	}()

	return ch, nil
}
