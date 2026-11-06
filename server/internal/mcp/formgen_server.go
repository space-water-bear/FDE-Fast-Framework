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
)

const formgenMCPName = "fde-admin-formgen"

// formgenToolDefs 三个工具的 name/description/JSON Schema 入参, 与
// docs/form-codegen-design.md 6.1 / 6.2 / 6.3 保持一致。
var formgenToolDefs = []*mcpsdk.Tool{
	{
		Name:        "generate_form_schema",
		Description: "根据业务对象生成 FDE Form Schema(JSON Schema 子集 + x-ui/x-linkages)",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"objectKey", "name", "fields"},
			"properties": map[string]any{
				"objectKey": map[string]any{"type": "string", "pattern": "^[a-z][a-z0-9_]{0,63}$"},
				"name":      map[string]any{"type": "string", "maxLength": float64(64)},
				"fields": map[string]any{
					"type": "array", "minItems": float64(1),
					"items": map[string]any{"$ref": "#/$defs/fieldDef"},
				},
				"relations": map[string]any{"type": "array", "items": map[string]any{
					"type":     "object",
					"required": []string{"field", "targetObject", "kind"},
					"properties": map[string]any{
						"field":        map[string]any{"type": "string"},
						"targetObject": map[string]any{"type": "string"},
						"kind":         map[string]any{"enum": []string{"many_to_one", "one_to_many"}},
					},
				}},
				"mode":   map[string]any{"enum": []string{"create", "update"}},
				"dryRun": map[string]any{"type": "boolean", "default": true},
			},
			"$defs": map[string]any{
				"fieldDef": map[string]any{
					"type":     "object",
					"required": []string{"name", "type"},
					"properties": map[string]any{
						"name":     map[string]any{"type": "string"},
						"title":    map[string]any{"type": "string"},
						"type":     map[string]any{"enum": []string{"string", "text", "int", "float", "bool", "datetime", "enum", "relation"}},
						"required": map[string]any{"type": "boolean"},
						"rules":    map[string]any{"type": "object"},
						"x-ui":     map[string]any{"type": "object"},
					},
				},
			},
		},
	},
	{
		Name:        "generate_business_code",
		Description: "基于统一业务对象模型生成 CRUD 代码(dry-run 预览, 不写盘)",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"objectKey", "business"},
			"properties": map[string]any{
				"objectKey":    map[string]any{"type": "string"},
				"business":     map[string]any{"type": "string", "maxLength": float64(64)},
				"tableName":    map[string]any{"type": "string", "pattern": "^[a-z][a-z0-9_]{0,63}$"},
				"structName":   map[string]any{"type": "string"},
				"parentMenuId": map[string]any{"type": "integer"},
				"fields": map[string]any{
					"type":  "array",
					"items": map[string]any{"$ref": "#/$defs/fieldDef"},
				},
				"dryRun": map[string]any{"type": "boolean", "default": true},
			},
			"$defs": map[string]any{
				"fieldDef": map[string]any{
					"type":     "object",
					"required": []string{"name", "type"},
					"properties": map[string]any{
						"name":     map[string]any{"type": "string"},
						"title":    map[string]any{"type": "string"},
						"type":     map[string]any{"enum": []string{"string", "text", "int", "float", "bool", "datetime"}},
						"search":   map[string]any{"type": "boolean"},
						"inList":   map[string]any{"type": "boolean"},
						"inForm":   map[string]any{"type": "boolean"},
						"required": map[string]any{"type": "boolean"},
					},
				},
			},
		},
	},
	{
		Name:        "apply_files",
		Description: "确认写入 dry-run 批次文件并执行快照/菜单授权/casbin 重建(approve 必须显式 true)",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"batchId", "approve"},
			"properties": map[string]any{
				"batchId":   map[string]any{"type": "string", "description": "dry-run 返回的批次号"},
				"objectKey": map[string]any{"type": "string"},
				"files": map[string]any{"type": "array", "items": map[string]any{
					"type":     "object",
					"required": []string{"path", "content"},
					"properties": map[string]any{
						"path":    map[string]any{"type": "string"},
						"content": map[string]any{"type": "string"},
					},
				}},
				"applyMenu":    map[string]any{"type": "boolean", "default": true},
				"parentMenuId": map[string]any{"type": "integer"},
				"snapshot":     map[string]any{"type": "boolean", "default": true},
				"approve":      map[string]any{"const": true, "description": "必须显式为 true 才执行写盘"},
			},
		},
	},
}

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
