# 钉钉 Stream 官方协议核对记录

核对日期：2026-11-06。本次重新获取了钉钉官方主站的 [Stream 模式协议](https://open.dingtalk.com/document/direction/stream-mode-protocol-access-description)、官方开发者百科的[协议补充](https://open-dingtalk.github.io/developerpedia/docs/learn/stream/protocol/)、[常见问题](https://open-dingtalk.github.io/developerpedia/docs/learn/stream/faq/)，以及[机器人接收消息](https://open.dingtalk.com/document/orgapp/receive-message)、[机器人回复/发送消息](https://open.dingtalk.com/document/orgapp/robot-reply-and-send-messages)正文。

本记录区分官方要求、平台行为和项目本地策略，范围为企业内部应用机器人的文字问答。当前核心协议已逐项核对并通过本地契约测试，修正内容和接纳容量见下表；真实企业凭据与平台收发仍需单独验收。

## 当前检查结果

| 范围 | 实现及证据 | 本地结论 |
|---|---|---|
| 取票建连 | [client.go](../server/internal/dingtalkstream/client.go)固定官方注册 API，使用返回 WSS Path 与单次 ticket；重连重新注册，不重复用旧票、不输出凭据 | 通过 |
| 心跳 | SYSTEM ping 按原 messageId 与原始 data/opaque 响应，慢接纳或慢模型不阻塞探活；控制 Ping/Pong 仅为额外本地策略 | 通过 |
| 断连与重连 | disconnect 不发 ACK，最多 10 秒处理已经读入的回调；EOF 处理已读缓冲、重新取票重连；取消立即退出 | 通过 |
| 消息 ACK | 成功、失败和未知 topic 的 messageId/contentType/code/data 格式；[mock 协议测试](../server/internal/dingtalkstream/client_test.go) | 通过；本次补齐 404 data 的 JSON 格式 |
| 数值元数据 | 官方字段与示例允许 time 为数字或字符串；解析同时兼容，数字不经 float64，topic/messageId/contentType 仍严格为字符串 | 本次修正并验证 |
| 文字业务收发 | [manager](../server/internal/service/ai_bot_dingtalk.go)先持久化再 ACK，AI 异步运行；去重、应用/群/用户隔离、配置启停；HTTPS 临时地址到期/重定向/业务码检查 | [集成测试](../server/internal/service/ai_bot_dingtalk_test.go)通过 |
| 平台异常消息 | 无 text/content 的用量暂停通知不触发 AI | 本次补充 errorCode=20001 测试通过 |
| 满载接纳 | 传输层一个正在接纳、最多 32 个排队；下一条超过容量时明确 ACK500，不虚报 ACK200、不交给业务层；恢复后已排队消息按序处理 | 容量边界已测试，非无损投递保证 |

本次修正了两个细节：原全量 `map[string]string` 解码会拒绝官方允许的数字时间元数据，导致合法回调或心跳被误判断线；未知 topic 的 404 ACK 原先使用空 data，现改为合法 JSON 字符串。生产流程的普通文字回复仍经临时 HTTPS 地址发送，Stream ACK 不承载 AI 回答。

### 接纳与验收边界

满载或存储故障时无法接纳的机器人消息不能依赖 ACK500 自动补投，平台当前为 fire-forgot。业务层已接纳的消息有持久化去重与繁忙回复，但这不等于无限容量、进程崩溃后的自动重放或端到端零丢失保证。传输队列满载与业务模型队列满载是两处不同的接纳边界。

相关 Go 测试（含 race）、重复协议测试与 go vet 已通过；本次未更改前端。未使用真实企业凭据验证应用发布、实际单聊/群聊、运营商网络恢复或平台真实错误码，因此不能将本地检查等同正式平台验收，也不代表卡片或 EVENT 能力已开放。

## 官方要求与实现核对矩阵

| 环节 | 官方要求或边界 | 性质 | 代码及测试应核对的行为 |
|---|---|---|---|
| 应用凭据 | 企业自建应用使用 Client ID（AppKey）和 Client Secret（AppSecret） | 注册前提 | 不使用群通知机器人 SEC 密钥；Secret 不回显、不写日志 |
| 注册连接 | HTTPS POST `https://api.dingtalk.com/v1.0/gateway/connections/open`，请求包含 `clientId`、`clientSecret`、`subscriptions` | 必填协议 | 设置 JSON Content-Type/Accept；当前只订阅 CALLBACK 类型的机器人 topic |
| 订阅机器人 | 机器人 topic 为 `/v1.0/im/bot/messages/get` | 必填协议 | 不用 EVENT 的 `*` 代替机器人订阅；未实现卡片及 EVENT 时不擅自订阅 |
| 可选诊断参数 | `ua`、`localIp` 可选；ua 采用 `name-sdk-lang/version` 格式 | 可选字段 | 不能因不传 localIp 判断不合规；不能把本地诊断值当鉴权信息 |
| 连接返回值 | 返回 `endpoint` 和 `ticket` | 必填返回信息 | 缺失字段、非法地址不能进入正常已连接状态 |
| ticket | 有效期 90 秒，只能建立一条连接，不得缓存重复使用 | 平台约束 | 每次失败重连或平台断连均重新注册取票，不重复拨旧 ticket |
| endpoint | 真实 Host 与 Path 必须使用注册接口返回值，示例不能硬编码替代 | 必填协议 | 保留动态 Path；校验 WSS、目的主机和 URL 结构，避免任意地址带走 ticket |
| 外联域名 | 当前官方 FAQ 列出 `api.dingtalk.com:443`、`wss-open-connection.dingtalk.com:443`，不提供固定 IP 列表 | 当前部署边界 | TLS 出站域名放行，不能要求用户提供公网回调地址；域名发生官方变更后需复核限制 |
| 帧结构 | `specVersion`、`type`、`headers`、`data`；`data` 是 JSON 字符串 | 通用协议 | 不把 data 当直接嵌套对象；正确读取 CALLBACK 与 SYSTEM |
| headers 类型 | topic/contentType/messageId 为字符串；时间是毫秒时间戳，官方字段表与示例存在数字/字符串两种写法 | 兼容性要求 | `time` 为 JSON number 时也应能处理整帧，不能导致合法消息或心跳进入重连循环 |
| 机器人 ACK | 成功 code 200、message OK，headers 回传原 messageId 及 `contentType=application/json` | 建议遵循的诊断协议 | ACK data 可以是 JSON 字符串 `{"response":null}`；关联的是推送 messageId，不是自行生成的 ID |
| 失败及未知 topic | 协议定义 code 500 内部错误、404 topic 不支持 | 诊断协议 | 不伪造成功；错误响应不能包含 Secret、ticket、临时 URL 或消息原文 |
| 心跳发起方 | 平台发送 `SYSTEM`、topic `ping`；data 中含 opaque | 必需系统处理 | 及时返回成功 ACK，messageId 相同，data 与请求一致；慢模型不能阻塞探活 |
| 心跳响应 | 官方主站明确要求探活数据完整返回 | 必需系统处理 | 保留原 data，不重新生成 opaque 或把 JSON 字符串变成对象 |
| 主动空闲探活 | 正文没有要求客户端采用固定的 120 秒、5 秒等数值 | 本地策略 | 可以额外使用 WebSocket 控制 Ping/Pong，但不能替代服务端 SYSTEM ping |
| 平台断连 | `SYSTEM/disconnect` 不需要响应；平台不再下发新消息，但仍允许响应在途请求，静默 10 秒后关闭 TCP | 必需系统处理及平台行为 | 排空已读但尚未接纳的回调，在有界时间内完成源连接 ACK；不能一见 disconnect 就丢弃缓冲回调 |
| 断后重连 | 收到平台断连后需要重新注册和建连，旧票不可复用 | 必需恢复行为 | 网络断开、EOF、平台正常断连均能恢复；不能套用企业微信“其他实例接管后停止”的语义 |
| 退避策略 | 正文未指定固定的退避秒数或最大尝试次数 | 本地策略 | 指数退避、抖动、认证失败停连及取消退出需要有可验证行为，但不能称其数值为官方强制值 |
| 多连接 | 官方支持多个 Stream 通道，并随机选取通道推送 | 平台能力 | 不把企业微信单连接限制搬到钉钉；本项目可选择避免同应用重复配置，不等于实现自动负载均衡 |
| 机器人投递可靠性 | 机器人当前为 fire-forgot，不因网络超时自动重投；EVENT 的 ACK 与重投规则不同 | 平台可靠性边界 | 不依赖 500 或无 ACK 作为机器人重试队列；本地接纳失败和重启中断应如实记录 |
| 去重 | 即使 ACK 正确，也应考虑消息重复 | 应用处理要求 | 用稳定业务 msgId 持久化去重，不重复执行模型或发第二次回答 |
| 接收与回复分离 | Stream 用于接收回调；机器人回答可以使用消息携带的 sessionWebhook 发 HTTPS 请求 | 受支持的回复方式 | ACK 不承载最终 AI 回答，普通文字回答不必经 WebSocket 发回 |
| 临时回复地址 | `sessionWebhookExpiredTime` 表示该地址的实际到期时间 | 接收消息字段语义 | 发送前检查实际到期值；不把示例中约 90 分钟推断为永久统一有效期 |
| 回复格式 | 对 sessionWebhook POST 标准机器人消息，例如 text/content | 回复协议 | 目标为平台临时地址，使用 HTTPS；HTTP 与业务失败如实记录，不盲目补发 |
| 身份隔离 | conversationType 1 为单聊、2 为群聊；senderId 是外部身份 | 业务字段语义 | 按应用、会话、发送者隔离历史；不能把 isAdmin/senderStaffId 当系统内部授权 |
| 调用量超量通知 | 官方接收文档列出 errorCode 20001；此时可能没有 text/content | 平台异常消息 | 无正文不能误触发模型；不能把平台用量暂停当普通空问题执行 |

矩阵前半部分依据[主站协议](https://open.dingtalk.com/document/direction/stream-mode-protocol-access-description)与[官方协议补充](https://open-dingtalk.github.io/developerpedia/docs/learn/stream/protocol/)；域名、能力范围依据[FAQ](https://open-dingtalk.github.io/developerpedia/docs/learn/stream/faq/)；身份、到期和用量错误依据[机器人接收文档](https://open.dingtalk.com/document/orgapp/receive-message)，回复方式依据[机器人回复文档](https://open.dingtalk.com/document/orgapp/robot-reply-and-send-messages)。

## 本次重点发现与证据边界

1. **时间元数据存在两种官方写法。** 开发者百科字段表将 `headers.time` 列为 Number，但回调示例使用字符串；主站 `.md` 还提供了数字 time/eventBornTime 示例。实现需兼容辅助数值元数据，并保持 topic、messageId 等路由字段为合法字符串。单纯 `map[string]string` 解码全部 headers 会拒绝数字形式，需代码与 fixture 验证。
2. **机器人 ACK 不是可靠队列。** 回调接纳应与模型推理解耦；网络中断或接收过载不能通过“等待钉钉重投”来伪装已可靠保存。
3. **disconnect 的 10 秒是平台宽限，不是心跳周期。** 客户端可在处理完所有已接回调后重建连接；不能为重连立即取消尚未持久化的缓冲数据，也不能无限等待不响应的应用回调。
4. **未确认的数值不补成官方规则。** 当前引用文档未规定机器人 ACK 的统一固定秒数，未给出临时 sessionWebhook 的统一固定分钟/小时频率；不能套用企业微信或自定义群通知机器人的额度。

## 范围与验收记录

当前范围是普通文字问答。媒体、富文本、卡片回调、事件订阅、主动业务通知及卡片打字机效果均须另行接入对应协议和权限，不能因底层 Stream 建连成功就宣称已支持。

本地代码/测试核对项：取票与每次重取、动态 Path、数字/字符串时间元数据、SYSTEM ping 完整回显、断连排空、EOF 恢复、慢模型不阻塞心跳、回执先于 ACK、同 msgId 不重复执行、临时地址到期与回复错误、配置轮换/停用/退出、敏感信息不进入日志。最终代码及测试结果由本次审查补充。

真实企业环境仍需验证应用发布、Stream 接收方式、可见范围、单聊/群聊真实收发、平台心跳与网络恢复。本文研究只读取了官方公开资料，没有使用企业凭据，也没有进行平台实际授权或发送消息。
