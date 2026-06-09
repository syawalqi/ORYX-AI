// Package mcpclient implements MCP client-to-registry wiring.
// ConnectToRegistry connects to MCP servers and registers their tools
// as first-class ORYX tool definitions.
package mcpclient

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/syawalqi/oryx/tools"
)

// ServerConfig describes a single MCP server to connect to.
type ServerConfig struct {
	Command string   `json:"command"`
	Args    []string `json:"args"`
}

// RegisterAll connects to MCP servers and registers their tools with the ORYX registry.
// Each MCP tool gets wrapped as a tools.Definition with "mcp_" prefix.
// Returns a cleanup function that closes all MCP connections.
func RegisterAll(ctx context.Context, reg *tools.Registry, servers []ServerConfig) (cleanup func(), errs []error) {
	var handlers []*MCPToolHandler

	for _, srv := range servers {
		handler, err := NewMCPToolHandler(ctx, srv.Command, srv.Args...)
		if err != nil {
			errs = append(errs, fmt.Errorf("mcp %s %v: %w", srv.Command, srv.Args, err))
			continue
		}
		handlers = append(handlers, handler)

		for _, mcpTool := range handler.Tools {
			toolName := "mcp_" + mcpTool.Name
			toolDesc := mcpTool.Description
			if toolDesc == "" {
				toolDesc = fmt.Sprintf("MCP tool: %s (from %s)", mcpTool.Name, srv.Command)
			}

			// Convert inputSchema to parameters map
			params := make(map[string]interface{})
			if mcpTool.InputSchema != nil {
				if schema, ok := mcpTool.InputSchema.(map[string]interface{}); ok {
					params = schema
				} else {
					// Wrap non-map schemas
					data, _ := json.Marshal(mcpTool.InputSchema)
					json.Unmarshal(data, &params)
				}
			}

			// Ensure required fields exist
			if _, hasType := params["type"]; !hasType {
				params["type"] = "object"
			}
			if _, hasProps := params["properties"]; !hasProps {
				params["properties"] = map[string]interface{}{}
			}
			if _, hasReq := params["required"]; !hasReq {
				params["required"] = []string{}
			}

			clientRef := handler.Client
			toolNameCopy := mcpTool.Name

			// Try to register — if already exists, skip
			func(name string) {
				defer func() {
					if r := recover(); r != nil {
						// Tool already registered, skip silently
					}
				}()
				reg.Register(tools.Definition{
					Name:        toolName,
					Description: toolDesc,
					Parameters:  params,
					BlockInPlan: true,
					MaxCalls:    0,
					Handler: func(ctx context.Context, args json.RawMessage) (string, error) {
						// Parse args into a map for the MCP call
						var argsMap map[string]interface{}
						if err := json.Unmarshal(args, &argsMap); err != nil {
							// Pass raw args directly
							return clientRef.CallTool(ctx, toolNameCopy, args)
						}
						return clientRef.CallTool(ctx, toolNameCopy, argsMap)
					},
				})
			}(toolName)
		}
	}

	cleanup = func() {
		for _, h := range handlers {
			h.Client.Close()
		}
	}
	return cleanup, errs
}
