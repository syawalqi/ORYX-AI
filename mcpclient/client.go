// Package mcpclient implements a Model Context Protocol (MCP) client.
// MCP is an open standard (JSON-RPC 2.0) for connecting AI agents to
// external tools and data sources. Servers can run over stdio or HTTP.
//
// See: https://modelcontextprotocol.io
package mcpclient

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
)

// ToolDefinition mirrors the MCP tool schema for registration with the ORYX registry.
type ToolDefinition struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	InputSchema interface{} `json:"inputSchema"`
}

// Client connects to an MCP server and exposes its tools.
type Client struct {
	mu       sync.Mutex
	cmd      *exec.Cmd
	stdin    io.WriteCloser
	stdout   *bufio.Scanner
	seq      int
	handlers map[int]chan json.RawMessage
}

// ConnectStdio starts an MCP server as a subprocess and connects via stdio.
func ConnectStdio(ctx context.Context, command string, args ...string) (*Client, error) {
	cmd := exec.CommandContext(ctx, command, args...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start: %w", err)
	}

	c := &Client{
		cmd:      cmd,
		stdin:    stdin,
		stdout:   bufio.NewScanner(stdout),
		handlers: make(map[int]chan json.RawMessage),
	}

	// Background reader goroutine
	go c.readLoop()

	return c, nil
}

// Close terminates the MCP server process.
func (c *Client) Close() error {
	c.stdin.Close()
	return c.cmd.Wait()
}

// ListTools calls the MCP tools/list endpoint and returns available tools.
func (c *Client) ListTools(ctx context.Context) ([]ToolDefinition, error) {
	raw, err := c.call(ctx, "tools/list", nil)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Tools []ToolDefinition `json:"tools"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("parse tools/list: %w", err)
	}
	return resp.Tools, nil
}

// CallTool invokes a tool on the MCP server with the given arguments.
func (c *Client) CallTool(ctx context.Context, name string, args interface{}) (string, error) {
	raw, err := c.call(ctx, "tools/call", map[string]interface{}{
		"name":      name,
		"arguments": args,
	})
	if err != nil {
		return "", err
	}

	var resp struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return "", fmt.Errorf("parse tools/call: %w", err)
	}

	var texts []string
	for _, c := range resp.Content {
		if c.Text != "" {
			texts = append(texts, c.Text)
		}
	}
	output := strings.Join(texts, "\n")
	if resp.IsError {
		return "", fmt.Errorf("mcp tool error: %s", output)
	}
	return output, nil
}

// call sends a JSON-RPC request and waits for the response.
func (c *Client) call(ctx context.Context, method string, params interface{}) (json.RawMessage, error) {
	c.mu.Lock()
	c.seq++
	id := c.seq
	ch := make(chan json.RawMessage, 1)
	c.handlers[id] = ch
	c.mu.Unlock()

	defer func() {
		c.mu.Lock()
		delete(c.handlers, id)
		c.mu.Unlock()
	}()

	req := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  method,
		"params":  params,
	}

	data, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	if _, err := c.stdin.Write(append(data, '\n')); err != nil {
		return nil, fmt.Errorf("write request: %w", err)
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case result := <-ch:
		return result, nil
	}
}

// readLoop processes JSON-RPC responses from the server.
func (c *Client) readLoop() {
	for c.stdout.Scan() {
		line := c.stdout.Text()
		var msg struct {
			ID     int              `json:"id"`
			Result json.RawMessage  `json:"result"`
			Error  *struct {
				Message string `json:"message"`
			} `json:"error,omitempty"`
		}
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			continue
		}

		c.mu.Lock()
		ch, ok := c.handlers[msg.ID]
		c.mu.Unlock()

		if ok && ch != nil {
			if msg.Error != nil {
				ch <- json.RawMessage(fmt.Sprintf(`{"error":%q}`, msg.Error.Message))
			} else {
				ch <- msg.Result
			}
			close(ch)
		}
	}
}

// MCPToolHandler wraps an MCP client into a function suitable for ORYX's
// ToolRegistry. It connects to an MCP server and provides tool definitions.
type MCPToolHandler struct {
	Client *Client
	Tools  []ToolDefinition
}

// NewMCPToolHandler connects to an MCP server and returns tools for the registry.
func NewMCPToolHandler(ctx context.Context, command string, args ...string) (*MCPToolHandler, error) {
	client, err := ConnectStdio(ctx, command, args...)
	if err != nil {
		return nil, err
	}

	tools, err := client.ListTools(ctx)
	if err != nil {
		client.Close()
		return nil, err
	}

	return &MCPToolHandler{
		Client: client,
		Tools:  tools,
	}, nil
}
