package main

import mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

// toolDefs 是网关对外暴露的三个工具, name/description/InputSchema 与
// docs/form-codegen-design.md 6.1 / 6.2 / 6.3 保持一致(独立抄写维护,
// 不得 import server/internal/mcp)。
var toolDefs = []*mcpsdk.Tool{
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
					"type":     "array",
					"minItems": float64(1),
					"items":    map[string]any{"$ref": "#/$defs/fieldDef"},
				},
				"relations": map[string]any{
					"type": "array",
					"items": map[string]any{
						"type":     "object",
						"required": []string{"field", "targetObject", "kind"},
						"properties": map[string]any{
							"field":        map[string]any{"type": "string"},
							"targetObject": map[string]any{"type": "string"},
							"kind":         map[string]any{"enum": []string{"many_to_one", "one_to_many"}},
						},
					},
				},
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
				"files": map[string]any{
					"type": "array",
					"items": map[string]any{
						"type":     "object",
						"required": []string{"path", "content"},
						"properties": map[string]any{
							"path":    map[string]any{"type": "string"},
							"content": map[string]any{"type": "string"},
						},
					},
				},
				"applyMenu":    map[string]any{"type": "boolean", "default": true},
				"parentMenuId": map[string]any{"type": "integer"},
				"snapshot":     map[string]any{"type": "boolean", "default": true},
				"approve":      map[string]any{"const": true, "description": "必须显式为 true 才执行写盘"},
			},
		},
	},
}
