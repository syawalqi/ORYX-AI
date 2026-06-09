package agent

import "fmt"

// ModelCost holds per-token pricing for a model in USD per million tokens.
type ModelCost struct {
	InputPerMTok  float64
	OutputPerMTok float64
}

// DefaultModelCosts maps known model prefixes to their pricing.
// Extend this as new providers are added.
var DefaultModelCosts = map[string]ModelCost{
	"deepseek":  {InputPerMTok: 0.14, OutputPerMTok: 0.28},
	"claude":    {InputPerMTok: 3.00, OutputPerMTok: 15.00},
	"gpt":       {InputPerMTok: 2.50, OutputPerMTok: 15.00},
	"gemini":    {InputPerMTok: 0.30, OutputPerMTok: 2.50},
}

func costForModel(model string) ModelCost {
	for prefix, c := range DefaultModelCosts {
		if len(model) >= len(prefix) && model[:len(prefix)] == prefix {
			return c
		}
	}
	// Default to DeepSeek Flash pricing as conservative estimate
	return ModelCost{InputPerMTok: 0.14, OutputPerMTok: 0.28}
}

// BudgetTracker enforces limits on token usage, cost, and iteration count
// for an agent session. Call Allow() before each LLM call and Record() after.
type BudgetTracker struct {
	MaxIter    int
	MaxTokens  int     // 0 = unlimited
	MaxCost    float64 // USD, 0 = unlimited
	modelCost  ModelCost

	Iter       int
	TokensIn   int
	TokensOut  int
	TotalCost  float64
}

func NewBudgetTracker(maxIter, maxTokens int, maxCost float64, modelName string) *BudgetTracker {
	if maxIter <= 0 {
		maxIter = 50
	}
	return &BudgetTracker{
		MaxIter:   maxIter,
		MaxTokens: maxTokens,
		MaxCost:   maxCost,
		modelCost: costForModel(modelName),
	}
}

// Allow returns an error if any budget limit is exceeded.
func (b *BudgetTracker) Allow() error {
	if b.Iter >= b.MaxIter {
		return fmt.Errorf("budget: max iterations (%d) reached", b.MaxIter)
	}
	if b.MaxTokens > 0 && b.TokensIn+b.TokensOut >= b.MaxTokens {
		return fmt.Errorf("budget: max tokens (%d) reached", b.MaxTokens)
	}
	if b.MaxCost > 0 && b.TotalCost >= b.MaxCost {
		return fmt.Errorf("budget: max cost ($%.2f) reached", b.MaxCost)
	}
	return nil
}

// Record updates counters after an LLM call. Pass usage from the provider response.
func (b *BudgetTracker) Record(promptTokens, completionTokens int) {
	b.Iter++
	b.TokensIn += promptTokens
	b.TokensOut += completionTokens
	costIn := float64(promptTokens) * b.modelCost.InputPerMTok / 1_000_000
	costOut := float64(completionTokens) * b.modelCost.OutputPerMTok / 1_000_000
	b.TotalCost += costIn + costOut
}

// Summary returns a human-readable budget usage line.
func (b *BudgetTracker) Summary() string {
	return fmt.Sprintf("iters:%d/%d tokens:%d cost:$%.4f",
		b.Iter, b.MaxIter, b.TokensIn+b.TokensOut, b.TotalCost)
}

// RemainingTokens returns tokens left before hitting the limit (or -1 if unlimited).
func (b *BudgetTracker) RemainingTokens() int {
	if b.MaxTokens <= 0 {
		return -1
	}
	remaining := b.MaxTokens - b.TokensIn - b.TokensOut
	if remaining < 0 {
		return 0
	}
	return remaining
}
