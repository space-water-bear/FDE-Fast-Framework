// Package toolcontract owns the JSON protocols shared by seeded native tools and MCP adapters.
package toolcontract

import "encoding/json"

type Definition struct {
	Name        string
	Description string
	Schema      map[string]any
}

// Definitions returns fresh maps so one adapter cannot mutate another's contract.
// Service-only MCP adapters never advertise permission to persist a schema.
func Definitions(serviceOnly bool) []Definition {
	definitions := []Definition{
		{Name: "generate_form_schema", Description: "生成完整 FDE Form Schema 预览；具有已验证用户/项目身份时 dryRun=false 只保存草稿。更新需 mode=update 与 expectedVersion；激活通过独立用户入口。", Schema: decode(formSchema)},
		{Name: "generate_business_code", Description: "生成 CRUD 代码预览批次，不写盘；仅支持 dryRun=true。需要已验证用户/项目身份，缺少身份返回 IDENTITY_REQUIRED；写盘必须在预览页面由用户确认批次。", Schema: decode(businessSchema)},
		{Name: "apply_files", Description: "兼容工具名：调用始终返回 APPROVAL_REQUIRED，不执行写盘。approve 等模型参数不能授权；请由用户在代码预览页面确认已验证批次。", Schema: decode(applySchema)},
	}
	if serviceOnly {
		definitions[0].Description = "只预览完整 FDE Form Schema，不保存、不激活；MCP 服务身份没有用户写权限。支持 schema 整图或字段数组、关系、联动和字段元数据。"
		definitions[0].Schema["properties"].(map[string]any)["dryRun"] = map[string]any{"type": "boolean", "const": true, "default": true, "description": "服务身份仅允许预览"}
		definitions[1].Description = "仅代码预览，不能写盘；需要已验证用户/项目身份，当前纯 MCP 服务会话返回 IDENTITY_REQUIRED。请在已登录应用中预览并确认批次。"
	}
	return definitions
}

func decode(raw string) map[string]any {
	var schema map[string]any
	if err := json.Unmarshal([]byte(raw), &schema); err != nil {
		panic(err)
	}
	return schema
}

const formSchema = `{
 "type":"object",
 "anyOf":[{"required":["schema"]},{"required":["objectKey","name","fields"]}],
 "properties":{
  "objectKey":{"type":"string","pattern":"^[a-z][a-z0-9_]{0,63}$"},
  "name":{"type":"string","maxLength":64},
  "tableName":{"type":"string"},
  "remark":{"type":"string"},
  "schema":{"type":"object","description":"完整 schema；保留未知扩展。fields 使用 JSON Schema 类型，x-fde.type 保留业务类型与 searchable/inList/inForm/sort 元数据。","required":["objectKey","name","fields"],"properties":{"objectKey":{"type":"string"},"name":{"type":"string"},"fields":{"type":"object"},"x-relations":{"type":"array"},"x-linkages":{"type":"array"}}},
  "fields":{"type":"array","minItems":1,"items":{"$ref":"#/$defs/fieldDef"}},
  "relations":{"type":"array","items":{"type":"object","required":["field","targetObject","kind"],"properties":{"field":{"type":"string"},"targetObject":{"type":"string"},"kind":{"enum":["many_to_one","one_to_many"]}}}},
  "linkages":{"type":"array","items":{"type":"object","required":["when","then"],"properties":{"when":{"type":"object"},"then":{"type":"object"}}}},
  "mode":{"enum":["create","update"],"default":"create"},
  "expectedVersion":{"type":"integer","minimum":1,"description":"更新草稿所读取的最新 version；版本已变化会拒绝"},
  "dryRun":{"type":"boolean","default":true,"description":"false 需要真实用户/项目身份，仅保存草稿"}
 },
 "allOf":[{"if":{"properties":{"mode":{"const":"update"}},"required":["mode"]},"then":{"required":["expectedVersion"]}}],
 "$defs":{"fieldDef":{"type":"object","required":["name","type"],"properties":{
  "name":{"type":"string"},"title":{"type":"string"},
  "type":{"enum":["string","text","int","float","bool","datetime","enum","relation"]},
  "required":{"type":"boolean"},"searchable":{"type":"boolean"},"inList":{"type":"boolean"},"inForm":{"type":"boolean"},"sort":{"type":"integer"},
  "rules":{"type":"object"},"x-ui":{"type":"object"}
 }}}
}`

const businessSchema = `{
 "type":"object",
 "anyOf":[{"required":["objectKey"]},{"required":["business","tableName","fields"]}],
 "properties":{
  "objectKey":{"type":"string","minLength":1,"description":"项目内业务对象键；提供后使用保存的草稿字段"},
  "business":{"type":"string","maxLength":64},"tableName":{"type":"string","pattern":"^[a-z][a-z0-9_]{0,63}$"},
  "structName":{"type":"string"},"parentMenuId":{"type":"integer"},
  "fields":{"type":"array","items":{"$ref":"#/$defs/fieldDef"}},
  "dryRun":{"type":"boolean","const":true,"default":true,"description":"仅预览；false 不能批准写盘"}
 },
 "$defs":{"fieldDef":{"type":"object","required":["name","type"],"properties":{
  "name":{"type":"string"},"title":{"type":"string"},"type":{"enum":["string","text","int","float","bool","datetime"]},
  "search":{"type":"boolean"},"inList":{"type":"boolean"},"inForm":{"type":"boolean"},"required":{"type":"boolean"}
 }}}
}`

const applySchema = `{
 "type":"object","required":["batchId"],
 "properties":{
  "batchId":{"type":"string","description":"预览批次号；工具不会执行此批次"},
  "approve":{"type":"boolean","description":"旧客户端兼容参数；true 也不能授权写盘"}
 }
}`
