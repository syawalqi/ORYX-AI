package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"strings"
	"time"

	"github.com/syawalqi/oryx/executor"
	"github.com/syawalqi/oryx/llm"
	"github.com/syawalqi/oryx/tools"
)

// AuditFunc is called after every tool execution for audit logging.
// Args is the raw JSON arguments string (truncated for safety).
// Result is the output string (first 200 chars).
// Duration is the execution time.
type AuditFunc func(tool, args, result string, success bool, duration string, iteration int)

type Agent struct {
	provider    llm.Provider
	exec        *executor.Executor
	registry    *tools.Registry
	model       string
	maxTokens   int
	temperature float64
	budget      *BudgetTracker
	auditFn     AuditFunc
	planMode    bool
}

// AgentOption configures the Agent.
type AgentOption func(*Agent)

// WithBudget sets budget limits. Zero values mean unlimited.
func WithBudget(maxIter, maxTokens int, maxCost float64) AgentOption {
	return func(a *Agent) {
		a.budget = NewBudgetTracker(maxIter, maxTokens, maxCost, a.model)
	}
}

// WithAudit sets an audit callback for tool execution logging.
func WithAudit(fn AuditFunc) AgentOption {
	return func(a *Agent) {
		a.auditFn = fn
	}
}

func New(provider llm.Provider, exec *executor.Executor, model string, maxTokens int, temperature float64, maxIter int, opts ...AgentOption) *Agent {
	reg := tools.New()
	tools.RegisterDefaults(reg, exec)

	a := &Agent{
		provider:    provider,
		exec:        exec,
		registry:    reg,
		model:       model,
		maxTokens:   maxTokens,
		temperature: temperature,
		budget:      NewBudgetTracker(maxIter, 0, 0, model),
	}

	for _, opt := range opts {
		opt(a)
	}

	return a
}

type ToolResult struct {
	ToolCallID string `json:"tool_call_id"`
	Name       string `json:"name"`
	Output     string `json:"output"`
}

type StreamResult struct {
	Token      string          // text token from LLM
	Reasoning  string          // reasoning/thinking token from LLM
	ToolCalls  []llm.ToolCall  // tool calls being made
	ToolResult *ToolResult     // result of executing a tool
	Done       bool            // agent execution complete
	Err        error
}

func (a *Agent) ModelName() string {
	return a.model
}

func (a *Agent) SetModel(model string) {
	a.model = model
}

func (a *Agent) SetPlanMode(enabled bool) {
	a.planMode = enabled
}

// Registry returns the agent's tool registry, allowing external code to
// register additional tools or inspect tool definitions.
func (a *Agent) Registry() *tools.Registry {
	return a.registry
}

// Budget returns the agent's budget tracker for inspecting usage mid-session.
func (a *Agent) Budget() *BudgetTracker {
	return a.budget
}

// exponentialBackoff sleeps for a duration that grows exponentially with the
// retry attempt, plus random jitter. Formula: min(maxWait, base * 2^attempt)
// Returns after sleeping, or immediately if ctx is cancelled.
func exponentialBackoff(ctx context.Context, attempt int) {
	if attempt <= 0 {
		return
	}
	base := time.Second
	maxWait := 30 * time.Second
	delay := time.Duration(base) * (1 << uint(attempt-1))
	if delay > maxWait {
		delay = maxWait
	}
	// Add jitter: ±25%
	jitter := time.Duration(float64(delay) * (0.75 + 0.5*rand.Float64()))
	select {
	case <-ctx.Done():
	case <-time.After(jitter):
	}
}

// RunStream executes the agent loop with streaming output.
// It returns a channel of StreamResult for live output.
// The channel is closed when the agent finishes.
func (a *Agent) RunStream(ctx context.Context, systemPrompt string, messages []llm.Message) (<-chan StreamResult, error) {
	ch := make(chan StreamResult, 128)

	fullMsgs := make([]llm.Message, 0)
	if systemPrompt != "" {
		fullMsgs = append(fullMsgs, llm.Message{Role: llm.RoleSystem, Content: systemPrompt})
	}
	fullMsgs = append(fullMsgs, messages...)

	a.registry.ResetCounts()

	go func() {
		defer close(ch)

		for {
			// Budget check
			if err := a.budget.Allow(); err != nil {
				ch <- StreamResult{Err: err}
				return
			}

			// LLM call with retry
			events, err := a.chatStreamWithRetry(ctx, fullMsgs)
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
						ch <- StreamResult{ToolCalls: []llm.ToolCall{tc}}
					}
				case llm.EventError:
					ch <- StreamResult{Err: evt.Error}
					return
				case llm.EventDone:
				}
			}

			if len(toolCalls) == 0 {
				ch <- StreamResult{Done: true}
				return
			}

			// Record iteration (no usage data from streaming, estimate: 1 token ≈ 4 chars)
			a.budget.Record(0, content.Len()/4)

			// Assistant message with tool calls
			fullMsgs = append(fullMsgs, llm.Message{
				Role:      llm.RoleAssistant,
				Content:   content.String(),
				ToolCalls: toolCalls,
			})

			// Execute each tool via the registry
			for _, tc := range toolCalls {
				start := time.Now()
				output, err := a.registry.Execute(ctx, tc.Function.Name, json.RawMessage(tc.Function.Arguments), a.planMode)
				duration := time.Since(start)

				if err != nil {
					output = fmt.Sprintf("tool error: %v", err)
				}

				// Audit logging
				if a.auditFn != nil {
					resultTrunc := output
					if len(resultTrunc) > 200 {
						resultTrunc = resultTrunc[:200]
					}
					argsTrunc := tc.Function.Arguments
					if len(argsTrunc) > 100 {
						argsTrunc = argsTrunc[:100]
					}
					a.auditFn(tc.Function.Name, argsTrunc, resultTrunc, err == nil, duration.Round(time.Millisecond).String(), a.budget.Iter)
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
		}
	}()

	return ch, nil
}

// chatStreamWithRetry calls ChatStream with exponential backoff on transient failures.
func (a *Agent) chatStreamWithRetry(ctx context.Context, msgs []llm.Message) (<-chan llm.StreamEvent, error) {
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			exponentialBackoff(ctx, attempt)
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
		}

		events, err := a.provider.ChatStream(ctx, llm.ChatRequest{
			Model:       a.model,
			Messages:    msgs,
			Tools:       a.registry.Schemas(),
			MaxTokens:   a.maxTokens,
			Temperature: a.temperature,
		})

		if err == nil {
			return events, nil
		}

		// Non-retryable: context cancelled, auth errors
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		lastErr = err
	}
	return nil, fmt.Errorf("chat stream failed after 3 retries: %w", lastErr)
}
