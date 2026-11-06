# AI 模块框架迁移开发文档：eino → Microsoft Agent Framework for Go

> 决策：AI 模块底层框架由 github.com/cloudwego/eino 迁移到
> github.com/microsoft/agent-framework-go（Microsoft Agent Framework 的 Go 实现，下称 MAF-Go）。
> 本文档为迁移开发的唯一依据，实施前请先通读。

> 实施状态：阶段 0/1/2/3 + B(MCP 运行时) + C(会话优化) + D(工程化加固) 全部完成；
> eino 依赖已移除；OTel 中间件已挂载(使用全局 provider, 配置 exporter 即生效)；
> service/ai_test.go 固化 6 项端到端测试(mock 网关 + 内存库 + 真实 MCP streamable)。

## 1. 调研事实（截至本文档撰写）

    - 仓库: https://github.com/microsoft/agent-framework-go
    - Go 包: github.com/microsoft/agent-framework-go
    - 可用版本: v0.1.0 / v0.1.1 / v0.2.0 / v0.2.1 / v0.3.0（当前最新 v0.3.0）
    - 状态: Public Preview，官方说明"正在上游核心代码库之外演进"，
      随采纳与反馈增加将与 MAF 生态进一步对齐
    - 安装: go get github.com/microsoft/agent-framework-go
    - 上游多语言框架: .NET 与 Python 实现位于 microsoft/agent-framework 仓库
    - 官方文档: https://learn.microsoft.com/en-us/agent-framework/

### 1.1 能力矩阵（Go 版）

已实现：

    - 核心 Agent: agent 包，Run / RunText / RunStream(流式)，消息类型(message 包)
    - 多供应商: provider/foundryprovider、provider/openaiprovider、
      provider/azureprovider、provider/otelprovider(OpenTelemetry 包装)
    - OpenAI provider 双协议:
        NewResponsesAgent        -> /v1/responses (Responses API)
        NewChatCompletionsAgent  -> /v1/chat/completions
        NewAgent                 -> 当前默认走 Responses
    - 工具: tool 包 + 自动工具调用中间件(AgentConfig.ToolAutoCall) + 工具审批
    - 中间件: 请求/响应中间件、日志、上下文提供器、OpenTelemetry
    - 编排: workflow 包，图式工作流(顺序/并发/分组协作/条件路由/子工作流/
      检查点/流式/人在环/时间旅行)
    - 技能: agent/skills 包(基于文件/内联定义/脚本的知识库, 供智能体发现使用)
    - RAG 与记忆: agent/textsearch(引用外部知识)、agent/memory(向量记忆)、
      Cosmos DB 会话历史、embedding 与 vectordata(内存/Azure AI Search)
    - 互操作: MCP、Agent2Agent(A2A)、AG-UI 集成示例

Go 版暂未实现(注意!):

    - Handoff 编排(多智能体交接)
    - Declarative Agents(声明式智能体)
    - AF Labs、DevUI
    - Foundry-hosted 部署

### 1.2 关键结论

    - MAF-Go 的 openaiprovider 原生同时支持 Responses 与 Chat Completions，
      与当前供应商的 apiMode 字段(默认 responses)完全对应
    - 底层使用 github.com/openai/openai-go/v3 SDK，支持 option.WithBaseURL
      自定义网关地址 -> 可无缝对接 sub2api / new-api 中转站
    - 内置 ToolAutoCall(自动函数工具调用)中间件，替代当前自写的 tool-calling 循环
    - eino 侧当前自实现的工具循环与 SSE 拼接逻辑可在迁移中删除

## 2. 关键 API 速览

### 2.1 创建供应商对应 Agent

    import (
        "github.com/microsoft/agent-framework-go/agent"
        "github.com/microsoft/agent-framework-go/provider/openaiprovider"
        "github.com/openai/openai-go/v3"
        "github.com/openai/openai-go/v3/option"
    )

    // 中转站: 通过 option.WithBaseURL 指向网关
    client := openai.NewClient(
        option.WithAPIKey(provider.APIKey),
        option.WithBaseURL(provider.BaseURL), // 如 https://gateway.example.com/v1
    )

    cfg := openaiprovider.AgentConfig{
        Instructions: agentSystemPrompt,   // 系统提示词(含技能注入后的文本)
        Config: agent.Config{
            Model: provider.Model,         // 默认模型
            Tools: toolList,               // 工具(可选)
        },
    }

    var a *agent.Agent
    if provider.APIMode == "chat" {
        a = openaiprovider.NewChatCompletionsAgent(client, cfg)
    } else {
        a = openaiprovider.NewResponsesAgent(client, cfg) // 默认
    }

### 2.2 对话(非流式/流式)

    // 非流式: 收集完整回复
    out := a.RunText(ctx, userInput).Collect()
    // 遍历回复文本 out.Text

    // 流式: RunText 返回 ResponseStream, 逐条消费
    for update := range a.RunText(ctx, userInput) {
        // update 携带增量文本/事件, 直接转 SSE 输出
    }

### 2.3 工具(自动调用)

    cfg.ToolAutoCall = &toolautocall.Config{} // 默认配置即可自动执行函数工具
    cfg.Config.Tools = []*tool.Tool{...}      // 由我们的 sys_ai_tools 转换而来

### 2.4 会话/历史

    - MAF 提供会话(agent.Session)与历史提供器(Cosmos DB 等)
    - 本项目会话继续用 sys_ai_sessions / sys_ai_messages 自管:
      每次 Run 前把历史消息转成 []*message.Message 传入,
      Run 后把回复落库(与现在 eino 版本同构)

## 3. 与现有 AI 模块的映射

    FDE 实体            MAF-Go 概念                     说明
    供应商(sys_ai_providers)  openaiprovider + openai-go client  baseUrl/apiKey/apiMode/model
                            直接映射; apiMode=chat -> NewChatCompletionsAgent,
                            apiMode=responses -> NewResponsesAgent
    智能体(sys_ai_agents)    AgentConfig.Instructions + agent.Config.Model
                            系统提示词+技能注入文本 = Instructions;
                            绑定的工具 = Config.Tools
    技能(sys_ai_skills)      agent/skills 或直接注入 Instructions
                            短期: 维持现状(拼进系统提示词);
                            中期: 迁移到 agent/skills 技能包
    工具(sys_ai_tools)       tool.Tool + ToolAutoCall 中间件
                            由 JSON Schema 字段构造 tool 定义;
                            自动调用替换现有手写循环
    MCP(sys_ai_mcp_servers)  MAF 的 MCP 集成(hostedtool/示例)
                            短期保留记录绑定; 中期接入真实 MCP 工具
    会话(sys_ai_sessions/消息) 自管历史 + 每次 Run 传消息列表  维持现有表结构不变

## 4. 迁移方案(分阶段)

阶段 0: 依赖与清理

    - go get github.com/microsoft/agent-framework-go@v0.3.0
    - 保留 eino 直至阶段 1 验收通过, 之后 go mod tidy 移除
      github.com/cloudwego/eino 与 eino-ext/components/model/openai

阶段 1: 非流式对话(chat 服务重写)

    - service/ai.go: 删除 buildChatModel/chat 分支与工具循环,
      替换为 openaiprovider 双协议 Agent
    - 保留 prepareChat(用户消息落库+历史组装)与消息落库逻辑
    - 验收: mock 网关(chat/completions 与 responses 两种)端到端回复正确

阶段 2: 流式(SSE)

    - 用 agent.RunText 的 ResponseStream 替换 eino Stream 与
      responsesStream 的自实现 SSE 解析, 输出 data: {"delta":...}
    - 验收: 两种 apiMode 下流式逐段输出且消息落库完整

阶段 3: 工具自动调用

    - sys_ai_tools -> tool.Tool 转换(含 JSON Schema 参数)
    - 启用 ToolAutoCall; 删除现有"工具未实现"占位循环
    - 验收: mock 网关返回 tool_call 时自动执行(先用内置占位工具)
      并产出最终回复

阶段 4(可选): 技能包 / MCP 运行时 / 工作流

    - 技能迁移到 agent/skills 包; MCP 接入真实 server;
      多智能体 workflow 作为后续功能

## 5. 迁移后的核心代码草图(service/ai.go)

    func buildAgent(agent *model.AiAgent, provider *model.AiProvider, tools []*tool.Tool) (*agent.Agent, error) {
        oclient := openai.NewClient(
            option.WithAPIKey(provider.APIKey),
            option.WithBaseURL(provider.BaseURL),
        )
        cfg := openaiprovider.AgentConfig{
            Instructions: systemPrompt, // prepareChat 拼好技能后的文本
            Config: agent.Config{Model: provider.Model, Tools: tools},
        }
        if provider.APIMode == "chat" {
            return openaiprovider.NewChatCompletionsAgent(oclient, cfg), nil
        }
        return openaiprovider.NewResponsesAgent(oclient, cfg), nil
    }

    func ChatAi(sessionID uint, content string) (*model.AiMessage, error) {
        messages, ag, err := prepareChat(sessionID, content) // 现有函数改造返回值
        ...
        a, err := buildAgent(ag, provider, agentTools)
        out := a.RunText(ctx, content).Collect()
        // out.Text 落库为 assistant 消息
    }

## 6. 风险与注意事项

    - Preview 状态: API 可能随版本变动, 升级需回归; 锁定 v0.3.0
    - 上游演进: Go 版在独立仓库演进, 功能对齐 .NET 有滞后(见 1.1 未实现清单)
    - Handoff 编排未实现: 多智能体交接暂不可用, 需要时用 workflow 子工作流替代
    - 中转站兼容性: 依赖网关支持 /v1/responses 与 /v1/chat/completions;
      new-api 类网关通常两者皆可, sub2api 需确认响应流字段
    - 会话历史: 继续自管 sys_ai_messages, 不启用框架会话存储,
      避免引入 Cosmos/向量库依赖
    - 审计与观测: 迁移后接入 provider/otelprovider 可补齐可观测性(可选)

## 7. 决策记录

    - 决策: AI 模块框架 eino -> Microsoft Agent Framework for Go
    - 原因: 官方多语言框架、生产级编排(workflow/检查点/人在环)、
      OpenAI provider 原生双协议(Responses/Chat)、内置自动工具调用与 MCP/A2A 生态
    - 代价: 依赖体积增大(openai-go v3 等)、Preview API 变动风险、
      需要重写 chat/stream/tool 三个服务函数
    - 替代方案(否决): 继续 eino(仅 chat/completions, 需自实现 responses);
      直接 openai-go v3(自研编排, 失去框架能力)
