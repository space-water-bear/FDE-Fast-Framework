# 表单生成器与代码生成器设计文档

> 版本 v1.0 ｜ 仓库 fde-admin ｜ 前置：README.md、docs/api-conventions.md、docs/agent-framework-go.md、docs/similar-projects-form-codegen.md

## 1. 目标与范围

- 表单生成器：智能体产出 JSON 表单定义（Schema 驱动动态表单），前端通用渲染器实时渲染；不写源码、无需重启、可增量修改。
- 代码生成器：智能体基于统一业务对象模型产出 CRUD 代码，复用现有 Go 模板与「菜单/按钮/角色授权/casbin 重建」联动。
- 交付形态：MAF 内置工具 + MCP 双通道，共用同一 Go 实现；两段式 dry-run 预览/diff → 人工确认 → apply 写盘。
- 非目标：不实现拖拽式低代码平台、不引入 Redis/对象存储、不改动登录与令牌体系。

## 2. 总体架构

数据流（智能体 → tools → 生成器 → 渲染器/写盘）：

```text
用户/智能体(MAF Chat 或外部 MCP 客户端)
  │ 1. 绑定工具(sys_ai_agent_tools) / MCP tools/list
  ▼
tools 层: buildTools()/buildMcpTools() —— sys_ai_tools.Schema → tool.Tool
  │ 2. MAF ToolAutoCall 自动调用(或 MCP tools/call)
  ▼
生成器 service(共享 Go 实现, 同一注册表)
  ├─ generate_form_schema  ──► 业务对象模型(sys_biz_objects/sys_biz_fields)
  │                              └─► FDE Form Schema(JSON) ──► dry-run 预览/diff
  └─ generate_business_code ──► 对象元数据 → codegen.Prepare/Render(Go template)
                                 └─► files+diffs ──► dry-run 预览
  ▼
人工确认(前端预览页面或聊天内审批)
  ▼
apply_files: git/备份快照 → 写盘(server/internal/... + web/src/...) →
             创建菜单/按钮/角色授权 → RebuildCasbin → 审计落库(sys_gen_audits) → 失败回滚
  ▼
运行时: 前端 FormRender.vue 渲染表单 / vxe-table 列表页(新路由需重启后端生效)
```

分层归属：生成/写盘逻辑 server/internal/service/；模板 server/internal/codegen/templates.go；工具注册 server/internal/service/tool_registry.go；MCP 暴露 server/internal/mcp/；渲染器 web/src/components/form-render/。

## 3. 业务对象模型（统一事实源）

唯一事实源：sys_biz_objects + sys_biz_fields，表单 schema 与 CRUD 代码都由它投影。Go 结构体（新增 server/internal/model/biz_object.go）：

```go
type BizObject struct {
    Base
    Key       string `json:"key" gorm:"size:64;uniqueIndex"`   // 业务键, 如 biz_customer
    Name      string `json:"name" gorm:"size:128"`             // 中文名, 如 客户
    TableName string `json:"tableName" gorm:"size:128"`        // 落库表名, 与 codegen 一致
    Remark    string `json:"remark" gorm:"size:255"`
    Schema    string `json:"schema" gorm:"type:text"`          // 最新 FDE Form Schema(JSON)
    Version   int    `json:"version" gorm:"default:1"`
}
type BizField struct {
    Base
    ObjectID   uint   `json:"objectId" gorm:"index"`
    Name       string `json:"name"`      // 列名/字段名(合法标识符)
    Title      string `json:"title"`
    Type       string `json:"type"`      // string|text|int|float|bool|datetime|enum|relation
    Required   bool   `json:"required"`
    Searchable bool   `json:"searchable"`
    InList     bool   `json:"inList"`
    InForm     bool   `json:"inForm"`
    UIRules    string `json:"uiRules" gorm:"type:text"`  // 校验+UI 提示 JSON
    Relation   string `json:"relation" gorm:"type:text"` // {"kind":"many_to_one","target":"biz_order"}
    Sort       int    `json:"sort" gorm:"default:0"`
}
```

字段类型 → Go/TS/控件/校验 映射：

| type | Go | TS | 默认控件 | 校验 keywords 示例 |
|---|---|---|---|---|
| string | string | string | input | minLength/maxLength/pattern |
| text | string | string | textarea | maxLength |
| int | int | number | number | minimum/maximum |
| float | float64 | number | number | minimum/maximum |
| bool | bool | boolean | switch | — |
| datetime | time.Time | string | datetime | format:"date-time" |
| enum | string | string | select/radio | enum:["A","B"] |
| relation | uint / []uint | number / number[] | select/table-select | 见 Relation |

与现有代码生成器元数据的兼容与升级：

- 现有元数据 = codegen.GenRequest/GenField（server/internal/codegen/codegen.go:40-56），请求内即用即弃、无持久化。
- 升级路径：新增 ObjectToGenRequest(objectKey)（server/internal/service/formgen_service.go），把 sys_biz_fields 投影为 []codegen.GenField（Type 取 string/text/int/float/bool/datetime 子集；Searchable→Search、InList→InList、InForm→InForm、Required→Required）。
- 兼容：codegen.Prepare/Render 与 6 个模板（server/internal/codegen/templates.go）不改签名；enum/relation 字段暂不进入 CRUD 代码生成，仅进表单 schema（记 warning）。
- 回写：apply 时以对象为源回写 BizObject.Schema/Version，保证表单与代码同一版本基线。

## 4. 表单生成器设计

Schema 协议：FDE Form Schema = JSON Schema 子集（type/title/required/enum/minimum/maxLength 等）+ 两个扩展关键字 x-ui（控件与布局）与 x-linkages（联动）。示例：

```json
{
  "$schema": "https://fde-admin/schema/form-v1",
  "objectKey": "biz_customer", "name": "客户", "version": 2,
  "fields": {
    "name":    {"title":"客户名称","type":"string","required":true,"minLength":2,"maxLength":64,
                "x-ui":{"control":"input","placeholder":"请输入","colSpan":12}},
    "level":   {"title":"等级","type":"string","enum":["A","B","C"],
                "x-ui":{"control":"select","options":[{"label":"A级","value":"A"}]}},
    "balance": {"title":"余额","type":"number","minimum":0,"x-ui":{"control":"number","step":0.01}},
    "ownerId": {"title":"负责人","type":"integer","x-ui":{"control":"select","dict":"sys_user"}},
    "active":  {"title":"启用","type":"boolean","default":true,"x-ui":{"control":"switch"}}
  },
  "x-linkages": [
    {"when":{"field":"level","eq":"A"},"then":{"field":"balance","required":true,"visible":true}}
  ]
}
```

前端渲染器（新增 web/src/components/form-render/）：

- FormRender.vue：入参 schema、modelValue；遍历 fields，按 x-ui.control 从控件注册表取组件；将 JSON Schema 校验关键字转换为 Element Plus FormRules。
- controls.ts 注册表：input/textarea/number/switch/datetime/select/radio/table-select 与 x-ui.dict 字典数据加载（复用 /api/dict/data/list）。
- linkage.ts：监听字段变化，顺序执行 x-linkages 的 when/then（设置 required/visible/value），支持单层条件，禁止循环依赖（schema 校验时检测）。
- 列表/搜索复用 web/src/components/table-page/；渲染器不依赖服务端重启，/api/formgen/schema/:objectKey 拉取即渲染。

校验与联动约束：智能体产出的 schema 必须通过服务端校验——字段名合法（codegen.nameRe）、类型枚举合法、x-linkages 无环、必填字段存在——不合法直接 40001 拒绝。

可视化编辑器演进位：先交付「智能体生成 + FormRender 预览 + diff 确认」；P4 再在 web/src/views/system/formgen/ 实现 schema 可视化编辑（拖拽字段、配置 x-ui），编辑结果写回 sys_biz_objects.schema，与智能体生成共用同一渲染器。

## 5. 代码生成器设计

模板引擎：沿用 server/internal/codegen/codegen.go 的 Go text/template（定界符 [[ 与 ]]）+ go/format 校验；模板文件不变，仍为 6 个（server/internal/model|service|api|router/{snake}.go、web/src/api/{snake}.ts、web/src/views/generated/{snake}/index.vue，见 templates.go）。改造点：

- server/internal/codegen/codegen.go：新增 Diff(oldPath, newContent) 与 Snapshot() 辅助函数（不破坏现有 API）。
- server/internal/service/codegen.go：CodegenGenerate 拆为 CodegenDryRun（现 Preview）与 CodegenApply(dryRunID)，内部事务顺序见下。
- server/internal/api/codegen.go：新增 POST /api/formgen/apply（与 tools 的 apply_files 同实现）。

两段式写盘事务（预览 → apply → 菜单授权 → casbin 重建 → 回滚）：

```text
阶段1 dry-run:  Prepare → Render → 返回 files[] + diff(新增/修改/冲突) + menuPlan, 不落盘
阶段2 人工确认: 前端展示 diff, 用户点击确认(或聊天内 approve)
阶段3 apply(单事务边界):
  1. 快照: git add -A && git stash create 或备份 .fde-gen-backup/<batchId>/
  2. 写盘: writeGenFiles() 写 6 类文件(web/ 前缀 → cfg.WebDir)
  3. 菜单: createGeneratedMenu() 建 type=1 菜单 + 4 个 type=2 按钮(查询/新增/编辑/删除)
  4. 授权: admin + 拥有父菜单的角色 role_menus 关联
  5. 权限: RebuildCasbin() 全量重建
  6. 审计: sys_gen_audits 落库(结果/文件清单/快照号)
  失败回滚: 任一步失败 → 恢复快照文件 → 删除本次菜单/关联 → RebuildCasbin → 返回 40001+回滚详情
```

与现有 CodegenGenerate（service/codegen.go:57-88）的差别：dry-run 有批次号、apply 校验批次一致且可回滚；菜单同名/表已存在等冲突在 dry-run 阶段即报错。

## 6. tools 设计（MAF + MCP 双通道）

注册方式：工具 Go 实现注册到 server/internal/service/tool_registry.go 的 aiToolRegistry（RegisterAiToolFunc）；buildTools()（service/ai.go:581）按 sys_ai_tools.Schema 转 tool.Tool 并经 lookupAiToolFunc 找到执行函数；MCP 通道新增 server/internal/mcp/formgen_server.go，tools/list 返回同一批定义，tools/call 同样调用 lookupAiToolFunc。种子数据在 sys_ai_tools 写 3 行（name/schema/description），智能体绑定即生效；MCP 服务对外经 sys_ai_mcp_servers 配置或作为内置 server 挂载。部署形态见第 11 章决策记录第 5 条：MCP 采用独立薄网关进程（cmd/mcp-gateway）经 /api/mcp/* + x-api-key 调用主应用，主应用不直接对外暴露 MCP。

### 6.1 generate_form_schema

参数 JSON Schema：

```json
{
  "type": "object", "required": ["objectKey", "name", "fields"],
  "properties": {
    "objectKey": {"type": "string", "pattern": "^[a-z][a-z0-9_]{0,63}$"},
    "name": {"type": "string", "maxLength": 64},
    "fields": {"type": "array", "minItems": 1, "items": {"$ref": "#/$defs/fieldDef"}},
    "relations": {"type": "array", "items": {
      "type": "object", "required": ["field", "targetObject", "kind"],
      "properties": {"field": {"type": "string"}, "targetObject": {"type": "string"},
                     "kind": {"enum": ["many_to_one", "one_to_many"]}}}},
    "mode": {"enum": ["create", "update"]},
    "dryRun": {"type": "boolean", "default": true}
  },
  "$defs": {"fieldDef": {"type": "object", "required": ["name", "type"],
    "properties": {"name": {"type": "string"}, "title": {"type": "string"},
      "type": {"enum": ["string","text","int","float","bool","datetime","enum","relation"]},
      "required": {"type": "boolean"}, "rules": {"type": "object"}, "x-ui": {"type": "object"}}}}
}
```

返回结构：{"ok":true,"schema":{...FDE Form Schema...},"warnings":["enum 字段不参与代码生成"],"diff":{"added":[...],"updated":[...]}}；失败 {"ok":false,"error":{"code":40001,"errCode":"FORM_INVALID_FIELD","msg":"字段 name 与保留列冲突"}}。

### 6.2 generate_business_code

参数 JSON Schema：

```json
{
  "type": "object", "required": ["objectKey", "business"],
  "properties": {
    "objectKey": {"type": "string"},
    "business": {"type": "string", "maxLength": 64},
    "tableName": {"type": "string", "pattern": "^[a-z][a-z0-9_]{0,63}$"},
    "structName": {"type": "string"},
    "parentMenuId": {"type": "integer"},
    "fields": {"type": "array", "items": {"$ref": "#/$defs/fieldDef"}},
    "dryRun": {"type": "boolean", "default": true}
  },
  "$defs": {"fieldDef": {"type": "object", "required": ["name", "type"],
    "properties": {"name": {"type": "string"}, "title": {"type": "string"},
      "type": {"enum": ["string","text","int","float","bool","datetime"]},
      "search": {"type": "boolean"}, "inList": {"type": "boolean"},
      "inForm": {"type": "boolean"}, "required": {"type": "boolean"}}}}
}
```

语义：objectKey 存在则忽略 fields 参数，改由 ObjectToGenRequest 从 sys_biz_fields 投影；缺省 dryRun=true。返回：{"ok":true,"files":[{"path":"internal/model/customer.go","content":"..."}],"diff":{"added":6,"modified":0,"conflicts":[]},"menuPlan":{"menu":{"path":"customer","title":"客户"},"buttons":4},"restartRequired":true}。

### 6.3 apply_files

参数 JSON Schema：

```json
{
  "type": "object", "required": ["batchId", "approve"],
  "properties": {
    "batchId": {"type": "string", "description": "dry-run 返回的批次号"},
    "objectKey": {"type": "string"},
    "files": {"type": "array", "items": {"type": "object",
      "required": ["path", "content"],
      "properties": {"path": {"type": "string"}, "content": {"type": "string"}}}},
    "applyMenu": {"type": "boolean", "default": true},
    "parentMenuId": {"type": "integer"},
    "snapshot": {"type": "boolean", "default": true},
    "approve": {"const": true, "description": "必须显式为 true 才执行写盘"}
  }
}
```

返回：{"ok":true,"applied":[{"path":"...","status":"written"}],"menuId":12,"casbinRebuilt":true,"rollback":{"snapshotId":"s-20250101","command":"git checkout <snapshotId> -- server web && 删除菜单12并重建casbin"}}。

### 6.4 错误码 / 权限校验点 / 审计日志点

| 项 | 约定 |
|---|---|
| 错误码 | 0 成功；40001 业务错误（msg 具体原因，errCode 细分：FORM_INVALID_SCHEMA / CODE_CONFLICT / BATCH_STALE / SNAPSHOT_FAIL）；40003 无权限；40004 未登录 |
| 权限校验点 | 三个工具调用前，用会话 userID 执行 casbin Enforce(userID, path, method)：generate_form_schema→/api/formgen/schema POST；generate_business_code→/api/codegen/preview POST；apply_files→/api/formgen/apply POST（均需在菜单管理预置 type=2 按钮） |
| 审计日志点 | ① dry-run 前：zap 记 user/tool/argsHash；② apply 前：写 sys_gen_audits（action=apply）；③ apply 结束：更新该行结果/文件清单/快照号；④ 失败：记 error 与回滚动作 |

## 7. 数据模型变更

新增表（AutoMigrate 自动建表，初始化脚本 server/internal/initialize/ 注册）：

| 表 | 关键字段 | 说明 |
|---|---|---|
| sys_biz_objects | key(唯一), name, table_name, remark, schema(text), version | 业务对象，唯一事实源 |
| sys_biz_fields | object_id, name, title, type, required, searchable, in_list, in_form, ui_rules(text), relation(text), sort | 字段元数据 |
| sys_form_schemas | object_id, schema(text), version, published, created_by | 表单 schema 版本历史 |
| sys_gen_audits | user_id, tool, action(dry_run/apply), object_key, args_hash, result, files(text), snapshot_id | 生成操作审计 |

修改：sys_ai_tools 表结构不变，仅种子写入 3 条工具定义（generate_form_schema / generate_business_code / apply_files）；sys_menus 不加列，复用 type 0/1/2（新增「表单生成」目录、预览与 apply 按钮）。

## 8. HTTP 接口清单（与 tools 对照）

| 方法 | 路径 | 对应工具 | 说明 |
|---|---|---|---|
| GET | /api/formgen/objects | — | 业务对象分页列表 |
| POST | /api/formgen/objects | generate_form_schema(apply 到对象) | 保存对象+字段+校验 schema |
| GET | /api/formgen/schema/:objectKey | — | 渲染器拉取最新发布 schema |
| POST | /api/formgen/schema/preview | generate_form_schema dryRun | 表单 schema 预览 |
| POST | /api/codegen/preview | generate_business_code dryRun | 现有接口，增加 objectKey 入参 |
| POST | /api/formgen/apply | apply_files | 两段式确认写盘（新增） |
| POST | /api/codegen/generate | apply_files(兼容旧路径) | 现有一次性生成保留，内部改走 apply 事务 |

所有接口沿用响应信封 {code,msg,data}（server/internal/utils/response.go），分页返回 {list,total}，错误码见 api-conventions.md。

## 9. 实施路线图

- P1 业务对象与表单 schema（后端）：新增第 7 章 4 张表与 CRUD；实现 generate_form_schema 工具 + schema 校验器。验收：工具对非法字段/类型/联动返回 40001，合法入参产出 schema 且落库 sys_biz_objects。
- P2 前端渲染器：FormRender.vue + controls.ts + linkage.ts + 表单预览页。验收：同一 schema 在 Element Plus 表单正确渲染，联动（visible/required）与字典加载通过前端单测/手工用例。
- P3 代码生成统一与两段式：ObjectToGenRequest 转换器；CodegenDryRun/CodegenApply 拆分 + apply_files 工具 + 快照/回滚 + sys_gen_audits。验收：从 sys_biz_fields 生成 customer 同构 CRUD；dry-run→apply 成功；人为注入失败文件后回滚到快照、菜单删除、casbin 重建一致。
- P4 MCP 化与可视化编辑器：内置 MCP server 暴露 3 工具；web/src/views/system/formgen 可视化 schema 编辑。验收：外部 MCP 客户端可 tools/list/tools/call；编辑器改 x-ui 后渲染器即时生效；全量 go test 与既有生成器回归通过。

## 10. 风险与对策

| 风险 | 对策 |
|---|---|
| 回滚失败/半写盘 | apply 前强制快照（git 或 .fde-gen-backup），写盘与菜单/授权/casbin 在一个可补偿流程内，失败自动恢复并审计 |
| 幻觉（生成错误代码/字段） | 服务端 Prepare 校验（合法标识符/保留字/类型）+ gofmt 语法校验 + dry-run 人工确认；不合法 40001 |
| 越权生成 | 工具调用前 casbin Enforce(userID, 虚拟 apiPath)；apply 需 approve=true 且批次匹配 |
| 冲突（表/菜单已存在） | dry-run 阶段查 HasTable/同名菜单即报错；apply 再查一次防并发 |
| schema 与代码漂移 | sys_biz_objects.schema/version 为唯一基线，生成代码前强制从对象投影；变更必须走 dry-run→apply |
| 新路由需重启 | 菜单即时可见但接口 404；前端提示 restartRequired=true（沿用现有约定） |
| 模板注入/路径穿越 | 所有 path 经 filepath.Join + 白名单校验（仅 server/internal 与 web/src 前缀），拒绝绝对路径与 .. |

## 11. 决策记录

不可变决策（访谈已确定）：

1. 表单生成器 = Schema 驱动动态表单：生成 JSON 表单定义，前端通用渲染器实时渲染，不写源码、无需重启、智能体可增量修改。理由：与调研结论一致（formily/NocoBase 内核），且避免生成 Vue 组件代码带来的编译重启与二次修改冲突。
2. 统一业务对象模型：sys_biz_objects/sys_biz_fields 为唯一事实源，表单 schema 与 CRUD 代码都由它产出；与现有 codegen 元数据经 ObjectToGenRequest 兼容升级。理由：消除双元数据漂移，gin-vue-admin 双生成器并存验证了该模式。
3. tools 机制：MAF 内置工具 + MCP 双通道共用同一 Go 实现，沿用 sys_ai_tools → tool.Tool 转换（agent-framework-go.md 第 3 节映射）。理由：复用现有 buildTools/lookupAiToolFunc 链路，一次实现两处可用。
4. 两段式落地：dry-run 预览/diff → 人工确认 → apply 写盘并执行菜单/授权/casbin 重建等副作用。理由：生成代码与 RBAC 副作用不可逆，人工确认是幻觉与越权的最后闸门（Claude Code permissions 同型）。
5. MCP 部署形态：MCP 采用独立薄网关进程（cmd/mcp-gateway），经主应用服务间端点 /api/mcp/* + x-api-key（mcp.api-key 配置）调用；主应用不直接对外暴露 MCP。对外鉴权采用静态 API Key：外部 MCP 客户端经 Authorization: Bearer 或 x-api-key 携带 GW_AUTH_TOKEN 访问网关，网关校验通过后转发。理由：鉴权/审计/限流收敛在主应用侧，MCP 协议面与业务服务解耦，网关可独立扩缩容与重启。

对调研文档第四章 10 条映射建议的回应：

| # | 结论 | 理由 |
|---|---|---|
| 1 | 采纳 | 表单内核用 JSON Schema，渲染组件后置绑定，不耦合组件实现 |
| 2 | 变体 | 定义 FDE Form Schema（JSON Schema 子集 + x-ui/x-linkages），不引入 Formily 依赖 |
| 3 | 采纳 | 可视化编辑器放 P4 演进位，先交付智能体生成 + 预览 |
| 4 | 采纳 | 统一对象表投影 GenField，代码与表单共享元数据 |
| 5 | 采纳 | 沿用现有 Go text/template（[[ ]] 定界符）与两段式 |
| 6 | 采纳 | 菜单/按钮/授权并入 apply 事务，复用 createGeneratedMenu |
| 7 | 变体 | 拆为 3 个工具：两个生成工具 + apply_files，写盘必须单独审批 |
| 8 | 采纳 | apply_files 独立、dry-run→审批→apply，权限复用 RBAC 按钮 |
| 9 | 变体 | git 快照/备份目录替代完整沙箱，轻量且可回滚 |
| 10 | 采纳 | 中期把同一注册表打包为 MCP server 对外 |

