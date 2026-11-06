# FDE-Fast-Framework

[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

基于 gin + gorm + jwt + casbin 的后台管理系统脚手架，默认即后台管理形态，内置完整 RBAC。
设计思路参考 gin-vue-admin（https://github.com/flipped-aurora/gin-vue-admin），并做了大幅精简：
去掉 redis、多机部署等重依赖，保留 JWT 鉴权、casbin 角色权限、动态菜单三个核心，
并内置表单式代码生成器（可视化定义表与字段，一键生成前后端 CRUD）。

前端通用组件文档：docs/components/table-page.md（表格页组件：左树 + 搜索表单 + vxe-grid 表格）。
API 前后端契约：docs/api-conventions.md（响应信封/业务码/鉴权/分页/错误处理约定）。
AI 框架迁移开发文档：docs/agent-framework-go.md（eino → Microsoft Agent Framework for Go）。
分阶段改造计划：[内置智能体业务开发与交付路线图](docs/agent-delivery-roadmap.md)。
当前改造说明：[P0 开发契约与工作区](docs/agent-delivery-p0.md)。

## 技术栈

后端

    Gin 1.10 | GORM 1.25 | golang-jwt v5 | Casbin v2(gorm adapter) | bcrypt

数据库（二选一，配置切换）

    SQLite      默认，纯 Go 驱动(glebarez/sqlite)，零 cgo 依赖，开箱即用
    PostgreSQL  gorm postgres 驱动，生产推荐

前端

    Vue 3.5 + Vite 5 + TypeScript + Element Plus + vxe-table + Pinia + Vue Router + Axios

## 特性

- 开箱即用的 RBAC：用户 - 角色 - 菜单/按钮 - 接口权限，casbin 策略随数据变更自动重建
- 动态菜单与动态路由：登录后按角色下发菜单树，前端动态注册路由，侧边栏自动渲染
- 标签导航栏：打开过的页面自动生成标签（对标 gin-vue-admin），支持切换、关闭、
  右键关闭其他/关闭全部；菜单管理支持 固定标签(affix)/隐藏标签(hideTab) 配置
- 数据库双支持：改 server/config.yaml 一行配置即可切换 SQLite / PostgreSQL
- JWT 登录鉴权 + bcrypt 密码哈希 + 图形验证码(可配置开关) + 刷新令牌(轮换/注销吊销, 前端 401 自动续期)
- 多设备登录与令牌黑名单：活跃设备管理(踢下线/踢出其他)，单点登录开关(新登录挤掉旧设备)，
  注销/被踢令牌即时失效，令牌带 jti 保证同秒签发也不重复
- 文件上传：multipart 上传到本地目录(扩展名白名单+大小限制)，静态访问 /uploads，
  文件列表/下载/删除，前端支持图片缩略图与预览
- zap 结构化日志：控制台 + lumberjack 轮转文件(server/log)，请求日志带
  方法/路径/状态码/IP/耗时字段，panic 完整堆栈
- 操作日志：中间件自动记录接口调用(用户/路径/耗时/状态码/请求体脱敏)，支持检索、删除、清空
- 字典管理：字典类型 + 字典数据两级维护，删除类型级联删除数据
- AI 助手（基于 Microsoft Agent Framework for Go v0.3.0）：供应商(OpenAI兼容，
  支持 Responses/Chat 两种 API 模式，默认 responses 适配 sub2api/new-api 中转站) /
  智能体 / 技能 / 工具 / MCP服务 / 会话聊天；供应商支持一键获取模型列表(/models)并用
  下拉选择默认模型；智能体可绑定技能(提示词注入)/工具(MAF ToolAutoCall 自动调用)/
  MCP服务(stdio/sse/streamable 运行时接入)；会话支持 SSE 流式输出(可中途停止)、
  首条消息自动生成标题、列表分页与搜索、重新生成/编辑重发/复制/重命名/清空、
  长对话上下文裁剪；用量统计(token/延迟 按供应商与智能体聚合)；聊天请求结构化
  日志(zap) + OTel 中间件；go test 固化验收(mock 网关 + 内存库 + 真实 MCP streamable)
- 内置页面：登录、首页、用户管理(vxe-table)、角色管理(菜单权限树)、菜单管理(树表)、
  字典管理、操作日志、登录设备、文件管理、代码生成器
- 代码生成器：表单式定义表与字段，一键生成后端模型/服务/接口/路由 + 前端 API/页面，
  自动创建菜单与按钮权限并授给对应角色（内置 customer、product 两个生成示例）
- 一键生成新项目：fde.sh 复制模板并替换模块名/应用名

## 快速开始

环境要求：Go 1.26+（与 server/go.mod 一致），Node.js 22.13+，pnpm 11.23.0（见 web/package.json）。

启动后端（默认端口 8080，首次启动自动建表并写入种子数据）

    cd server
    go mod tidy
    go run .

启动前端（开发端口 5173，/api 自动代理到 8080）

    cd web
    pnpm install
    pnpm dev

访问 http://localhost:5173

内置账号

    admin / 123456   超级管理员，拥有全部权限
    user  / 123456   普通用户，仅有首页，访问管理接口会被 403 拒绝（RBAC 演示）

## 配置说明

见 server/config.yaml：

    app:
      name: FDE-Fast-Framework     # 应用名
      port: 8080          # 后端端口
      mode: debug         # debug | release
    db:
      driver: sqlite      # sqlite | postgres
      dsn: ./data/app.db  # sqlite 填文件路径
    jwt:
      secret: ...                # 生产必须修改
      access-expires-hours: 2    # 访问令牌有效期
      refresh-expires-hours: 168 # 刷新令牌有效期(每次刷新自动轮换)
      use-multipoint: true       # false=单点登录(新登录踢掉旧设备)
    captcha:
      enabled: true              # 登录验证码开关
      expire-minutes: 5
    upload:
      dir: ./uploads             # 上传文件存储目录
      max-size-mb: 10            # 单文件大小上限
    zap:
      level: info                # debug | info | warn | error
      director: ./log            # 日志文件目录(留空仅控制台)
      log-in-console: true
    seed:
      enabled: true              # 首次启动写入内置菜单/角色/账号

切换到 PostgreSQL，只需改两行：

    db:
      driver: postgres
      dsn: host=127.0.0.1 user=postgres password=123456 dbname=fde_admin port=5432 sslmode=disable TimeZone=Asia/Shanghai

也支持环境变量覆盖：FDE_PORT、FDE_DB_DRIVER、FDE_DB_DSN、FDE_JWT_SECRET、
FDE_CAPTCHA_ENABLED、FDE_JWT_USE_MULTIPOINT。

## RBAC 工作原理

1. sys_menus 保存菜单与按钮：type 0=目录 1=菜单(前端路由) 2=按钮(接口权限)；
   按钮/菜单可配置 apiPath + apiMethod，即一条 casbin p 策略的素材。
2. sys_roles 与 sys_menus 通过 role_menus 多对多关联，决定角色可见菜单与可调接口。
3. casbin 模型：g(用户ID, 角色ID) 表示用户属于哪些角色；p(角色ID, 路径, 方法) 表示角色可调哪些接口。
   内置 admin 角色自动获得 /* * 通配权限。
4. 任何用户/角色/菜单变更后，服务层调用 RebuildCasbin 从数据库全量重建策略。
5. 中间件链：JWT 解析 -> Casbin Enforce(userID, path, method)，路径匹配使用 keyMatch2，
   因此按钮权限可写成 /api/user/:id 这样的通配形式。
6. 用户登录后，/api/auth/userinfo 按用户角色聚合返回菜单树；前端据此注册路由并渲染侧边栏。

## 目录结构

    fde-admin/
    ├── config.yaml -> server/config.yaml
    ├── fde.sh                  # 项目生成器
    ├── server/                 # Go 后端
    │   ├── main.go
    │   ├── config.yaml
    │   └── internal/
    │       ├── api/            # HTTP 处理器(controller)
    │       ├── config/         # 配置加载
    │       ├── global/         # 全局 DB / Enforcer
    │       ├── initialize/     # 初始化 + 种子数据
    │       ├── middleware/     # CORS / JWT / Casbin
    │       ├── model/          # 数据模型
    │       ├── router/         # 路由注册
    │       ├── service/        # 业务逻辑 + casbin 重建
    │       └── utils/          # 响应/jwt/密码/分页工具
    └── web/                    # Vue3 前端
        └── src/
            ├── api/            # 接口封装
            ├── layout/         # 布局 + 递归侧边栏
            ├── router/         # 静态路由 + 动态路由注册 + 守卫
            ├── stores/         # pinia 用户态
            ├── utils/          # axios 拦截器 / 格式化
            └── views/          # 登录/首页/404/系统管理

## API 一览

    GET    /api/auth/captcha     获取登录验证码(含开关状态)
    POST   /api/auth/login       登录，返回访问令牌 + 刷新令牌（请求头 x-token）
    POST   /api/auth/refresh     刷新令牌换新访问令牌(刷新令牌同步轮换)
    GET    /api/auth/userinfo    当前用户 + 角色 + 菜单树
    POST   /api/auth/logout      登出并吊销刷新令牌(访问令牌入黑名单)
    GET    /api/auth/devices     当前账号活跃登录设备
    DELETE /api/auth/devices/:id 踢下线指定设备
    DELETE /api/auth/devices     踢出除当前设备外的全部设备
    GET    /api/user/list        用户分页列表(关键词搜索)
    POST   /api/user             新增用户(含角色分配)
    PUT    /api/user/:id         修改用户
    DELETE /api/user/:id         删除用户
    GET    /api/role/list        角色列表
    GET    /api/role/:id         角色详情 + 已分配菜单ID
    POST   /api/role             新增角色
    PUT    /api/role/:id         修改角色(含菜单权限树)
    DELETE /api/role/:id         删除角色
    GET    /api/menu/tree        菜单树
    POST   /api/menu             新增菜单/按钮
    PUT    /api/menu/:id         修改菜单/按钮
    DELETE /api/menu/:id         删除菜单/按钮
    GET    /api/codegen/options  代码生成器选项(字段类型/父菜单)
    POST   /api/codegen/preview  代码生成预览(不写盘)
    POST   /api/codegen/generate 执行代码生成(写盘+建菜单+授权+重建casbin)
    GET    /api/dict/types       字典类型列表
    POST   /api/dict/type        新增字典类型
    PUT    /api/dict/type/:id    修改字典类型
    DELETE /api/dict/type/:id    删除字典类型(级联删除其数据)
    GET    /api/dict/data/list   字典数据列表(按 typeId)
    POST   /api/dict/data        新增字典数据
    PUT    /api/dict/data/:id    修改字典数据
    DELETE /api/dict/data/:id    删除字典数据
    GET    /api/oplog/list       操作日志分页(关键词: 用户名/路径)
    DELETE /api/oplog/:id        删除单条日志
    DELETE /api/oplog/clear      清空日志
    POST   /api/file/upload      文件上传(multipart, 字段名 file)
    GET    /api/file/list        文件列表(关键词: 文件名/上传人)
    DELETE /api/file/:id         删除文件(磁盘一并删除)
    GET    /uploads/...          上传文件静态访问(公开)

统一响应格式：{ code: 0 成功 | 40001 业务错误 | 40003 无权限 | 40004 未登录, msg, data }。

## 用脚手架生成新项目

    ./fde.sh ~/dev/my-admin github.com/me/my-admin "我的管理系统"

参数：目标目录（必填）、go 模块名（可选，默认 fde-admin）、应用名称（可选，默认 FDE-Fast-Framework）。
生成后：

    cd ~/dev/my-admin/server && go mod tidy && go run .
    cd ~/dev/my-admin/web && pnpm install && pnpm dev

## 代码生成器

菜单：系统管理 -> 代码生成器。填写表信息与字段，一键生成一套完整 CRUD（后端四件套 + 前端列表页）。

使用步骤：

    1. 填写表名(如 biz_customer)、业务名称(如 客户)、选择父菜单(不选则生成顶级菜单)
    2. 添加字段：字段名(列名)、中文名、类型，勾选 查询/列表/表单/必填
    3. 点击「预览」查看本批次代码，确认后点击「应用此预览」；修改参数后需要重新预览
    4. 重启后端服务(go run .)，刷新页面，新菜单出现在侧边栏即可使用

字段类型与前端控件：

    string   字符串     el-input（可模糊查询 LIKE）
    text     长文本     el-input textarea（数据库 type:text，可模糊查询）
    int      整数       el-input-number（查询为精确匹配）
    float    浮点数     el-input-number（查询为精确匹配）
    bool     布尔       el-switch（列表显示 启用/禁用 标签）
    datetime 时间       el-date-picker（RFC3339 存储；暂不支持作为查询条件）

每次生成写入 6 个文件：

    后端   internal/model/{name}.go        数据模型，init() 自动注册建表
    后端   internal/service/{name}.go      CRUD 业务逻辑
    后端   internal/api/{name}.go          HTTP 处理器
    后端   internal/router/{name}.go       路由注册，init() 自动挂载，无需改 router.go
    前端   web/src/api/{name}.ts           接口封装
    前端   web/src/views/generated/{name}/index.vue  列表页(搜索/分页/表单弹窗)

同时自动创建菜单与 4 个接口按钮(查询/新增/编辑/删除)，分配给 admin 及所有拥有父菜单的角色，
casbin 策略随之重建。生成器会校验表名/字段名合法性，并禁止与内置模块(user/role/menu/auth/codegen)
及内置字段(id/created_at/updated_at)冲突。

注意事项：

    - 生成后必须重启后端（重新编译才会挂载新路由并执行建表）
    - 同一张表只能生成一次（表已存在或菜单同名会被拒绝）；若要重新生成，先删除对应菜单并清掉数据表
    - 结构体名由表名自动推导（去掉 sys_/biz_/t_ 前缀转大驼峰），如 biz_customer -> Customer
    - 生成的是普通源码文件，可自由二次修改

生成批次与当前用户、项目、对象版本及文件基线绑定，30 分钟内有效；文件发生变化会拒绝覆盖。
内置智能体和 MCP 的 `apply_files` 返回待人工确认提示，模型填写 `approve=true` 不会授权写入。
表单编辑保存为草稿，运行端读取已激活版本；草稿保存、版本激活和代码生成是独立操作。

## 二次开发指引

新增一个业务页面：

1. 在 web/src/views 下新建 .vue 文件（如 views/order/index.vue）。
2. 菜单管理里新增「菜单」，type=菜单，路由 path/name、组件填 order/index。
3. 角色管理里给角色勾选该菜单，刷新后侧边栏即出现。

新增一个受 RBAC 保护的接口：

1. server/internal/router 注册路由（挂在 authed 分组下）。
2. 菜单管理里为该菜单新增「按钮」，apiPath/apiMethod 填接口路径与方法。
3. 角色管理里勾选该按钮，该角色用户即可调用（casbin 策略自动重建）。

## 生产环境注意事项

- 必须修改 jwt.secret 为强随机值，建议 seed.enabled 改为 false 并删除初始账号
- 将 mode 改为 release；CORS 当前全开（开发便利），生产请收紧 Origin 白名单
- PostgreSQL 部署前先在本地验证一次迁移（AutoMigrate 会建 sys_* 与 casbin_rule 表）

## 相对 gin-vue-admin 的取舍

保留：JWT(图形验证码+刷新令牌+多设备管理+令牌黑名单) + casbin RBAC + 动态菜单 +
前后端分离 + 统一响应 + 表单式代码生成器 + 字典管理 + 操作日志 + 文件上传 + zap 结构化日志。
未实现（可作为扩展方向）：redis 缓存、验证码/令牌状态存储换 Redis(当前为数据库/进程内存)、
对象存储(七牛/OSS/S3)、文件分片断点续传、用户行为埋点。
前端 element-plus 采用全量引入换取脚手架简单性，如在意包体可改为按需自动导入。

## 开源许可

本项目采用 [MIT License](LICENSE)。第三方依赖遵循各自的许可证。
