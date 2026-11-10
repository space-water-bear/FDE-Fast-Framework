package main

import (
	"fde-admin/internal/toolcontract"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// The thin gateway shares JSON protocols without importing the native MCP or service packages.
var toolDefs = func() []*mcpsdk.Tool {
	var tools []*mcpsdk.Tool
	for _, definition := range toolcontract.Definitions(true) {
		tools = append(tools, &mcpsdk.Tool{Name: definition.Name, Description: definition.Description, InputSchema: definition.Schema})
	}
	return tools
}()
