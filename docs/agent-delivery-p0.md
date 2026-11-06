# P0：开发契约与客户工作区

本批实现路线图中的规格往返、draft/active 生命周期、可信执行身份、统一生成批次、项目隔离及工作区/任务记录。自动开发 worker、检查修复 Runner、版本包安装和增量迁移属于后续 P1/P2，排队记录尚不会自动执行代码开发。

## 行为与兼容性

- 表单编辑器保存完整 Schema 与规范化业务字段，保留校验、枚举值类型、UI、关系、联动、排序与未知扩展。草稿更新需要 `expectedVersion`，显式激活需要 `version + expectedActiveVersion`。
- `GET /api/formgen/schema/:objectKey` 读取 active；`?version=draft` 读取最新草稿，`?version=N` 读取历史版本。草稿保存不会改变运行版本。
- 历史已发布 Schema 迁移为 active；原对象归入默认项目。旧全局 key 唯一索引改为 `(project_id,key)` 唯一索引，迁移幂等且不删除业务字段数据。
- `/api/codegen/preview` 对所有生成请求返回批次；`/api/codegen/generate` 与 `/api/formgen/apply` 仅接受已预览的 `batchId + approve:true`，旧的直接生成请求会要求先预览。
- `generate_business_code` 只生成预览；`dryRun=false` 不再自动写入。原生 `apply_files` 总是返回 `APPROVAL_REQUIRED`，供用户在管理界面确认批次。API Key 服务身份只能进行无副作用的表单预览，不能冒充用户写盘。
- 批次检查用户、项目、对象版本、文件 hash、输出根、有效期与单次领取状态，始终创建快照。恢复失败明确标为 `rollback_failed`，不会宣告恢复完成。
- 默认项目兼容原生成器配置，仅当前有权用户可显式应用；客户项目写入各自源码目录，保留菜单计划，不把业务菜单写到工作台。目标系统的菜单、Schema 和权限安装由 P1 交付流程实现。
- 会话绑定项目并按当前用户过滤，读取/继续/编辑/重生成均检查归属；非默认项目每次检查当前访问权限。工程工具执行另外检查项目和具体工具的 RBAC。

## 客户项目与任务入口

工程请求通过 `X-FDE-Project-ID` 或 `projectId` 查询参数指定项目；两者同时存在时必须一致。默认项目 ID 为 1。HTTP 处理器验证当前用户和项目，再生成不可由工具 JSON 覆盖的执行上下文。

| 请求 | 用途 |
|---|---|
| `GET /api/dev/projects` | 查询当前用户可访问的项目 |
| `POST /api/dev/projects`，`{key,name}` | 从固定模板 commit 导出独立源码仓库 |
| `POST /api/dev/runs`，`{requirement}` | 在所选客户项目持久化 queued 任务及需求 |
| `GET /api/dev/runs/:key` | 查询本人的任务与事件 |
| `POST /api/dev/runs/:key/cancel` | 原子取消排队任务并记录事件 |
| `POST /api/formgen/objects/:objectKey/activate` | 激活指定草稿版本 |

新权限项通过现有菜单/角色机制分配。默认项目仅供原管理生成器兼容；创建开发任务需要具有独立工作区的客户项目。任务 API 目前是 P1 worker 的持久化基础，状态不会伪装为“已生成”或“已部署”。

工作区导出只使用明确提交的 Git archive，过滤已跟踪的运行数据库、日志、上传、构建二进制、缓存和本地环境文件；未跟踪和未提交修改不会进入客户项目。Go 导出器拒绝越界路径、链接和特殊文件。`fde.sh` 采用同样的提交导出方式，`FDE_TEMPLATE_REF` 可固定模板版本。

## 后续迁移与运行约定

迁移执行选择 Goose v3 的独立 Provider 与版本化 SQL 文件，复用 GORM 底层数据库连接；数据回填必要时使用 Go 迁移。正式接入与依赖锁版在 P2 进行，先验证 SQLite，再完成 PostgreSQL 矩阵。Provider 负责执行和版本记录，文件校验和、发布互斥及恢复策略仍由交付模块负责；不把 Down 迁移视为业务数据备份。依据：[Goose 项目文档](https://github.com/pressly/goose)、[Provider 接口](https://github.com/pressly/goose/blob/main/provider.go)。

当前工具链沿用 `go.mod` 的 Go 1.26、Node.js 22.13+ 与 pnpm 11.23.0。前端开发可用 `FDE_DEV_API_TARGET` 指向独立后端进行验收；生产构建不包含该开发代理配置。P1 将固定隔离检查环境并验证 Linux 发布制品。

## 本批验证

- Go 全包测试与 `go vet ./...`。
- 工程身份、文件保护、项目隔离、迁移及请求日志等相关包的 race 测试。
- 前端 `node --test tests/*.test.mjs`、`vue-tsc --noEmit`、Vite 生产构建。
- 独立临时数据库中的实际浏览器验收：多类型草稿保存/重载，激活 v1 后保存 v2 而 active 保持 v1；修改生成参数使旧预览失效；新批次实际写入六个文件后不能重复应用。
- 干净源码导出 smoke：模块和应用名按字面替换，保留许可证，过滤运行数据与构建产物。

这些结果证明本批 P0 契约已被验证；尚未代表真实模型自动构建业务、Linux 客户部署或保留数据升级已经验收。
