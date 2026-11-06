# mcp-gateway

独立 MCP 网关进程: 对外提供 MCP streamable HTTP server(3 个工具), tools/call
翻译为对主应用 /api/mcp/* 的 HTTP 调用。仅依赖标准库与 go.mod 公共依赖。

## 构建

    cd server
    go build -o bin/mcp-gateway ./cmd/mcp-gateway

## 运行

    export GW_API_KEY=<主应用 mcp.api-key 配置值>     # 必填, 内部调用主应用的 key
    export GW_AUTH_TOKEN=<对外鉴权 token>             # 必填, 外部 MCP 客户端携带的 token
    export GW_ADDR=:8090                             # 可选, 默认 :8090
    export GW_UPSTREAM=http://127.0.0.1:8080         # 可选, 默认主应用地址
    ./bin/mcp-gateway

也可在运行目录放可选 config.yaml(键: addr / upstream / api_key / auth_token), 环境变量优先。

## 对外鉴权(外部 MCP 客户端 → 网关)

外部客户端必须携带以下任一请求头(值等于 GW_AUTH_TOKEN):
    Authorization: Bearer <GW_AUTH_TOKEN>
    x-api-key: <GW_AUTH_TOKEN>
缺失或不匹配返回 401。客户端配置示例(Claude/Cursor 等支持自定义 header 的客户端):
    "Authorization": "Bearer <GW_AUTH_TOKEN>"

## 与主应用的关系

- 主应用暴露 POST /api/mcp/formgen/schema/preview、/api/mcp/codegen/preview、
  /api/mcp/formgen/apply, 鉴权头 x-api-key, 响应统一信封 {code,msg,data}。
- 网关工具映射: generate_form_schema / generate_business_code / apply_files
  分别转发到上述三个端点, 请求带 x-api-key, HTTP 客户端超时 60s。
- 网关不落库、不执行业务逻辑, 只做 MCP 协议翻译与 API Key 转发。
