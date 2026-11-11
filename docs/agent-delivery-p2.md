# P2 增量开发与 SQLite 升级

本阶段支持在单实例开发工作台中继续修改已接管的业务模块，并在本机 Docker Linux 与 SQLite 目标上完成新增字段升级和受保护的快照回退。代码入口和首次安装准备沿用 [P1 操作说明](agent-delivery-p1.md)。

## 在工作台继续开发

1. 完成 V1 任务，查看需求、源码差异和完整检查报告，构建并验收 V1 发布包。
2. 审阅后点击提升基线并确认任务及当前源码版本。系统重新核对源码、检查报告与制品哈希，创建新的基线目录，通过项目版本比较后切换指针。排队或运行中的任务会阻止提升。
3. 创建第二个任务。任务记录固定当前基线 commit 和 revision，智能体可按文件哈希修改已经接管的业务源码。框架文件、配置、锁文件及已有验收测试仍受保护。
4. 对已生成对象追加字段时，工程工具使用 `mode=update` 和 `baseSpecVersion`。`fields` 只列新增字段。模型、编译注册的 Schema、规格和业务安装资产一起更新，自定义服务、接口及页面代码保留，业务规则通过明确的源码 patch 修改。
5. V2 检查通过后审阅差异并构建发布包。在目标面板核对当前版本，明确确认升级候选和目标版本，随后执行升级。

例如在 `biz_repair_ticket` 上新增优先级，字段规格为下面的工具输入。历史记录回填为 `normal`。

```json
{
  "objectKey": "biz_repair_ticket",
  "mode": "update",
  "baseSpecVersion": 1,
  "fields": [{
    "name": "priority",
    "title": "优先级",
    "type": "enum",
    "required": true,
    "inForm": true,
    "inList": true,
    "rules": {
      "enum": ["normal", "urgent"],
      "default": "normal"
    }
  }]
}
```

文件所有权保存在宿主管理的 `fde/assets/ownership.json`，包含对象归属、原模板哈希和当前哈希。后续任务按已批准的基线读取所有权。预览后文件变化、规格版本变化或未接管文件冲突会停止写入。旧项目的业务文件如果缺少清单，首版会停止生成，需要另行审阅接管方案。

## 升级与恢复

工作台升级绑定项目、候选发布包和 `expectedCurrentReleaseId`。升级候选必须来自当前目标版本已提升的源码基线。目标串行执行操作，目标路径按安装器的规范路径识别，当前操作状态与真实发布版本分别展示。

升级先停止目标服务，取得数据库维护锁，再备份 SQLite 与上传文件。系统在数据库副本上预演迁移并校验版本，然后在事务中完成新增列、默认值回填、活动 Schema、菜单及安装版本记录。启动新版本后，还要检查真实 readiness、业务列表和活动 Schema，最后记录部署完成。普通 `serve` 只读取已安装状态。

回退使用升级前的数据库与上传快照。恢复前核对备份哈希、业务行、上传文件和权限。如果升级后新增或修改了业务数据、上传文件，自动快照恢复会被拒绝。现有用户和角色之间的无关授权调整可以保留，本次受管理菜单的冲突及用户、角色主体变化会停止自动恢复。拒绝回退后，工作台只在当前版本的数据库校验与 HTTP 就绪检查均通过时恢复当前服务。

任务和操作中断后保留日志。已提交的迁移可以按记录核对并重试。项目基线提升中断时，启动恢复只会核对并清理有明确证据的预留，不自动提升候选。目标的中断升级或回退可在确认没有活动操作后继续。

## Linux 命令入口

使用目标架构匹配的已验证包，在维护窗口停止服务后布置 V2。下面的路径需要替换为实际安装结果。

```sh
/offline-package/V2/fde install-upgrade \
  --from-package /opt/customer-app/releases/V1 \
  --package /offline-package/V2 \
  --target /opt/customer-app

/opt/customer-app/releases/V2/server upgrade \
  --config /opt/customer-app/shared/config.yaml \
  --from-manifest /opt/customer-app/releases/V1/manifest.json \
  --manifest /opt/customer-app/releases/V2/manifest.json

/opt/customer-app/releases/V2/server verify \
  --config /opt/customer-app/shared/config.yaml \
  --manifest /opt/customer-app/releases/V2/manifest.json

/opt/customer-app/releases/V2/server serve \
  --config /opt/customer-app/shared/config.yaml \
  --manifest /opt/customer-app/releases/V2/manifest.json \
  --web /opt/customer-app/releases/V2/web
```

需要快照恢复时，先停止目标服务，再使用 V2 包内命令核对与执行恢复，成功后用 V1 的 `verify` 和 `serve` 启动。工作台会在就绪验收后更新目标版本指针，手工运行时以上述明确版本路径和数据库安装记录为准。

```sh
/opt/customer-app/releases/V2/server rollback \
  --config /opt/customer-app/shared/config.yaml \
  --from-manifest /opt/customer-app/releases/V1/manifest.json \
  --manifest /opt/customer-app/releases/V2/manifest.json
```

## 复测方式

常规 Go race、vet、构建，以及前端测试、类型检查、生产构建均使用项目现有命令。Docker 用例需要先按 P1 说明准备固定检查镜像。

```sh
cd server
FDE_RUN_DOCKER_INCREMENTAL_ACCEPTANCE=1 go test ./internal/service \
  -run '^TestDevelopmentDockerIncrementalDelivery$' -count=1 -v -timeout 35m
```

真实模型用例通过明确的 opt-in 执行。`FDE_REAL_MODEL_INPUT` 指向仓库外的私有 JSON，提供 `provider` 对象及可选的 `skills` 数组。供应商字段包括 `baseUrl`、`apiKey`、`model`、`apiMode` 和 `enabled`。模型调用及工程任务在独立控制库中记录，目标使用独立业务库。

```sh
FDE_RUN_REAL_MODEL_ACCEPTANCE=1 FDE_REAL_MODEL_INPUT=/private/provider.json \
  go test ./internal/service -run '^TestDevelopmentRealModelFirstDelivery$' \
  -count=1 -v -timeout 35m

FDE_RUN_REAL_MODEL_ACCEPTANCE=1 FDE_REAL_MODEL_INPUT=/private/provider.json \
  FDE_REAL_MODEL_CONTINUE_ROOT=/private/retained-v1-fixture \
  go test ./internal/service -run '^TestDevelopmentRealModelIncrementalDelivery$' \
  -count=1 -v -timeout 35m
```

第一项真实模型用例包含宿主注入的编译错误，注入也遵守业务文件哈希与所有权约束。第二项使用保留的同一 V1 项目和智能体，新增枚举及状态规则，再执行升级和回退。普通 CI 跳过付费模型调用，手动 Docker 工作流运行首次与增量两项固定验收。

## 2026 年 11 月 11 日验收记录

完整验收基线为 `25f57ff9211a9f065447687c2415d1ee0855f354`。后续更新涉及工作台完成态提示、文档及入口。实际安装目标为本机 Docker Linux/arm64，数据库为 SQLite，符合本次选定的验收范围。[PR #2](https://github.com/space-water-bear/FDE-Fast-Framework/pull/2)

| 验收 | 结果 |
| --- | --- |
| 框架检查 | Go 全包 race、vet、构建和前端 20 项测试、类型检查、生产构建通过。GitHub 同一提交的前后端检查成功。 |
| 固定工程协议 | 脚本网关通过真实 MAF worker 生成 V1，加入 BLOCKED 登记号自定义校验，提升基线后用第二个任务新增 priority 枚举和状态规则。两个任务各一轮检查通过，旧自定义校验保留。 |
| SQLite 交付 | 同一目标完成 V1 → V2，20 条历史记录的 ID 和业务字段保持，priority 回填 normal。V2 新装预览、业务规则、枚举拒绝、重启与相同升级重试通过。 |
| 快照恢复保护 | 新增第 21 条专用验收记录后，回退被拒绝且 V2 恢复运行。经明确删除该验收记录后，快照回退与重复回退通过，原有 20 条记录保持。 |
| 真实模型首次开发 | `deepseek-flash` 经 Responses 协议驱动内置工具生成维修登记，宿主注入编译错误后第二轮修复通过，完成首装、CRUD、必填、操作员权限和重启检查。 |
| 同一真实模型继续开发 | 同一项目提升 V1 基线，原智能体新增 priority 和 urgent 不能直接 closed 的服务规则，一轮检查通过，完成 V2 升级、默认值回填、规则拒绝、重复升级、重启及 V1 回退。20 条原记录的全部已有字段保持。 |
| 恢复回归 | 迁移事务失败重试、孤立备份恢复、中断基线提升、列名映射、受管理权限冲突、无关授权保留、备份篡改和运行维护锁均有通过的定向回归。 |

GitHub 常规 CI 的重型 Docker job 按工作流条件跳过，上表中的真实安装来自本机显式 opt-in 执行。[GitHub CI](https://github.com/space-water-bear/FDE-Fast-Framework/actions/runs/42951759241)

| 真实模型版本 | 发布 ID | 业务源码快照 SHA256 | 发布清单 SHA256 |
| --- | --- | --- | --- |
| V1 | `f09d964f-1aae-47a5-b91a-04e6627c282f` | `d90ad6756ea885f1aa508aa5705af9c45276b91db5f2e18863fe9146d6f0294b` | `d58faa01896479cc7bbdc6dd27f8106b1651530dc1093f5fb2c8a8b867486c1a` |
| V2 | `4daf742c-29c2-4db0-bde9-b6176cf8003e` | `58c60e3af9c08d5fb7e3554a2e8f7f37eb12724fae22ea3d66c322c01675bdf8` | `cb3dcd02487a4d4e9d9f075dfab05b6ea068647b9893e1b1b9439fc1199f7de9` |

这轮真实模型首次任务观察到 13 次供应商 HTTP 请求，客户端记录两次模型执行回合的报告用量。运行库、私有供应商配置、完整事件与截图保留在本机，公开文档列出可核对的版本和哈希。

本轮整合验收修复了无参数工具 Schema 的空 `required` 编码、目标路径别名导致容器身份不一致、数据库字段名映射，以及新装 V2 时活动表单版本固定为 1 的问题。此前的真实模型试验、当前同一内置模型的两次任务和固定脚本网关回归各自保留记录。

## 当前支持范围

本轮覆盖新增基础字段、字符串枚举、可校验默认值与回填、受管理源码的后续 patch，以及同一 SQLite 目标的相邻版本升级。删除或改名字段、类型缩窄、修改已有约束、替换对象集合和跨版本跳跃需要另外的迁移方案。独立 UI 发布、PostgreSQL 交付及远程客户主机验收继续按 [路线图](agent-delivery-roadmap.md) 推进。
