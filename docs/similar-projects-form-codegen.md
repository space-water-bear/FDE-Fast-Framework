# 同类项目调研：表单生成器 / 代码生成器 / 智能体 tools 模式

> 调研目标：为 FDE 规划的 ①表单生成器 ②代码生成器（智能体经 tools 生成）提供业界证据与架构参照。
> 核实方式：官方 GitHub 仓库（GitHub API）、官方文档站、npm registry 逐一验证，所有断言附一手来源 URL。

## 一、摘要结论

1. 表单侧：JSON Schema 是验证充分的主流内核——formily（@formily/json-schema）、form-render、react-jsonschema-form 直接以标准 JSON Schema 驱动；amis 以 JSON 配置驱动；NocoBase 的 UI Schema 协议官方声明“基于 Formily Schema 2.0 的 JSON Schema-like”。共同点：schema 与渲染组件解耦。
2. 低代码平台（Appsmith/ToolJet/Budibase/Retool）普遍用内部 DSL 而非标准 JSON Schema，但均已推出 AI 生成能力；ToolJet 还支持从 Claude Code/Codex/Cursor 经 MCP 构建应用。
3. 代码生成侧模式高度一致：元数据表 + 模板引擎（Velocity/Go template/EJS）+ 预览后写盘 + 自动生成菜单/按钮权限。
4. gin-vue-admin 与 FDE 技术栈几乎同构（Gin+Vue3+Element Plus），且已同时内置“代码生成器 + 表单生成器 + MCP/skills”，是最直接对标对象。
5. 智能体 tools 侧：MCP 成为跨端工具标准；OpenAI function calling 与 Anthropic tool use 是底层协议；Claude Code 用 Edit/Bash 等内置工具 + permissions + sandboxing 落地代码；Coze 已实现“Agent 自动生成插件（工具）”。
6. 对 FDE 的结论：表单生成器采用 JSON Schema 内核 + 可视化 schema 编辑器；代码生成器沿用元数据表 + Go 模板 + 两段式写盘 + 菜单权限联动；tools 层暴露 schema 生成与文件写盘两类工具，中期以 MCP server 形式对外。

## 二、分类对比表

### 2.1 Schema 驱动表单引擎 / 低代码表单

| 项目 | 形态 | 核心机制 | 是否支持AI生成 | 对本项目的启示 | 来源 |
|---|---|---|---|---|---|
| Formily | 开源表单库（React/Vue） | JSON Schema 内核（@formily/json-schema），core 与 react/vue 渲染层解耦 | 否（无官方 AI；NocoBase 采用其 Schema） | 表单生成器以 JSON Schema 为中间表示，组件后置绑定 | https://github.com/alibaba/formily |
| amis | 百度开源低代码前端框架 | JSON 配置驱动渲染，表单支持 JSON Schema 模式 | 否 | schema 与组件解耦的成熟参考 | https://github.com/baidu/amis ；https://baidu.github.io/amis/zh-CN/components/form/json-schema |
| lowcode-engine | 阿里开源低代码引擎 | 协议（schema）与渲染/出码分离，物料与 setter 可扩展 | 否 | 出码器与渲染器解耦思路 | https://github.com/alibaba/lowcode-engine |
| form-create | Vue3 动态表单组件 | JSON 规则驱动渲染；Element Plus 版官方包名为 @form-create/element-ui（npm 无 @form-create/element-plus） | 否 | 与 FDE 前端栈（Element Plus）同源，可直接消费 JSON 规则 | https://github.com/xaboy/form-create ；http://form-create.com/v3/element-ui/ |
| react-jsonschema-form | React 表单组件 | 标准 JSON Schema → HTML 表单 | 否 | 标准 JSON Schema 互操作与校验 | https://github.com/rjsf-team/react-jsonschema-form |
| form-render（x-render） | 阿里 React 表单/表格方案 | JSON Schema 驱动，带 schema-builder 可视化编辑 | 否 | schema 可视化编辑器可作表单生成器前端 | https://github.com/alibaba/x-render ；https://xrender.fun/schema-builder |
| NocoBase | 开源低代码平台 | UI Schema 协议（官方：基于 Formily Schema 2.0 的 JSON Schema-like），插件化架构 | 是（官方定位“Agents write the code”） | schema 驱动 UI + 插件治理 + AI 生成 | https://github.com/nocobase/nocobase ；https://nocode.docs.nocobase.com/flow-engine/ui-schema.md |
| Appsmith | 开源内部工具平台 | 内部 DSL（widget 树），非标准 JSON Schema | 是（Appsmith AI 生成应用） | AI 生成应用的交付形态 | https://github.com/appsmithorg/appsmith ；https://www.appsmith.com/ai/low-code |
| ToolJet | 开源内部工具平台 | 内部 schema，支持可视化/prompt 生成 | 是（ToolJet AI；可从 Claude Code/Codex/Cursor 经 MCP 构建） | MCP 作为外部 AI 工具入口的现成范例 | https://github.com/ToolJet/ToolJet |
| Budibase | 开源低代码平台 | 内部 schema | 是（AI agents/automations 定位） | AI 生成业务应用与自动化 | https://github.com/Budibase/budibase |
| Retool | 商业低代码平台 | 内部 DSL（classic app builder） | 是（Retool AI 文档） | 商业产品 AI 建应用形态 | https://docs.retool.com/apps/concepts/ai |

### 2.2 CRUD / 业务代码生成器

| 项目 | 形态 | 核心机制 | 是否支持AI生成 | 对本项目的启示 | 来源 |
|---|---|---|---|---|---|
| JeecgBoot | 企业级低代码平台（Java+Vue3） | 在线代码生成器：建表→前后端代码+菜单权限；AI Skills 一句话生成完整系统 | 是 | 元数据+模板+菜单权限联动+AI Skills | https://github.com/jeecgboot/JeecgBoot ；https://help.jeecg.com/java/ai/skills/skill-codegen/ |
| RuoYi | SpringBoot 权限管理系统 | 生成配置存 gen 表（GenTable/GenTableColumn），Velocity 模板（vm 目录含 java/js/sql/ts/vue/xml），预览后生成 | 否 | 元数据表+模板引擎+预览写盘 | https://github.com/yangzongzhuan/RuoYi ；https://doc.ruoyi.vip/ruoyi/document/htsc.html |
| gin-vue-admin | Go（Gin）+Vue3 开发平台 | 同时内置代码生成器与表单生成器；Go template（server/resource/function 下 api.go.tpl 等） | 是（AI 辅助、内置 MCP/skills） | 与 FDE 技术栈最接近的直接对标 | https://github.com/flipped-aurora/gin-vue-admin |
| JHipster | 应用生成平台 | JDL 领域语言描述实体，Yeoman 生成器 + EJS 模板（仓库含 1566 个 .ejs） | 否（生成式，非 LLM） | 元数据 DSL + 模板 + 增量生成 | https://github.com/jhipster/generator-jhipster ；https://www.jhipster.tech/jdl/ |
| gorm/gen | Go 代码生成库 | 数据库 schema → struct + 类型安全 DAO | 否 | FDE 模型/DAO 生成可复用 | https://github.com/go-gorm/gen ；https://gorm.io/gen/database_to_structs.html |
| sqlboiler | Go ORM 代码生成 | 数据库 schema → Go ORM 模型 | 否 | 备选 DB-first 生成 | https://github.com/aarondl/sqlboiler |

### 2.3 智能体经 tools 生成表单/代码的模式

| 项目 | 形态 | 核心机制 | 是否支持AI生成 | 对本项目的启示 | 来源 |
|---|---|---|---|---|---|
| MCP（Model Context Protocol） | 开放协议规范 | JSON-RPC 2.0，tools/resources/prompts 原语；官方 servers 仓库提供参考实现 | 协议本身不生成 | tools 层中期对外暴露 MCP | https://github.com/modelcontextprotocol/modelcontextprotocol ；https://github.com/modelcontextprotocol/servers |
| mcp-forger | MCP server 生成器 | 分析 OpenAPI/仓库/URL → 生成生产级 MCP server | 是 | “用 AI 生成工具服务器”的直接范例 | https://github.com/coderXcode/mcp-forger |
| OpenAI Function Calling | API 能力 | tools 参数以 JSON Schema 定义，模型返回调用意图，应用侧执行 | 是（协议层） | 对应 FDE 的 MAF ToolAutoCall | https://platform.openai.com/docs/guides/function-calling |
| Anthropic Tool Use | API 能力 | 官方 tool use loop 文档 | 是（协议层） | 同上 | https://platform.claude.com/docs/en/agents-and-tools/tool-use/how-tool-use-works |
| Claude Code | AI 编码 CLI | 内置工具（Edit/Bash 等，见 tools-reference）+ permission modes + sandboxing | 是 | 两段式写盘+审批+沙箱直接参考 | https://code.claude.com/docs/en/tools-reference ；https://code.claude.com/docs/en/permissions ；https://code.claude.com/docs/en/sandboxing |
| v0 | Vercel AI UI 生成 | 文本提示 → React/UI 代码 | 是 | 生成代码的交付与预览 | https://vercel.com/academy/ai-sdk/ui-with-v0 |
| bolt.new | StackBlitz 全栈生成 | 浏览器沙箱内生成并运行 Web 应用 | 是 | 沙箱运行生成结果 | https://bolt.new |
| Cursor | AI 代码编辑器 | Agent 模式，diff 级应用模型 | 是 | diff/审批式代码落地 | https://cursor.com/docs |
| Dify | LLM 应用平台 | Agent/工作流节点绑定工具 | 是 | 工具编排与调用参考 | https://docs.dify.ai/en/self-host/use-dify/nodes/agent ；https://github.com/langgenius/dify |
| Coze | Agent 开发平台 | 插件=工具集；Agent 可自动梳理需求并生成插件 | 是 | 用“插件”封装表单/代码生成能力 | https://docs.coze.cn/create-plugin.md |
| LangChain | Agent 工程平台 | Tool 抽象 + tool calling 文档 | 是 | 工具抽象参考（Go 侧借鉴其接口设计） | https://github.com/langchain-ai/langchain ；https://docs.langchain.com/oss/python/langchain/frontend/tool-calling |

## 三、可复用架构模式提炼

1. **Schema 内核模式（表单）**：以 JSON Schema（或 JSON Schema-like 协议）作为表单的单一中间表示；协议层与渲染层分离，渲染组件后置绑定。证据：formily 的 @formily/json-schema + react/vue 多端渲染；NocoBase UI Schema 明确基于 Formily Schema 2.0；react-jsonschema-form 直接消费标准 JSON Schema。
2. **元数据表 + 模板引擎（代码生成）**：生成配置持久化在数据库表（RuoYi gen 表、gin-vue-admin 生成配置），模板按层分类（Go .tpl：api.go/api.js/server.go；RuoYi vm：java/js/sql/ts/vue/xml；JHipster：1566 个 .ejs）。
3. **两段式写盘（预览 → 写盘）**：生成结果先以预览/文件列表呈现，确认后再写盘；Claude Code 以 Edit/Bash 工具 + permissions 审批落地修改；RuoYi 生成器提供“预览”后再生成。FDE 应做成 dry-run（返回 diff 预览）→ 审批 → apply。
4. **菜单与权限联动生成**：代码生成的同时创建菜单/按钮权限并授权（JeecgBoot“建表、前后端代码、菜单权限全自动”；gin-vue-admin 代码生成器与 RBAC 菜单联动）。FDE 已具备 sys_menus 0/1/2 类型，可直接复用。
5. **工具化封装（智能体）**：把“生成”能力封装为带 JSON Schema 参数的工具，由模型自动调用（OpenAI function calling / Anthropic tool use / MAF ToolAutoCall）；Coze 更进一步：Agent 可自动生成“插件”（工具+界面）——即“用工具生成工具”。
6. **AI 生成的两类落地**：a) 生成 schema（低代码平台内部 DSL，如 Appsmith/ToolJet）；b) 生成代码文件（Claude Code/v0/bolt.new/Cursor）。FDE 两条路线都经 tools 走：表单走 schema 路线，代码走文件生成路线。
7. **沙箱与可回滚**：bolt.new 在浏览器沙箱内运行生成结果；Claude Code 提供 sandboxing 与 permission modes。FDE 写盘前应做快照（git commit/备份），失败可回滚。
8. **MCP 作为对外标准**：ToolJet 支持 Claude Code/Codex/Cursor 经 MCP 构建；mcp-forger 演示了“AI 生成 MCP server”。FDE 中期可将生成工具打包为 MCP server，被任意 MCP 客户端复用。

## 四、对 FDE 的映射建议（10 条）

1. **表单生成器**：表单内核采用 JSON Schema（字段类型/校验/UI 提示），渲染层组件后置绑定，参照 formily 的 schema 与 react/vue 解耦方式，避免 schema 里耦合组件实现。
2. **表单生成器**：定义 FDE 自己的“UI Schema 协议”层（借鉴 NocoBase UI Schema 基于 Formily Schema 2.0 的做法），让智能体只产出 schema 文本，不产出组件代码。
3. **表单生成器**：前端做可视化 schema 编辑器（参照 x-render schema-builder / form-create designer），作为智能体生成结果的“可预览、可修改”界面。
4. **代码生成器**：元数据模型沿用现有表定义，把字段级“表单类型/校验”补全为 schema 片段，使代码生成器与表单生成器共享同一元数据源（对齐 gin-vue-admin 双生成器并存）。
5. **代码生成器**：模板引擎用 Go text/template（gin-vue-admin 同款，按 api.go.tpl/api.js.tpl/server.go.tpl 分层），生成流程做成两段式：先产出文件列表/diff 预览，再写盘。
6. **代码生成器**：菜单与按钮权限的生成并入生成事务（复用 sys_menus 0/1/2 与角色授权），参照 JeecgBoot/gin-vue-admin 的“代码+菜单权限全自动”。
7. **tools**：暴露两个 MAF tool.Tool——generate_form_schema（参数为 JSON Schema 描述的字段定义，返回 UI Schema）与 generate_business_code（参数为表/字段元数据，返回文件清单+dry-run diff）；沿用现有 sys_ai_tools → tool.Tool 转换，无需新表。
8. **tools**：文件写盘单独拆为 apply_files 工具（路径/内容/diff），先 dry-run 返回预览，审批后执行；权限校验复用 RBAC 接口权限，参照 Claude Code permissions。
9. **tools**：生成前自动 git 快照（或备份目标文件），写盘失败可回滚；参照 bolt.new 沙箱运行与 Claude Code sandboxing 的隔离思路。
10. **tools**：中期把上述工具打包为 MCP server（参照 modelcontextprotocol/servers 参考实现与 mcp-forger 的生成器思路），使 Claude Code/Cursor 等外部客户端也能调用 FDE 的表单/代码生成能力。
