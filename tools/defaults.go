package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"strings"

	"github.com/syawalqi/oryx/executor"
)

// RegisterDefaults adds ORYX's standard set of tools to the registry.
// The executor is shared across all tools that need shell access.
func RegisterDefaults(r *Registry, exec *executor.Executor) {
	r.Register(Definition{
		Name:        "run_command",
		Description: "Execute a shell command on the server. Use for system administration, package management, file operations, git, and any command-line task.",
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
		BlockInPlan: true,
		MaxCalls:    0,
		Handler: func(ctx context.Context, args json.RawMessage) (string, error) {
			var p struct {
				Command string `json:"command"`
			}
			if err := json.Unmarshal(args, &p); err != nil {
				return "", fmt.Errorf("invalid args: %w", err)
			}
			result, err := exec.Run(ctx, p.Command)
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
		},
	})

	r.Register(Definition{
		Name:        "read_file",
		Description: "Read the contents of a file. Returns file content or an error message.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path": map[string]interface{}{
					"type":        "string",
					"description": "Absolute path to the file to read",
				},
			},
			"required": []string{"path"},
		},
		BlockInPlan: false,
		MaxCalls:    0,
		Handler: func(ctx context.Context, args json.RawMessage) (string, error) {
			var p struct {
				Path string `json:"path"`
			}
			if err := json.Unmarshal(args, &p); err != nil {
				return "", fmt.Errorf("invalid args: %w", err)
			}
			data, err := os.ReadFile(p.Path)
			if err != nil {
				return fmt.Sprintf("error: %v", err), nil
			}
			lines := strings.Split(string(data), "\n")
			if len(lines) > 500 {
				lines = lines[:500]
			}
			return strings.Join(lines, "\n"), nil
		},
	})

	r.Register(Definition{
		Name:        "write_file",
		Description: "Create or overwrite a file with content. Creates parent directories if needed.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path": map[string]interface{}{
					"type":        "string",
					"description": "Absolute path to the file to write",
				},
				"content": map[string]interface{}{
					"type":        "string",
					"description": "Content to write to the file",
				},
			},
			"required": []string{"path", "content"},
		},
		BlockInPlan: true,
		MaxCalls:    0,
		Handler: func(ctx context.Context, args json.RawMessage) (string, error) {
			var p struct {
				Path    string `json:"path"`
				Content string `json:"content"`
			}
			if err := json.Unmarshal(args, &p); err != nil {
				return "", fmt.Errorf("invalid args: %w", err)
			}
			dir := path.Dir(p.Path)
			if err := os.MkdirAll(dir, 0755); err != nil {
				return fmt.Sprintf("error: %v", err), nil
			}
			if err := os.WriteFile(p.Path, []byte(p.Content), 0644); err != nil {
				return fmt.Sprintf("error: %v", err), nil
			}
			return fmt.Sprintf("wrote %s (%d bytes)", p.Path, len(p.Content)), nil
		},
	})

	r.Register(Definition{
		Name:        "service_action",
		Description: "Start, stop, restart, or reload a systemd service. Also supports status checks.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"action": map[string]interface{}{
					"type": "string",
					"enum": []string{"start", "stop", "restart", "reload", "status"},
				},
				"service": map[string]interface{}{
					"type":        "string",
					"description": "Systemd service name (without .service suffix)",
				},
			},
			"required": []string{"action", "service"},
		},
		BlockInPlan: true,
		MaxCalls:    0,
		Handler: func(ctx context.Context, args json.RawMessage) (string, error) {
			var p struct {
				Action  string `json:"action"`
				Service string `json:"service"`
			}
			if err := json.Unmarshal(args, &p); err != nil {
				return "", fmt.Errorf("invalid args: %w", err)
			}
			if p.Action == "status" {
				result, err := exec.Run(ctx, fmt.Sprintf("systemctl status %s --no-pager -l 2>&1 | head -30", p.Service))
				if err != nil {
					return fmt.Sprintf("error: %v", result.Stderr), nil
				}
				return result.Stdout, nil
			}
			result, err := exec.Run(ctx, fmt.Sprintf("systemctl %s %s 2>&1", p.Action, p.Service))
			if err != nil {
				return fmt.Sprintf("error: %v", result.Stderr), nil
			}
			return fmt.Sprintf("service %s %s: %s", p.Service, p.Action, result.Stdout), nil
		},
	})

	r.Register(Definition{
		Name:        "search_logs",
		Description: "Search systemd journal logs. Optionally filter by service unit, priority, and line count.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"unit": map[string]interface{}{
					"type":        "string",
					"description": "Service unit name to filter by (optional)",
				},
				"priority": map[string]interface{}{
					"type":        "string",
					"description": "Log priority level: emerg, alert, crit, err, warning, info",
				},
				"lines": map[string]interface{}{
					"type":        "integer",
					"description": "Number of recent log lines to return (max 200, default 50)",
				},
			},
		},
		BlockInPlan: false,
		MaxCalls:    0,
		Handler: func(ctx context.Context, args json.RawMessage) (string, error) {
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
			result, err := exec.Run(ctx, cmd)
			if err != nil {
				return fmt.Sprintf("error: %v", err), nil
			}
			return result.Stdout, nil
		},
	})
}
