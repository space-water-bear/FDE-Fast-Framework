# 钉钉机器人 Stream 长连接接入

核对日期：2026-11-06。依据钉钉官方协议、开发者百科和官方 Go SDK；本次范围为企业内部应用机器人的双向文字问答。Stream 用于接收机器人消息，回答仍通过消息携带的临时 HTTPS `sessionWebhook` 发送，不代表已实现卡片打字机效果或其他事件订阅。[官方协议](https://open-dingtalk.github.io/developerpedia/docs/learn/stream/protocol/)、[机器人回复/发送消息](https://open.dingtalk.com/document/orgapp/robot-reply-and-send-messages)

## 平台配置与旧 HTTP 模式迁移

1. 在钉钉开发者后台创建或打开企业内部应用，添加机器人能力。
2. 将机器人的消息接收模式选择为 **Stream 模式**，按后台要求保存机器人配置并发布应用，设置需要访问的成员范围。
3. 在应用凭证中取得 **Client ID（原 AppKey）** 与 **Client Secret（原 AppSecret）**。不要使用自定义群通知机器人的 SEC 签名密钥。
4. 在本系统编辑钉钉机器人，填写这两个凭据、选择问答智能体，保存并启用后检查连接状态。旧 HTTP 记录原 AppSecret 本身可作为对应应用的 Client Secret，但首次补齐 Client ID 时需重新填写该密钥以确认归属；切换到另一应用时必须填写新应用的 Client Secret。
5. 单聊直接发送文字，群聊中 @ 机器人发送文字。切换为 Stream 后，不再配置本系统公网回调地址，也不再对入站消息执行旧 HTTP 回调的 timestamp/sign 校验。

旧记录没有应用标识时，原会话历史仍保留供管理端查看；补齐 Client ID 后使用明确的应用身份隔离新上下文与去重回执，不把未知归属的旧历史自动带入。

钉钉官方教程将机器人能力作为应用扩展配置，并明确要求 Stream 接收方式；后台中的新旧凭据名称可能处于过渡期，应按应用凭证实际页面对应填写。[创建应用与机器人](https://open-dingtalk.github.io/developerpedia/docs/explore/tutorials/stream/bot/python/create-bot/)、[接收模式说明](https://open-dingtalk.github.io/developerpedia/docs/learn/bot/appbot/receive/)

服务不需要公网入站地址，但需能够出站连接 `api.dingtalk.com:443` 与 `wss-open-connection.dingtalk.com:443`；使用临时会话回复时还需访问消息中的官方 `oapi.dingtalk.com` 地址。Stream 官方 FAQ 未提供固定 IP 清单，应按域名放行。[官方安全外联说明](https://open-dingtalk.github.io/developerpedia/docs/learn/stream/faq/)

## 长连接与回执协议

| 环节 | 官方规定 | 本次接入边界 |
|---|---|---|
| 注册连接 | POST `https://api.dingtalk.com/v1.0/gateway/connections/open`，发送 `clientId`、`clientSecret`、`subscriptions`；`ua`、`localIp` 为可选诊断信息 | 只订阅机器人 CALLBACK topic `/v1.0/im/bot/messages/get`，不订阅所有 EVENT |
| 取得凭证 | 返回 `endpoint` 和 `ticket`；ticket 有效期 90 秒，且只能建立一条连接，不得存储或重复使用 | 每次重连重新注册获取新 ticket，不能缓存旧连接凭据 |
| 建立连接 | 使用返回 endpoint 的 Host 和 Path，并添加 ticket 查询参数；文档中的域名、Path 是示例，不能拿示例替代真实返回值 | 要求 TLS/WSS，校验官方目标及 URL 结构，保留服务返回路径；不把用户填写的任意 URL 作为连接目标 |
| 接收数据 | `specVersion`、`type`、`headers`、`data`；其中 `data` 是包含 JSON 的字符串，不是直接嵌套对象 | 仅将对应机器人 CALLBACK 的 data 交给文字问答处理 |
| 机器人 ACK | `code=200`、`message=OK`，headers 透传 `messageId` 并声明 `contentType=application/json`；data 可固定为字符串 `{"response":null}` | ACK 表示接收状态，不将长时间 AI 推理阻塞在 ACK 路径上 |
| 业务心跳 | 服务推送 `type=SYSTEM`、`topic=ping`，客户端响应时在 data 中返回同一 `opaque` | 这是服务端发起的应用层探活，不能照搬企业微信的客户端定时 JSON ping |
| 优雅断连 | `SYSTEM/disconnect` 不要求 ACK；平台停止下发新消息，允许处理在途请求，静默 10 秒后关闭 TCP | 重新注册并建立连接；这是正常连接管理事件，不等于其他实例接管后永久停止 |
| 多连接 | 官方支持多个 Stream 通道，平台随机选择通道推送，可用于负载均衡 | 不套用企业微信“一机器人仅一个有效连接”的规则；当前应用仍自行避免同配置重复启动 |

以上字段及系统消息处理以[官方 Stream 协议接入说明](https://open.dingtalk.com/document/direction/stream-mode-protocol-access-description)和[官方开发者百科的协议补充](https://open-dingtalk.github.io/developerpedia/docs/learn/stream/protocol/)为准。

官方协议 FAQ 区分了事件和机器人消息：EVENT 支持相对可靠推送，不回复 ACK 可引起超时重投；机器人消息当前采用 fire-forgot，不会因为网络超时自动重投。机器人 ACK 仍建议按协议回复，以便诊断。当前核对的官方文档未给出机器人消息固定的 ACK 秒数，不能把本系统的处理超时称为平台强制时限；应用仍需要幂等处理可能重复到达的数据。[官方 ACK 与重复推送说明](https://open-dingtalk.github.io/developerpedia/docs/learn/stream/protocol/)

## 文字回答与身份

机器人回调包含 `msgId`、`conversationId`、`conversationType`、`senderId`、`chatbotCorpId`、`robotCode` 等信息。`conversationType=1` 表示单聊，`2` 表示群聊；发送者 ID 应作为外部不透明身份使用，不能把 `isAdmin` 或 `senderStaffId` 自动视为本系统内部权限。[机器人消息字段](https://open.dingtalk.com/document/orgapp/receive-message)

回答可以向 `sessionWebhook` POST 标准文字消息。`sessionWebhookExpiredTime` 是该临时地址的过期时间，发送前应按实际字段判断；官方示例的有效时长不应被硬编码为所有消息的统一期限。临时地址不作为永久群 Webhook 保存，过期、HTTP 失败或平台业务错误均需要记录为回复失败，不盲目重发。[回复消息方式](https://open.dingtalk.com/document/orgapp/robot-reply-and-send-messages)、[过期时间字段定义](https://open.dingtalk.com/document/orgapp/receive-message)

本次只在模型完成后发送一次文字回答。Stream 连接本身不提供把普通文字反复编辑为打字机效果的能力；如后续需要流式卡片，应单独接入卡片实例、更新接口与相应权限，不能直接照搬企业微信的 `aibot_respond_msg/stream.id`。[钉钉官方 Stream FAQ](https://open-dingtalk.github.io/developerpedia/docs/learn/stream/faq/)

## 官方 SDK 版本与实现核对

本次读取的官方 Go SDK 最新标签为 `v0.9.2-beta.1`，对应 commit `d8d6e1f19d68b231c1514fdac973d866a2882b93`；此前稳定标签为 `v0.9.1`。官方推荐采用 SDK，也公开协议供自行实现或改进 SDK。版本应固定核对，不能仅依赖浮动 main。[官方 SDK](https://github.com/open-dingtalk/dingtalk-stream-sdk-go)、[固定版本源码](https://github.com/open-dingtalk/dingtalk-stream-sdk-go/tree/d8d6e1f19d68b231c1514fdac973d866a2882b93)

该版本已处理心跳、重连、连接归属和并发 handler 上限，但若直接使用还需结合本项目要求包装：默认 Info 连接日志包含等同 ticket 的 sessionId，Debug wire 日志包含凭据请求体和返回 ticket；默认文字 replier 在 HTTP 200 时未检查业务 `errcode`。本项目的凭据保密、目标地址校验、业务错误码检查、启停与连接状态仍需显式保证。[client.go](https://github.com/open-dingtalk/dingtalk-stream-sdk-go/blob/d8d6e1f19d68b231c1514fdac973d866a2882b93/client/client.go)、[chatbot_replier.go](https://github.com/open-dingtalk/dingtalk-stream-sdk-go/blob/d8d6e1f19d68b231c1514fdac973d866a2882b93/chatbot/chatbot_replier.go)

SDK 在 disconnect 时回复 ACK 后关闭，与官方正文的“不需要响应”存在实现差异；协议核对优先采用正文，不把 SDK 的所有细节等同强制协议要求。SDK 中 120 秒空闲检测、5 秒控制 Ping 等待等也是客户端默认策略，不能据此宣称平台要求所有客户端采用同样数值。

本项目采用公开协议适配现有 WebSocket 库，只实现机器人通道，不直接引入上述 SDK。默认网络重连采用带抖动的指数退避，基础 1 秒、上限 30 秒；每次重连重新取得 ticket。服务器 SYSTEM ping 原样回显，另使用本地 120 秒空闲/5 秒控制 Ping 探活策略。平台 disconnect 时最多用 10 秒排空已经收到的回调并确认接纳后再重新注册；父上下文取消则立即退出。凭据明确失败停止重连，修正配置并保存后重建连接。

Client ID、Secret、ticket、临时回复 URL 和消息原文不会进入连接日志。回复同时检查 HTTPS 状态与明确 `errcode=0`，过期或失败不自动重发。只轮换 Secret 保留同应用上下文，换 Client ID 则隔离应用身份、会话和去重回执。

## 验证边界

本地测试已覆盖连接注册、ticket 每次重取、WSS 目标校验、SYSTEM ping 原样 opaque 响应、disconnect 排空后重连、网络 EOF 时处理已读回调、机器人 ACK、慢模型不阻塞探活、重复消息不重复执行、配置启停与 Secret 轮换、换应用隔离、繁忙回执保留及回复业务错误码。Go 测试（含 race）、静态检查、前端构建与配置页操作已通过。

最终逐条复核见[钉钉 Stream 官方协议核对记录](dingtalk-stream-audit.md)。其中补齐了数字/字符串时间元数据兼容和 404 ACK 的 JSON data；传输层最多 32 条排队（另有一条正在接纳），超额明确返回失败。机器人 fire-forgot 不提供自动补投，不能把该有界接纳实现当作零丢失消息队列。

真实企业凭据下仍须确认应用发布、Stream 模式、可见范围、单聊与群聊实际收发、网络断开恢复及临时会话回复。本文研究未使用实际企业凭据，没有执行真实平台授权或发送消息。本次尚未从核对的 primary 来源确认临时会话 Webhook 的统一频控数值，不以群通知机器人的限额推断该接口额度；实现与运行验收需检查平台真实返回的错误码。
