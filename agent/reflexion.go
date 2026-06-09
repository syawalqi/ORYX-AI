package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/syawalqi/oryx/llm"
)

// ReflexionConfig controls the self-critique loop behavior.
type ReflexionConfig struct {
	// Enabled enables the self-critique step.
	Enabled bool

	// MaxRounds is the maximum number of critique-revise cycles (default: 2).
	MaxRounds int

	// CriticPrompt is the system prompt for the critic.
	// If empty, a default prompt is used.
	CriticPrompt string
}

// DefaultReflexionConfig returns sensible defaults for self-critique.
func DefaultReflexionConfig() ReflexionConfig {
	return ReflexionConfig{
		Enabled:    false,
		MaxRounds:  2,
		CriticPrompt: "",
	}
}

const reflexionCriticPrompt = `You are a critical reviewer of AI assistant responses. Your job is to identify problems in the assistant's draft response before it is sent to the user.

Review the conversation and the assistant's draft response for:
1. FACTUAL ERRORS — Does the response make claims that contradict the conversation history or tool outputs?
2. HALLUCINATIONS — Does the response mention things that weren't in the tools' actual output?
3. MISSING INFORMATION — Did the user ask for something the assistant didn't provide?
4. UNCLEAR EXPLANATIONS — Is any part confusing or ambiguous?
5. TOOL MISUSE — Did the assistant use the wrong tool or pass wrong parameters?

Format your critique as a JSON object with these fields:
{
  "has_issues": true/false,
  "issues": ["short description of each issue"],
  "suggestion": "brief suggestion for fixing the main issue"
}

Only flag genuine problems. If the response is correct and clear, set has_issues to false.
Do NOT be overly critical — minor phrasing differences are not issues.`

const reflexionRevisionPrompt = `The reviewer identified the following issues with your response:

%s

Please revise your response to address these issues. Be concise — fix the problem without over-explaining.`

// RunReflexion runs the self-critique loop on the agent's last response.
func (a *Agent) RunReflexion(ctx context.Context, messages []llm.Message) (revisedContent string, didRevise bool, err error) {
	if !a.reflexion.Enabled || a.reflexion.MaxRounds <= 0 {
		return lastAssistantContent(messages), false, nil
	}

	round := 0
	currentMsgs := copyMessages(messages)
	draftContent := lastAssistantContent(currentMsgs)
	currentContent := draftContent

	maxRounds := a.reflexion.MaxRounds
	if maxRounds <= 0 {
		maxRounds = 2
	}

	for round < maxRounds {
		round++

		critique, err := a.runCritique(ctx, currentMsgs, currentContent)
		if err != nil {
			return currentContent, didRevise, nil
		}

		if !critique.hasIssues {
			return currentContent, didRevise, nil
		}

		didRevise = true

		revisionContent := fmt.Sprintf(reflexionRevisionPrompt, strings.Join(critique.issues, "\n- "))

		revMsgs := make([]llm.Message, len(currentMsgs))
		copy(revMsgs, currentMsgs)
		revMsgs = append(revMsgs, llm.Message{
			Role:    llm.RoleUser,
			Content: revisionContent,
		})

		ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		resp, err := a.provider.Chat(ctx, llm.ChatRequest{
			Model:       a.model,
			Messages:    revMsgs,
			MaxTokens:   a.maxTokens,
			Temperature: a.temperature,
		})
		cancel()

		if err != nil {
			return currentContent, didRevise, nil
		}

		currentContent = resp.Content
		if currentContent == "" {
			currentContent = draftContent
			break
		}

		currentMsgs = revMsgs
	}

	return currentContent, didRevise, nil
}

type critiqueResult struct {
	hasIssues  bool
	issues     []string
	suggestion string
}

func (a *Agent) runCritique(ctx context.Context, msgs []llm.Message, draftContent string) (*critiqueResult, error) {
	criticPrompt := a.reflexion.CriticPrompt
	if criticPrompt == "" {
		criticPrompt = reflexionCriticPrompt
	}

	criticMsgs := []llm.Message{
		{Role: llm.RoleSystem, Content: criticPrompt},
	}

	start := 0
	if len(msgs) > 10 {
		start = len(msgs) - 10
	}
	criticMsgs = append(criticMsgs, msgs[start:]...)

	criticMsgs = append(criticMsgs, llm.Message{
		Role:    llm.RoleUser,
		Content: fmt.Sprintf("Review this assistant response:\n\n%s", draftContent),
	})

	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	resp, err := a.provider.Chat(ctx, llm.ChatRequest{
		Model:       a.model,
		Messages:    criticMsgs,
		MaxTokens:   1024,
		Temperature: 0.2,
	})

	if err != nil {
		return &critiqueResult{hasIssues: false}, nil
	}

	result, parseErr := parseCritiqueResponse(resp.Content)
	if parseErr != nil {
		lower := strings.ToLower(resp.Content)
		hasIssues := strings.Contains(lower, "has_issues") ||
			strings.Contains(lower, "issue:") ||
			strings.Contains(lower, "problem:") ||
			strings.Contains(lower, "error:")
		if hasIssues {
			return &critiqueResult{
				hasIssues: true,
				issues:    []string{"critic flagged potential issues"},
			}, nil
		}
		return &critiqueResult{hasIssues: false}, nil
	}

	return result, nil
}

func parseCritiqueResponse(content string) (*critiqueResult, error) {
	content = strings.TrimSpace(content)

	jsonStart := strings.Index(content, "{")
	jsonEnd := strings.LastIndex(content, "}")

	if jsonStart >= 0 && jsonEnd > jsonStart {
		jsonStr := content[jsonStart : jsonEnd+1]
		var parsed struct {
			HasIssues  bool     `json:"has_issues"`
			Issues     []string `json:"issues"`
			Suggestion string   `json:"suggestion"`
		}
		if err := json.Unmarshal([]byte(jsonStr), &parsed); err == nil {
			if parsed.Issues == nil {
				parsed.Issues = []string{}
			}
			return &critiqueResult{
				hasIssues:  parsed.HasIssues,
				issues:     parsed.Issues,
				suggestion: parsed.Suggestion,
			}, nil
		}
	}

	return nil, fmt.Errorf("no JSON in critique response")
}

func lastAssistantContent(msgs []llm.Message) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == llm.RoleAssistant && msgs[i].Content != "" {
			return msgs[i].Content
		}
	}
	return ""
}

func copyMessages(msgs []llm.Message) []llm.Message {
	out := make([]llm.Message, len(msgs))
	copy(out, msgs)
	return out
}
