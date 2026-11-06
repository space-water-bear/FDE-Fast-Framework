# API 约定（前后端契约）

本文档定义服务端与客户端之间固定的接口格式。新增接口、修改接口、或把前端组件抽到其他工程时，
都以此文档为准。

## 1. 通用响应信封

所有接口统一返回：

    {
      "code": 0,          // 业务码
      "msg": "success",   // 提示信息
      "data": null        // 业务数据(任意 JSON)
    }

code 取值固定：

    code    含义                        HTTP 状态码
    0       成功                        200
    40001   业务错误(msg 带具体原因)     200
    40003   无权限(casbin 拒绝)          403
    40004   未登录/令牌失效/被踢下线     401

服务端统一通过 internal/utils/response.go 的四个函数输出，禁止手写其他结构：

    utils.OK(c, data)         成功
    utils.Fail(c, "原因")     业务错误(40001)
    utils.NoAuth(c, "原因")   未登录/令牌失效(40004, HTTP 401)
    utils.Forbidden(c, "原因") 无权限(40003, HTTP 403)

注意：业务错误(40001)仍然返回 HTTP 200，由前端拦截器识别 code 后弹出 msg。

## 2. 鉴权与令牌

请求头：

    x-token: <访问令牌>                # 首选
    Authorization: Bearer <访问令牌>  # 兼容

登录接口 POST /api/auth/login 返回：

    { "code": 0, "data": { "token": "...", "refreshToken": "...", "user": { ... } } }

访问令牌过期或失效后，前端用刷新令牌换取新对（刷新令牌同步轮换，旧令牌立即吊销）：

    POST /api/auth/refresh   body: { "refreshToken": "..." }
    返回: { "code": 0, "data": { "token": "...", "refreshToken": "...", "user": { ... } } }

前端拦截器行为（web/src/utils/request.ts）：

    - 请求前自动给请求头加 x-token
    - 响应 code===0 时直接返回 data(业务层拿到的就是 data 本体)
    - HTTP 401 时自动调用 /api/auth/refresh 换新令牌并重试原请求
      (并发 401 共享同一次刷新；刷新失败才清除令牌跳登录页)
    - 其余业务错误自动 ElMessage.error(msg)

令牌校验三层：JWT 签名 -> 黑名单(sys_jwt_blacklists) -> 活跃设备(sys_active_tokens)。
被踢下线/单点登录挤掉的请求返回 401 + code 40004。

## 3. 请求格式

    - BaseURL: /api(前端 vite 代理到 127.0.0.1:8080)
    - GET: query 参数;分页固定 page(从 1 开始) + pageSize
    - POST/PUT: JSON body(Content-Type: application/json)
    - 文件上传: multipart/form-data,字段名固定为 file
    - 上传文件静态访问: /uploads/...(公开,无需鉴权)

分页列表接口统一返回：

    { "code": 0, "data": { "list": [...], "total": 12 } }

此结构被两个地方消费：

    - web/src/api/*.ts 的泛型返回类型
    - TablePageGrid 组件的 vxe proxy 映射: response: { result: 'list', total: 'total' }

## 4. 命名约定

    - URL: /api/<模块>/<动作>,例如 /api/user/list、/api/dict/type/:id
    - 参数/JSON 字段: camelCase(userId, pageSize, createdAt)
    - 数据库列: snake_case,由 gorm 自动映射
    - 时间字段: RFC3339 字符串(Go time.Time 默认序列化)

## 5. 常见返回结构示例

    /api/auth/userinfo  -> data: { user, roles, menus }
    /api/auth/captcha   -> data: { captchaEnabled, captchaId, picPath }
    /api/auth/devices   -> data: { list: [...] }
    /api/role/:id       -> data: { role, menuIds }
    /api/file/upload    -> data: { file: { id, name, url, ext, size, ... } }
    /api/menu/tree      -> data: { list: [...] }(树形,children 递归)

## 6. 错误处理约定

    - 业务失败一律 code 40001 + msg,不要往 data 里塞自定义错误结构(拦截器不识别)
    - 前端业务层捕获 reject 即可,提示已由拦截器统一完成
    - 需要前端特殊处理的失败(如重新登录)用 40004,不要用 40001

## 7. 新增接口接入清单

后端：

    1. internal/router/router.go 注册路由(受保护接口挂 authed 分组)
    2. internal/api/*.go 写 handler,成功 utils.OK / 失败 utils.Fail
    3. 分页接口返回 {list, total}
    4. 菜单管理新增按钮(apiPath/apiMethod),角色勾选 -> casbin 自动生效

前端：

    1. web/src/api/*.ts 声明函数与返回类型(泛型指向 data 本体)
    2. 页面调用,错误提示/鉴权/刷新自动生效
    3. 列表页建议用 TablePage 组件(见 docs/components/table-page.md)

代码生成器(系统管理 -> 代码生成器)产出的代码完全遵循本约定。

## 8. 契约源文件清单

    服务端   server/internal/utils/response.go    响应结构与业务码
            server/internal/middleware/jwt.go     鉴权与令牌校验
            server/internal/api/*.go              各接口
    客户端   web/src/utils/request.ts             拦截器/刷新/错误提示
            web/src/api/*.ts                      接口声明与类型
            web/src/components/table-page/        表格组件消费 {list,total}

## 9. 修改契约的影响面

若要调整响应结构或业务码，需要同步修改：

    - server/internal/utils/response.go(结构 + 常量)
    - web/src/utils/request.ts(code 判断逻辑)
    - web/src/components/table-page/TablePageGrid.vue(list/total 映射)
    - 涉及登录态的 40004 处理与 store/user.ts 的 token 存取

改动前先评估这三四处，避免只改一端导致前端拿不到 data 或分页空白。
