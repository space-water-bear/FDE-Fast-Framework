// Package mcp 提供表单生成器与代码生成器的 MCP 服务端。
//
// 运行模式: streamable HTTP(与 server/internal/service/ai.go 中 MCP 客户端
// 支持的 "streamable" 传输保持一致)。
//
// 挂载点: 如需随主服务对外暴露, 在 server/internal/router/router.go 的
// NewRouter() 中挂载(注意 MCP 客户端一般不携带业务 JWT, 需自行决定鉴权策略):
//
//	r.Any("/mcp/formgen", mcp.FormgenHandler())
//
// 当前不挂路由, 避免把未鉴权的写盘工具直接暴露到公网; 测试与内网部署可
// 直接使用 FormgenHandler() 挂到任意 http.ServeMux / gin.Engine。
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"fde-admin/internal/service"
	"fde-admin/internal/toolcontract"
)

const formgenMCPName = "fde-admin-formgen"

// formgenToolDefs advertises the service-only variant of the shared protocol.
var formgenToolDefs = func() []*mcpsdk.Tool {
	var tools []*mcpsdk.Tool
	for _, definition := range toolcontract.Definitions(true) {
		tools = append(tools, &mcpsdk.Tool{Name: definition.Name, Description: definition.Description, InputSchema: definition.Schema})
	}
	return tools
}()

// formgenToolFuncs 工具名到 service 层 AiToolFunc 的映射, 复用
// service/tool_registry.go 中注册的同一批实现(GenerateFormSchema /
// GenerateBusinessCode / ApplyFiles)。
var formgenToolFuncs = map[string]service.AiToolFunc{
	"generate_form_schema":   service.GenerateFormSchema,
	"generate_business_code": service.GenerateBusinessCode,
	"apply_files":            service.ApplyFiles,
}

// NewFormgenServer 构建包含三个 formgen/codegen 工具的 MCP server。
func NewFormgenServer() *mcpsdk.Server {
	srv := mcpsdk.NewServer(&mcpsdk.Implementation{Name: formgenMCPName, Version: "1.0"}, nil)
	for _, t := range formgenToolDefs {
		srv.AddTool(t, callFormgenTool)
	}
	return srv
}

// FormgenHandler 返回 streamable HTTP 处理器, 每次会话复用同一个 server。
func FormgenHandler() http.Handler {
	return mcpsdk.NewStreamableHTTPHandler(func(*http.Request) *mcpsdk.Server {
		return NewFormgenServer()
	}, nil)
}

// callFormgenTool tools/call 统一入口: 按 name 分发到 service 实现, 未注册的工具名返回明确错误。
func callFormgenTool(ctx context.Context, req *mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	name := req.Params.Name
	fn, ok := formgenToolFuncs[name]
	if !ok {
		return &mcpsdk.CallToolResult{
			IsError: true,
			Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: fmt.Sprintf("工具未找到: %s", name)}},
		}, nil
	}
	result, err := fn(ctx, string(req.Params.Arguments))
	if err != nil {
		return &mcpsdk.CallToolResult{
			IsError: true,
			Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: "工具执行失败: " + err.Error()}},
		}, nil
	}
	b, err := json.Marshal(result)
	if err != nil {
		return &mcpsdk.CallToolResult{
			IsError: true,
			Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: "结果序列化失败: " + err.Error()}},
		}, nil
	}
	return &mcpsdk.CallToolResult{Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: string(b)}}}, nil
}
