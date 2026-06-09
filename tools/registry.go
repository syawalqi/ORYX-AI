// Package tools provides a formal tool registry with JSON Schema definitions,
// execution dispatch, and call-count tracking. All ORYX tools register here
// instead of being hardcoded in the agent loop.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/syawalqi/oryx/llm"
)

// Definition describes a tool that the agent can call.
type Definition struct {
	// Name is the unique tool identifier (snake_case).
	Name string

	// Description explains when and how to use this tool.
	Description string

	// Parameters is the JSON Schema object describing the tool's arguments.
	Parameters map[string]interface{}

	// Handler executes the tool and returns a string result for the LLM.
	Handler func(ctx context.Context, args json.RawMessage) (string, error)

	// BlockInPlan prevents this tool from running in plan mode.
	BlockInPlan bool

	// MaxCalls limits how many times this tool can be called in one session.
	// 0 means unlimited.
	MaxCalls int
}

// Registry manages a collection of tool definitions and tracks their usage.
type Registry struct {
	mu         sync.RWMutex
	defs       map[string]Definition
	callCounts map[string]int
}

// New creates an empty tool registry.
func New() *Registry {
	return &Registry{
		defs:       make(map[string]Definition),
		callCounts: make(map[string]int),
	}
}

// Register adds a tool definition. Panics on duplicate names.
func (r *Registry) Register(def Definition) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.defs[def.Name]; exists {
		panic(fmt.Sprintf("tool already registered: %s", def.Name))
	}
	r.defs[def.Name] = def
	r.callCounts[def.Name] = 0
}

// Get returns a tool definition by name.
func (r *Registry) Get(name string) (Definition, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	def, ok := r.defs[name]
	return def, ok
}

// Schemas returns the tool definitions in the format expected by LLM providers.
func (r *Registry) Schemas() []llm.ToolDef {
	r.mu.RLock()
	defer r.mu.RUnlock()
	schemas := make([]llm.ToolDef, 0, len(r.defs))
	for _, def := range r.defs {
		schemas = append(schemas, llm.ToolDef{
			Type: "function",
			Function: llm.ToolFuncDef{
				Name:        def.Name,
				Description: def.Description,
				Parameters:  def.Parameters,
			},
		})
	}
	return schemas
}

// Execute dispatches a tool call. It returns the tool's output string or an error.
// It enforces the MaxCalls limit — if exceeded, the tool is rejected with a message
// telling the LLM to use other tools instead.
func (r *Registry) Execute(ctx context.Context, name string, args json.RawMessage, planMode bool) (string, error) {
	def, ok := r.Get(name)
	if !ok {
		return "", fmt.Errorf("unknown tool: %s", name)
	}

	// Plan mode block
	if planMode && def.BlockInPlan {
		return fmt.Sprintf("⚠️ BLOCKED: %s is not available in plan mode. Switch to normal mode (/plan) or use read_file/search_logs instead.", name), nil
	}

	// Call limit enforcement
	if def.MaxCalls > 0 {
		r.mu.Lock()
		count := r.callCounts[name]
		if count >= def.MaxCalls {
			r.mu.Unlock()
			return fmt.Sprintf("LIMIT REACHED: %s has been called %d times (max %d). Use a different approach or stop.", name, count, def.MaxCalls), nil
		}
		r.callCounts[name] = count + 1
		r.mu.Unlock()
	} else {
		r.mu.Lock()
		r.callCounts[name]++
		r.mu.Unlock()
	}

	output, err := def.Handler(ctx, args)
	if err != nil {
		return "", fmt.Errorf("tool error: %w", err)
	}
	return output, nil
}

// CallCount returns how many times a tool has been called this session.
func (r *Registry) CallCount(name string) int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.callCounts[name]
}

// AllNames returns all registered tool names.
func (r *Registry) AllNames() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.defs))
	for n := range r.defs {
		names = append(names, n)
	}
	return names
}

// ResetCounts resets all call counters to zero. Useful between sessions.
func (r *Registry) ResetCounts() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for k := range r.callCounts {
		r.callCounts[k] = 0
	}
}
