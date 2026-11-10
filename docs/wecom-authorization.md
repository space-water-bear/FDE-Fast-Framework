# 企业微信机器人连接与文档授权说明

核对日期：2026-11-06。本文依据企业微信官方当前页面与官方 SDK，区分长连接凭据和成员授予的文档使用权限。用户指定的 [101468 文档](https://developer.work.weixin.qq.com/document/path/101468)标题为《API模式机器人文档使用说明》，页面标注最后更新日期为 2026/03/13。

## 配置时先区分两种权限

| 用途 | 配置内容 | 获得的能力 |
|---|---|---|
| 连接机器人消息通道 | 长连接专用 Bot ID、Secret | 通过 WebSocket 订阅消息并回复 |
| 使用企业微信文档、智能表格 | 成员在机器人设置中授权，随后取得 MCP 连接配置 | 调用官方开放的文档、智能表格工具 |

Bot ID、Secret 的连接认证不会自动授予文档操作权限。官方文档中的 **7 天有效期指文档使用授权**，不能据此认定长连接 Secret 每 7 天过期。依据：[长连接配置与订阅](https://developer.work.weixin.qq.com/document/path/101463)、[文档使用授权](https://developer.work.weixin.qq.com/document/path/101468)。

## 先完成文字问答的长连接配置

1. 在企业微信智能机器人配置中开启 API 模式，选择长连接。
2. 取得该机器人的 Bot ID 和长连接专用 Secret，在本系统的企业微信机器人配置中填写。
3. 选择问答智能体，启用后查看连接状态，再在企业微信中向机器人发送文字；群聊需按平台方式 @ 机器人。

长连接凭据不同于旧回调模式的 Token、EncodingAESKey，不应互相填入。企业微信 API 模式一次只能选择长连接或接收消息回调地址；切换模式会使另一种模式失效。[官方长连接文档](https://developer.work.weixin.qq.com/document/path/101463)

## 需要文档能力时，按官方步骤单独授权

前提是已经创建 API 模式机器人。成员在企业微信中按以下路径操作：

1. 进入工作台中的智能机器人，找到对应机器人并编辑。
2. 打开「可使用权限」，完成授权。
3. 授权成功后打开「查看使用方式」。
4. 根据实际 MCP 客户端需要，复制 streamableHTTP URL 或 JSON Config。

官方当前标明授权有效期为 **7 天**。应在授权失效后检查企业微信中的授权状态，并按官方页面重新处理需要的授权和连接配置。上述 URL、JSON Config 属于文档 MCP 的连接配置，**不要将其作为 Bot ID 或 Secret 填入长连接表单**。完整界面步骤以[企业微信官方授权文档](https://developer.work.weixin.qq.com/document/path/101468)为准。

授权后，官方 MCP 提供文档与智能表格的新建、内容写入以及部分表结构和记录操作；机器人作为所建文档的创建者，只能编辑自己创建的文档。这不是任意读取、修改成员全部文档的授权。[官方能力与范围说明](https://developer.work.weixin.qq.com/document/path/101468)

本项目的机器人入口当前提供文字问答，尚未加载文档 MCP 工具。完成上述平台授权不会自动启用本系统的文档写入能力；接入说明与页面链接用于指导平台配置，后续开放工具仍须另行完成权限与执行能力接入。

## 长连接运行时的注意事项

每个机器人同一时间仅允许一个有效长连接。新连接完成订阅会使旧连接收到 `disconnected_event` 并被断开；不要让同一 Bot ID 的两个实例持续相互重连。高可用应采用主备接管。[官方连接数量限制及断开事件](https://developer.work.weixin.qq.com/document/path/101463)

官方建议每 30 秒发送业务 `ping` 心跳。官方 SDK 使用 Bot ID、Secret 发送 `aibot_subscribe`，通过响应中的 `req_id` 和 `errcode` 关联认证、心跳和回复回执；网络断开采用指数退避，延迟上限 30 秒。收到其他连接接管事件时，SDK 会停止自动重连。[官方协议](https://developer.work.weixin.qq.com/document/path/101463)、[官方 SDK 对照源码](https://github.com/WecomTeam/aibot-node-sdk/blob/80615b987ef69c6028ad764924609247c0725955/src/ws.ts)

本次核对仅进行了官方文档与源码读取，没有使用真实企业凭据完成授权或消息收发。实际授权状态以企业微信界面为准。
