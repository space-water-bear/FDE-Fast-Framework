// mock MCP server: stdio 传输, 提供 mcp_echo 工具, 用于本地验证 MCP 服务接入链路
// 构建: go build -o bin/mock-mcp ./testdata/mcp-mock
package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func main() {
	srv := mcp.NewServer(&mcp.Implementation{Name: "mock-mcp", Version: "1.0.0"}, nil)
	srv.AddTool(&mcp.Tool{
		Name:        "mcp_echo",
		Description: "回声工具: 原样返回调用参数(用于测试 MCP 工具调用链路)",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"text": map[string]any{"type": "string", "description": "要回显的文本"},
			},
		},
	}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: "echo: " + string(req.Params.Arguments)}},
		}, nil
	})

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := srv.Run(ctx, &mcp.StdioTransport{}); err != nil {
		log.Fatalf("mock mcp server: %v", err)
	}
}
