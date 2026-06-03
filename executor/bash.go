package executor

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

type Result struct {
	Stdout    string `json:"stdout"`
	Stderr    string `json:"stderr"`
	ExitCode  int    `json:"exit_code"`
	Truncated bool   `json:"truncated"`
	Duration  string `json:"duration"`
}

type Executor struct {
	timeout       time.Duration
	maxOutputLines int
	blocked       []string
}

func New(timeoutSec, maxOutputLines int, blocked []string) *Executor {
	return &Executor{
		timeout:       time.Duration(timeoutSec) * time.Second,
		maxOutputLines: maxOutputLines,
		blocked:       blocked,
	}
}

func (e *Executor) isBlocked(cmd string) bool {
	lower := strings.ToLower(cmd)
	for _, b := range e.blocked {
		if strings.Contains(lower, strings.ToLower(b)) {
			return true
		}
	}
	return false
}

func (e *Executor) Run(ctx context.Context, command string) (*Result, error) {
	if e.isBlocked(command) {
		return &Result{
			Stdout:   "",
			Stderr:   "blocked: command matched blocklist",
			ExitCode: -1,
		}, nil
	}

	ctx, cancel := context.WithTimeout(ctx, e.timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "bash", "-c", command)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	start := time.Now()
	err := cmd.Run()
	duration := time.Since(start)

	exitCode := 0
	if err != nil {
		if ctx.Err() != nil {
			exitCode = -1 // timeout
		} else if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = -2 // other error
		}
	}

	out := stdout.String()
	errOut := stderr.String()
	truncated := false

	outLines := strings.Split(out, "\n")
	if len(outLines) > e.maxOutputLines {
		outLines = outLines[:e.maxOutputLines]
		outLines = append(outLines, fmt.Sprintf("... [truncated at %d lines]", e.maxOutputLines))
		out = strings.Join(outLines, "\n")
		truncated = true
	}

	return &Result{
		Stdout:    out,
		Stderr:    errOut,
		ExitCode:  exitCode,
		Truncated: truncated,
		Duration:  duration.Round(time.Millisecond).String(),
	}, nil
}
