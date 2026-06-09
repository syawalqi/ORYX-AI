package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/syawalqi/oryx/llm"
)

// PlanStep represents one step in a structured execution plan.
type PlanStep struct {
	ID          int    `json:"id"`
	Description string `json:"description"`
	Status      string `json:"status"` // "pending", "running", "done", "failed"
	Result      string `json:"result,omitempty"`
	Error       string `json:"error,omitempty"`
}

// Plan holds a goal and its decomposed steps.
type Plan struct {
	Goal    string     `json:"goal"`
	Steps   []PlanStep `json:"steps"`
	created time.Time
}

// PlanEvent is sent on the stream channel during plan execution.
// Only one field is non-zero per event.
type PlanEvent struct {
	Plan       *Plan     `json:"plan,omitempty"`       // initial plan created
	StepUpdate *PlanStep `json:"step_update,omitempty"` // step status changed
	StepResult string    `json:"step_result,omitempty"` // step output text
	Done       bool      `json:"done,omitempty"`
	Error      string    `json:"error,omitempty"`
}

// RunPlanStream executes a Plan-and-Execute loop.
// It first asks the LLM to decompose the goal into steps, then executes each
// step sequentially with its own ReAct sub-loop.
func (a *Agent) RunPlanStream(ctx context.Context, systemPrompt string, goal string) (<-chan PlanEvent, error) {
	ch := make(chan PlanEvent, 128)

	go func() {
		defer close(ch)

		// Step 1: Create the plan
		plan, err := a.createPlan(ctx, systemPrompt, goal)
		if err != nil {
			ch <- PlanEvent{Error: fmt.Sprintf("plan creation failed: %v", err)}
			return
		}
		ch <- PlanEvent{Plan: plan}

		// Step 2: Execute each step sequentially
		for i := range plan.Steps {
			select {
			case <-ctx.Done():
				ch <- PlanEvent{Error: "plan execution cancelled"}
				return
			default:
			}

			plan.Steps[i].Status = "running"
			ch <- PlanEvent{StepUpdate: &plan.Steps[i]}

			// Build context from previous step results
			result, err := a.executePlanStep(ctx, systemPrompt, plan, i)

			if err != nil {
				plan.Steps[i].Status = "failed"
				plan.Steps[i].Error = err.Error()
				ch <- PlanEvent{StepUpdate: &plan.Steps[i]}
				ch <- PlanEvent{Error: fmt.Sprintf("step %d failed: %v", i+1, err)}
				return
			}

			plan.Steps[i].Status = "done"
			plan.Steps[i].Result = result
			ch <- PlanEvent{StepUpdate: &plan.Steps[i]}
			ch <- PlanEvent{StepResult: result}
		}

		ch <- PlanEvent{Done: true}
	}()

	return ch, nil
}

// createPlan asks the LLM to decompose a goal into numbered steps.
func (a *Agent) createPlan(ctx context.Context, systemPrompt, goal string) (*Plan, error) {
	planPrompt := systemPrompt + `

You are a PLANNER. Your job is to decompose the user's goal into a numbered list of concrete steps.

For each step, describe WHAT to do. Each step should be achievable with a single tool call or a small set of related tool calls.

Respond with ONLY this JSON format, no other text:
{"steps": ["Step 1: description", "Step 2: description", ...]}

User goal: ` + goal

	msgs := []llm.Message{
		{Role: llm.RoleUser, Content: planPrompt},
	}

	resp, err := a.chatWithRetry(ctx, msgs, false) // no tools for planning
	if err != nil {
		return nil, err
	}

	// Parse JSON response
	var planData struct {
		Steps []string `json:"steps"`
	}
	cleaned := strings.TrimSpace(resp)
	// Handle code fences
	if idx := strings.Index(cleaned, "{"); idx >= 0 {
		cleaned = cleaned[idx:]
	}
	if idx := strings.LastIndex(cleaned, "}"); idx >= 0 {
		cleaned = cleaned[:idx+1]
	}
	if err := json.Unmarshal([]byte(cleaned), &planData); err != nil {
		return nil, fmt.Errorf("parse plan: %w\nresponse: %s", err, resp)
	}

	if len(planData.Steps) == 0 {
		return nil, fmt.Errorf("plan has no steps")
	}

	plan := &Plan{
		Goal:    goal,
		created: time.Now(),
	}
	for i, desc := range planData.Steps {
		plan.Steps = append(plan.Steps, PlanStep{
			ID:          i + 1,
			Description: desc,
			Status:      "pending",
		})
	}
	return plan, nil
}

// executePlanStep runs one plan step using the agent's ReAct loop.
func (a *Agent) executePlanStep(ctx context.Context, systemPrompt string, plan *Plan, stepIndex int) (string, error) {
	step := plan.Steps[stepIndex]

	// Build context from previous step results
	var prevResults []string
	for i := 0; i < stepIndex; i++ {
		if plan.Steps[i].Result != "" {
			prevResults = append(prevResults, fmt.Sprintf("Step %d (%s):\n%s", i+1, plan.Steps[i].Description, plan.Steps[i].Result))
		}
	}

	prevContext := ""
	if len(prevResults) > 0 {
		prevContext = "\n\nPrevious steps completed:\n" + strings.Join(prevResults, "\n---\n")
	}

	stepPrompt := fmt.Sprintf(`%s

PLAN MODE — You are executing step %d of the plan.

Overall goal: %s
Current step: %s%s

Focus ONLY on this step. Use available tools to complete it. When done, summarize what was accomplished.`,
		systemPrompt, step.ID, plan.Goal, step.Description, prevContext)

	msgs := []llm.Message{
		{Role: llm.RoleUser, Content: stepPrompt},
	}

	// Run a bounded ReAct loop for this step
	fullMsgs := make([]llm.Message, 0)
	if systemPrompt != "" {
		fullMsgs = append(fullMsgs, llm.Message{Role: llm.RoleSystem, Content: stepPrompt})
	}
	fullMsgs = append(fullMsgs, msgs...)

	const maxStepIters = 10
	for iter := 0; iter < maxStepIters; iter++ {
		events, err := a.provider.ChatStream(ctx, llm.ChatRequest{
			Model:       a.model,
			Messages:    fullMsgs,
			Tools:       a.registry.Schemas(),
			MaxTokens:   a.maxTokens,
			Temperature: a.temperature,
		})
		if err != nil {
			return "", fmt.Errorf("step %d stream: %w", step.ID, err)
		}

		var content strings.Builder
		var toolCalls []llm.ToolCall
		for evt := range events {
			switch evt.Type {
			case llm.EventToken:
				content.WriteString(evt.Content)
			case llm.EventToolCall:
				if evt.ToolCallDelta != nil {
					toolCalls = append(toolCalls, *evt.ToolCallDelta)
				}
			case llm.EventError:
				return "", evt.Error
			}
		}

		if len(toolCalls) == 0 {
			return strings.TrimSpace(content.String()), nil
		}

		fullMsgs = append(fullMsgs, llm.Message{
			Role:      llm.RoleAssistant,
			Content:   content.String(),
			ToolCalls: toolCalls,
		})

		for _, tc := range toolCalls {
			output, err := a.registry.Execute(ctx, tc.Function.Name, json.RawMessage(tc.Function.Arguments), true)
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

	return "", fmt.Errorf("step %d reached max iterations (%d)", step.ID, maxStepIters)
}

// chatWithRetry is a simplified non-streaming call with retry for plan creation.
func (a *Agent) chatWithRetry(ctx context.Context, msgs []llm.Message, tools bool) (string, error) {
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			exponentialBackoff(ctx, attempt)
		}

		var toolDefs []llm.ToolDef
		if tools {
			toolDefs = a.registry.Schemas()
		}

		resp, err := a.provider.Chat(ctx, llm.ChatRequest{
			Model:       a.model,
			Messages:    msgs,
			Tools:       toolDefs,
			MaxTokens:   a.maxTokens,
			Temperature: a.temperature,
		})
		if err == nil {
			return resp.Content, nil
		}
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		lastErr = err
	}
	return "", fmt.Errorf("chat failed after 3 retries: %w", lastErr)
}
