package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/microsoft/agent-framework-go/agent"
	"github.com/microsoft/agent-framework-go/message"
	"github.com/microsoft/agent-framework-go/provider/openaiprovider"
	"github.com/microsoft/agent-framework-go/provider/otelprovider"
	"github.com/microsoft/agent-framework-go/tool"
	"github.com/microsoft/agent-framework-go/tool/mcptool"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/responses"
	"go.uber.org/zap"
	"gorm.io/gorm"
	"os/exec"

	"fde-admin/internal/global"
	"fde-admin/internal/model"
)

// ---------- 供应商 ----------

func ListAiProviders(keyword string) ([]model.AiProvider, error) {
	var list []model.AiProvider
	q := global.DB.Model(&model.AiProvider{})
	if keyword != "" {
		like := "%" + keyword + "%"
		q = q.Where("name LIKE ? OR model LIKE ?", like, like)
	}
	if err := q.Order("id asc").Find(&list).Error; err != nil {
		return nil, err
	}
	return list, nil
}

func CreateAiProvider(name, baseURL, apiKey, apiMode, modelName string, temperature float64, enabled bool, remark string) error {
	if apiMode == "" {
		apiMode = "responses"
	}
	return global.DB.Create(&model.AiProvider{
		Name: name, BaseURL: baseURL, APIKey: apiKey, APIMode: apiMode, Model: modelName,
		Temperature: temperature, Enabled: enabled, Remark: remark,
	}).Error
}

func UpdateAiProvider(id uint, name, baseURL, apiKey, apiMode, modelName string, temperature float64, enabled bool, remark string) error {
	var p model.AiProvider
	if err := global.DB.First(&p, id).Error; err != nil {
		return err
	}
	p.Name = name
	p.BaseURL = baseURL
	if apiKey != "" {
		p.APIKey = apiKey
	}
	if apiMode != "" {
		p.APIMode = apiMode
	}
	p.Model = modelName
	p.Temperature = temperature
	p.Enabled = enabled
	p.Remark = remark
	return global.DB.Save(&p).Error
}

// FetchAiProviderModels 调用供应商 /models 接口拉取模型列表并保存
func FetchAiProviderModels(id uint) ([]string, error) {
	var p model.AiProvider
	if err := global.DB.First(&p, id).Error; err != nil {
		return nil, err
	}
	base := strings.TrimRight(p.BaseURL, "/")
	if base == "" {
		return nil, errors.New("供应商未配置接口地址")
	}
	url := base + "/models"
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+p.APIKey)
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("获取模型列表失败: HTTP %d %s", resp.StatusCode, truncateStr(string(body), 200))
	}
	var parsed struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, errors.New("解析模型列表失败")
	}
	ids := make([]string, 0, len(parsed.Data))
	for _, d := range parsed.Data {
		if d.ID != "" {
			ids = append(ids, d.ID)
		}
	}
	sort.Strings(ids)
	raw, _ := json.Marshal(ids)
	p.Models = string(raw)
	if err := global.DB.Save(&p).Error; err != nil {
		return nil, err
	}
	return ids, nil
}

func DeleteAiProvider(id uint) error {
	var n int64
	if err := global.DB.Model(&model.AiAgent{}).Where("provider_id = ?", id).Count(&n).Error; err != nil {
		return err
	}
	if n > 0 {
		return errors.New("仍有智能体引用该供应商，请先解除")
	}
	return global.DB.Delete(&model.AiProvider{}, id).Error
}

// ---------- 智能体 ----------

func ListAiAgents(keyword string) ([]model.AiAgent, error) {
	var list []model.AiAgent
	q := global.DB.Model(&model.AiAgent{})
	if keyword != "" {
		like := "%" + keyword + "%"
		q = q.Where("name LIKE ?", like)
	}
	if err := q.Preload("Skills").Preload("Tools").Preload("McpServers").Order("id asc").Find(&list).Error; err != nil {
		return nil, err
	}
	return list, nil
}

func CreateAiAgent(name, systemPrompt string, providerID uint, temperature float64, enabled bool, remark string, skillIDs, toolIDs, mcpIDs []uint) error {
	agent := &model.AiAgent{
		Name: name, ProviderID: providerID, SystemPrompt: systemPrompt,
		Temperature: temperature, Enabled: enabled, Remark: remark,
	}
	return global.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(agent).Error; err != nil {
			return err
		}
		return replaceAgentAssociations(tx, agent, skillIDs, toolIDs, mcpIDs)
	})
}

func UpdateAiAgent(id uint, name, systemPrompt string, providerID uint, temperature float64, enabled bool, remark string, skillIDs, toolIDs, mcpIDs []uint) error {
	var agent model.AiAgent
	if err := global.DB.First(&agent, id).Error; err != nil {
		return err
	}
	agent.Name = name
	agent.ProviderID = providerID
	agent.SystemPrompt = systemPrompt
	agent.Temperature = temperature
	agent.Enabled = enabled
	agent.Remark = remark
	return global.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Save(&agent).Error; err != nil {
			return err
		}
		return replaceAgentAssociations(tx, &agent, skillIDs, toolIDs, mcpIDs)
	})
}

func replaceAgentAssociations(tx *gorm.DB, agent *model.AiAgent, skillIDs, toolIDs, mcpIDs []uint) error {
	skills := []model.AiSkill{}
	if len(skillIDs) > 0 {
		if err := tx.Find(&skills, skillIDs).Error; err != nil {
			return err
		}
	}
	if err := tx.Model(agent).Association("Skills").Replace(skills); err != nil {
		return err
	}
	tools := []model.AiTool{}
	if len(toolIDs) > 0 {
		if err := tx.Find(&tools, toolIDs).Error; err != nil {
			return err
		}
	}
	if err := tx.Model(agent).Association("Tools").Replace(tools); err != nil {
		return err
	}
	mcps := []model.AiMcpServer{}
	if len(mcpIDs) > 0 {
		if err := tx.Find(&mcps, mcpIDs).Error; err != nil {
			return err
		}
	}
	return tx.Model(agent).Association("McpServers").Replace(mcps)
}

func DeleteAiAgent(id uint) error {
	var n int64
	if err := global.DB.Model(&model.AiSession{}).Where("agent_id = ?", id).Count(&n).Error; err != nil {
		return err
	}
	if n > 0 {
		return errors.New("仍有会话引用该智能体，请先删除相关会话")
	}
	if err := global.DB.Model(&model.AiBot{}).Where("agent_id = ?", id).Count(&n).Error; err != nil {
		return err
	}
	if n > 0 {
		return errors.New("仍有机器人引用该智能体，请先修改或删除相关机器人")
	}
	return global.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&model.AiAgent{Base: model.Base{ID: id}}).Association("Skills").Clear(); err != nil {
			return err
		}
		if err := tx.Model(&model.AiAgent{Base: model.Base{ID: id}}).Association("Tools").Clear(); err != nil {
			return err
		}
		if err := tx.Model(&model.AiAgent{Base: model.Base{ID: id}}).Association("McpServers").Clear(); err != nil {
			return err
		}
		return tx.Delete(&model.AiAgent{}, id).Error
	})
}

// ---------- 技能 ----------

func ListAiSkills(keyword string) ([]model.AiSkill, error) {
	var list []model.AiSkill
	q := global.DB.Model(&model.AiSkill{})
	if keyword != "" {
		like := "%" + keyword + "%"
		q = q.Where("name LIKE ? OR description LIKE ?", like, like)
	}
	if err := q.Order("id asc").Find(&list).Error; err != nil {
		return nil, err
	}
	return list, nil
}

func SaveAiSkill(id uint, name, description, prompt, remark string) error {
	s := model.AiSkill{Base: model.Base{ID: id}, Name: name, Description: description, Prompt: prompt, Remark: remark}
	if id == 0 {
		return global.DB.Create(&s).Error
	}
	var old model.AiSkill
	if err := global.DB.First(&old, id).Error; err != nil {
		return err
	}
	s.CreatedAt = old.CreatedAt
	return global.DB.Save(&s).Error
}

func DeleteAiSkill(id uint) error {
	return global.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("DELETE FROM sys_ai_agent_skills WHERE ai_skill_id = ?", id).Error; err != nil {
			return err
		}
		return tx.Delete(&model.AiSkill{}, id).Error
	})
}

// ---------- 工具 ----------

func ListAiTools(keyword string) ([]model.AiTool, error) {
	var list []model.AiTool
	q := global.DB.Model(&model.AiTool{})
	if keyword != "" {
		like := "%" + keyword + "%"
		q = q.Where("name LIKE ? OR description LIKE ?", like, like)
	}
	if err := q.Order("id asc").Find(&list).Error; err != nil {
		return nil, err
	}
	return list, nil
}

func SaveAiTool(id uint, name, description, schemaText string, enabled bool, remark string) error {
	t := model.AiTool{Base: model.Base{ID: id}, Name: name, Description: description, Schema: schemaText, Enabled: enabled, Remark: remark}
	if id == 0 {
		return global.DB.Create(&t).Error
	}
	var old model.AiTool
	if err := global.DB.First(&old, id).Error; err != nil {
		return err
	}
	t.CreatedAt = old.CreatedAt
	return global.DB.Save(&t).Error
}

func DeleteAiTool(id uint) error {
	return global.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("DELETE FROM sys_ai_agent_tools WHERE ai_tool_id = ?", id).Error; err != nil {
			return err
		}
		return tx.Delete(&model.AiTool{}, id).Error
	})
}

// ---------- MCP 服务 ----------

func ListAiMcpServers(keyword string) ([]model.AiMcpServer, error) {
	var list []model.AiMcpServer
	q := global.DB.Model(&model.AiMcpServer{})
	if keyword != "" {
		like := "%" + keyword + "%"
		q = q.Where("name LIKE ? OR url LIKE ?", like, like)
	}
	if err := q.Order("id asc").Find(&list).Error; err != nil {
		return nil, err
	}
	return list, nil
}

func SaveAiMcpServer(id uint, name, transport, url, command, args, headers string, enabled bool, remark string) error {
	m := model.AiMcpServer{Base: model.Base{ID: id}, Name: name, Transport: transport, URL: url, Command: command, Args: args, Headers: headers, Enabled: enabled, Remark: remark}
	if id == 0 {
		return global.DB.Create(&m).Error
	}
	var old model.AiMcpServer
	if err := global.DB.First(&old, id).Error; err != nil {
		return err
	}
	m.CreatedAt = old.CreatedAt
	return global.DB.Save(&m).Error
}

func DeleteAiMcpServer(id uint) error {
	return global.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("DELETE FROM sys_ai_agent_mcp WHERE ai_mcp_server_id = ?", id).Error; err != nil {
			return err
		}
		return tx.Delete(&model.AiMcpServer{}, id).Error
	})
}

// ---------- 会话 ----------

func ListAiSessions(keyword string, page, pageSize int) ([]model.AiSession, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}
	q := global.DB.Model(&model.AiSession{})
	if keyword != "" {
		like := "%" + keyword + "%"
		q = q.Where("title LIKE ?", like)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var list []model.AiSession
	if err := q.Order("id desc").Offset((page - 1) * pageSize).Limit(pageSize).Find(&list).Error; err != nil {
		return nil, 0, err
	}
	return list, total, nil
}

func CreateAiSession(title string, agentID, userID uint) (*model.AiSession, error) {
	if title == "" {
		title = "新会话"
	}
	s := &model.AiSession{Title: title, AgentID: agentID, UserID: userID}
	if err := global.DB.Create(s).Error; err != nil {
		return nil, err
	}
	return s, nil
}

func DeleteAiSession(id uint) error {
	return global.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("session_id = ?", id).Delete(&model.AiMessage{}).Error; err != nil {
			return err
		}
		return tx.Delete(&model.AiSession{}, id).Error
	})
}

func ListAiMessages(sessionID uint) ([]model.AiMessage, error) {
	var list []model.AiMessage
	if err := global.DB.Where("session_id = ?", sessionID).Order("id asc").Find(&list).Error; err != nil {
		return nil, err
	}
	return list, nil
}

// ---------- 聊天(Microsoft Agent Framework for Go) ----------

// 上下文长度控制(防止长对话 token 爆炸)
const (
	aiMaxContextMessages = 20 // 最多携带最近 20 条消息
	aiMaxContextChars    = 12000
)

// loadChatAgent 加载会话对应的智能体及其关联数据
func loadChatAgent(sessionID uint) (*model.AiAgent, error) {
	var session model.AiSession
	if err := global.DB.First(&session, sessionID).Error; err != nil {
		return nil, errors.New("会话不存在")
	}
	var agent model.AiAgent
	if err := global.DB.Preload("Skills").Preload("Tools").Preload("McpServers").First(&agent, session.AgentID).Error; err != nil {
		return nil, errors.New("智能体不存在")
	}
	if !agent.Enabled {
		return nil, errors.New("智能体已禁用")
	}
	return &agent, nil
}

// buildSystemPrompt 组装系统提示词(含技能注入)
func buildSystemPrompt(agent *model.AiAgent) string {
	systemPrompt := agent.SystemPrompt
	if len(agent.Skills) > 0 {
		var sb strings.Builder
		sb.WriteString(systemPrompt)
		sb.WriteString("\n\n【可用技能】\n")
		for _, s := range agent.Skills {
			sb.WriteString("- " + s.Name + ": " + s.Prompt + "\n")
		}
		systemPrompt = sb.String()
	}
	return systemPrompt
}

// trimHistory 按条数与总字符数裁剪历史(保留最近消息)
func trimHistory(history []model.AiMessage) []model.AiMessage {
	if len(history) > aiMaxContextMessages {
		history = history[len(history)-aiMaxContextMessages:]
	}
	total := 0
	for _, m := range history {
		total += len(m.Content)
	}
	for len(history) > 1 && total > aiMaxContextChars {
		total -= len(history[0].Content)
		history = history[1:]
	}
	return history
}

// historyToMessages 将历史记录转为 MAF 消息
func historyToMessages(history []model.AiMessage) []*message.Message {
	messages := make([]*message.Message, 0, len(history))
	for _, m := range history {
		switch m.Role {
		case "user":
			messages = append(messages, message.NewText(m.Content))
		case "assistant":
			messages = append(messages, &message.Message{
				Role:     message.RoleAssistant,
				Contents: message.Contents{&message.TextContent{Text: m.Content}},
			})
		}
	}
	return messages
}

// prepareChatContext 组装上下文(系统提示词 + 裁剪后的历史), 不保存用户消息
func prepareChatContext(sessionID uint) ([]*message.Message, *model.AiAgent, string, error) {
	agent, err := loadChatAgent(sessionID)
	if err != nil {
		return nil, nil, "", err
	}
	history, err := ListAiMessages(sessionID)
	if err != nil {
		return nil, nil, "", err
	}
	history = trimHistory(history)
	return historyToMessages(history), agent, buildSystemPrompt(agent), nil
}

// saveUserMessage 保存用户消息并自动生成会话标题
func saveUserMessage(sessionID uint, content string) error {
	if err := global.DB.Create(&model.AiMessage{SessionID: sessionID, Role: "user", Content: content}).Error; err != nil {
		return err
	}
	var session model.AiSession
	if err := global.DB.First(&session, sessionID).Error; err != nil {
		return nil
	}
	if session.Title == "" || session.Title == "新会话" {
		title := content
		if runes := []rune(title); len(runes) > 20 {
			title = string(runes[:20])
		}
		global.DB.Model(&session).Update("title", title)
	}
	return nil
}

// runAgentAndSave 构建 Agent 并执行一轮对话, 保存助手消息
func runAgentAndSave(sessionID uint, messages []*message.Message, ag *model.AiAgent, systemPrompt string) (*model.AiMessage, error) {
	started := time.Now()
	provider, err := loadProvider(ag)
	if err != nil {
		logChatResult(sessionID, "", started, 0, err)
		return nil, err
	}
	mcpTools, closeMcp := buildMcpTools(ag)
	defer closeMcp()
	allTools := append(buildTools(ag), mcpTools...)
	a, err := buildAgent(ag, provider, allTools, systemPrompt)
	if err != nil {
		logChatResult(sessionID, provider.APIMode, started, 0, err)
		return nil, err
	}
	resp, err := a.Run(context.Background(), messages).Collect()
	if err != nil {
		logChatResult(sessionID, provider.APIMode, started, 0, err)
		return nil, err
	}
	final := resp.String()
	if strings.TrimSpace(final) == "" {
		err = errors.New("模型未返回有效回复")
		logChatResult(sessionID, provider.APIMode, started, 0, err)
		return nil, err
	}
	assistant := &model.AiMessage{SessionID: sessionID, Role: "assistant", Content: final}
	if err := global.DB.Create(assistant).Error; err != nil {
		logChatResult(sessionID, provider.APIMode, started, 0, err)
		return nil, err
	}
	recordAiUsage(sessionID, ag.ID, provider.ID, provider.Model, resp.Usage(), time.Since(started))
	logChatResult(sessionID, provider.APIMode, started, len(final), nil)
	return assistant, nil
}

// loadProvider 加载智能体供应商并校验
func loadProvider(agent *model.AiAgent) (*model.AiProvider, error) {
	var provider model.AiProvider
	if err := global.DB.First(&provider, agent.ProviderID).Error; err != nil {
		return nil, errors.New("供应商不存在")
	}
	if !provider.Enabled {
		return nil, errors.New("供应商已禁用")
	}
	if provider.APIKey == "" {
		return nil, errors.New("供应商未配置 API Key")
	}
	if provider.Model == "" {
		return nil, errors.New("供应商未配置模型")
	}
	return &provider, nil
}

// schemaFuncTool 将 sys_ai_tools 的 JSON Schema 适配为 MAF-Go 可调用工具
type schemaFuncTool struct {
	name        string
	description string
	schema      any
	call        AiToolFunc
}

func (t *schemaFuncTool) Name() string        { return t.name }
func (t *schemaFuncTool) Description() string { return t.description }
func (t *schemaFuncTool) Schema() any         { return t.schema }
func (t *schemaFuncTool) ReturnSchema() any   { return map[string]any{"type": "object"} }
func (t *schemaFuncTool) Call(ctx context.Context, args string) (any, error) {
	if t.call != nil {
		return t.call(ctx, args)
	}
	return map[string]any{"error": fmt.Sprintf("工具 %s 未配置可执行实现", t.name)}, nil
}

// buildTools 将智能体绑定的工具转换为 MAF-Go 工具
func buildTools(agent *model.AiAgent) []tool.Tool {
	tools := make([]tool.Tool, 0, len(agent.Tools))
	for _, t := range agent.Tools {
		schema := any(map[string]any{"type": "object"})
		if strings.TrimSpace(t.Schema) != "" {
			var parsed map[string]any
			if json.Unmarshal([]byte(t.Schema), &parsed) == nil {
				schema = parsed
			}
		}
		call, _ := lookupAiToolFunc(t.Name)
		tools = append(tools, &schemaFuncTool{name: t.Name, description: t.Description, schema: schema, call: call})
	}
	return tools
}

// buildMcpTools 连接智能体绑定的 MCP 服务并枚举其工具; 返回的 cleanup 应在聊天结束后调用
func buildMcpTools(agent *model.AiAgent) ([]tool.Tool, func()) {
	out := make([]tool.Tool, 0)
	var sessions []*mcp.ClientSession
	cleanup := func() {
		for _, s := range sessions {
			_ = s.Close()
		}
	}
	for _, s := range agent.McpServers {
		if !s.Enabled {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		tools, session, err := connectMcpTools(ctx, &s)
		cancel()
		if err != nil {
			global.Logger.Error(fmt.Sprintf("MCP 服务 %s 连接失败: %v", s.Name, err))
			continue
		}
		sessions = append(sessions, session)
		out = append(out, tools...)
	}
	return out, cleanup
}

// connectMcpTools 按传输类型连接单个 MCP 服务并枚举工具, 返回会话(由调用方负责关闭)
func connectMcpTools(ctx context.Context, s *model.AiMcpServer) ([]tool.Tool, *mcp.ClientSession, error) {
	var transport mcp.Transport
	switch s.Transport {
	case "stdio":
		args := strings.Fields(s.Args)
		transport = &mcp.CommandTransport{Command: exec.Command(s.Command, args...)}
	case "streamable":
		transport = &mcp.StreamableClientTransport{Endpoint: s.URL, HTTPClient: mcpHTTPClient(s.Headers)}
	default: // sse
		transport = &mcp.SSEClientTransport{Endpoint: s.URL, HTTPClient: mcpHTTPClient(s.Headers)}
	}
	session, err := mcptool.Connect(ctx, transport)
	if err != nil {
		return nil, nil, err
	}
	tools, err := mcptool.ListTools(ctx, session)
	if err != nil {
		_ = session.Close()
		return nil, nil, err
	}
	return tools, session, nil
}

// mcpHTTPClient 解析 headers JSON 并构造带超时与自定义头的 HTTP 客户端
func mcpHTTPClient(headersJSON string) *http.Client {
	client := &http.Client{Timeout: 10 * time.Second}
	if strings.TrimSpace(headersJSON) == "" {
		return client
	}
	var h map[string]string
	if json.Unmarshal([]byte(headersJSON), &h) != nil || len(h) == 0 {
		return client
	}
	client.Transport = headerRoundTripper{base: http.DefaultTransport, headers: h}
	return client
}

type headerRoundTripper struct {
	base    http.RoundTripper
	headers map[string]string
}

func (t headerRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	for k, v := range t.headers {
		req.Header.Set(k, v)
	}
	return t.base.RoundTrip(req)
}

// buildAgent 依据供应商与智能体构建 MAF-Go Agent(双协议)
func buildAgent(ag *model.AiAgent, provider *model.AiProvider, tools []tool.Tool, systemPrompt string) (*agent.Agent, error) {
	opts := []option.RequestOption{option.WithAPIKey(provider.APIKey)}
	if provider.BaseURL != "" {
		opts = append(opts, option.WithBaseURL(provider.BaseURL))
	}
	oclient := openai.NewClient(opts...)

	temp := ag.Temperature
	if temp <= 0 {
		temp = 0.7
	}
	cfg := openaiprovider.AgentConfig{
		Instructions: systemPrompt,
		Model:        provider.Model,
		Config: agent.Config{
			Tools:       tools,
			Middlewares: []agent.Middleware{otelprovider.NewMiddleware(otelprovider.MiddlewareConfig{})},
		},
	}
	if provider.APIMode == "chat" {
		cfg.RunOptions = append(cfg.RunOptions, openaiprovider.ChatCompletionNewParams(openai.ChatCompletionNewParams{
			Temperature: openai.Float(temp),
		}))
		return openaiprovider.NewChatCompletionsAgent(oclient, cfg), nil
	}
	cfg.RunOptions = append(cfg.RunOptions, openaiprovider.ResponsesNewParams(responses.ResponseNewParams{
		Temperature: openai.Float(temp),
	}))
	return openaiprovider.NewResponsesAgent(oclient, cfg), nil
}

// recordAiUsage 记录一次调用用量
func recordAiUsage(sessionID, agentID, providerID uint, modelName string, u message.UsageDetails, latency time.Duration) {
	global.DB.Create(&model.AiUsage{
		SessionID: sessionID, AgentID: agentID, ProviderID: providerID, Model: modelName,
		PromptTokens: u.InputTokenCount, CompletionTokens: u.OutputTokenCount,
		TotalTokens: u.TotalTokenCount, LatencyMs: latency.Milliseconds(),
	})
}

// AiUsageGroup 按供应商/智能体聚合的用量
type AiUsageGroup struct {
	ID     uint   `json:"id"`
	Name   string `json:"name"`
	Calls  int64  `json:"calls"`
	Tokens int64  `json:"tokens"`
}

// AiUsageSummary 用量汇总
type AiUsageSummary struct {
	TotalCalls     int64          `json:"totalCalls"`
	TotalTokens    int64          `json:"totalTokens"`
	TotalLatencyMs int64          `json:"totalLatencyMs"`
	ByProvider     []AiUsageGroup `json:"byProvider"`
	ByAgent        []AiUsageGroup `json:"byAgent"`
}

func ListAiUsages(page, pageSize int, agentID, providerID uint) ([]model.AiUsage, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}
	q := global.DB.Model(&model.AiUsage{})
	if agentID > 0 {
		q = q.Where("agent_id = ?", agentID)
	}
	if providerID > 0 {
		q = q.Where("provider_id = ?", providerID)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var list []model.AiUsage
	if err := q.Order("id desc").Offset((page - 1) * pageSize).Limit(pageSize).Find(&list).Error; err != nil {
		return nil, 0, err
	}
	return list, total, nil
}

func GetAiUsageSummary() (*AiUsageSummary, error) {
	s := &AiUsageSummary{}
	var totals struct {
		Calls   int64
		Tokens  int64
		Latency int64
	}
	if err := global.DB.Model(&model.AiUsage{}).
		Select("count(*) as calls, coalesce(sum(total_tokens),0) as tokens, coalesce(sum(latency_ms),0) as latency").
		Scan(&totals).Error; err != nil {
		return nil, err
	}
	s.TotalCalls = totals.Calls
	s.TotalTokens = totals.Tokens
	s.TotalLatencyMs = totals.Latency
	providers, _ := ListAiProviders("")
	agents, _ := ListAiAgents("")
	groupsByProvider := map[uint]AiUsageGroup{}
	groupsByAgent := map[uint]AiUsageGroup{}
	var rows []struct {
		ProviderID uint
		AgentID    uint
		Calls      int64
		Tokens     int64
	}
	if err := global.DB.Model(&model.AiUsage{}).
		Select("provider_id, agent_id, count(*) as calls, coalesce(sum(total_tokens),0) as tokens").
		Group("provider_id, agent_id").Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, r := range rows {
		pn := fmt.Sprintf("供应商%d", r.ProviderID)
		for _, p := range providers {
			if p.ID == r.ProviderID {
				pn = p.Name
			}
		}
		g := groupsByProvider[r.ProviderID]
		g.ID = r.ProviderID
		g.Name = pn
		g.Calls += r.Calls
		g.Tokens += r.Tokens
		groupsByProvider[r.ProviderID] = g

		an := fmt.Sprintf("智能体%d", r.AgentID)
		for _, a := range agents {
			if a.ID == r.AgentID {
				an = a.Name
			}
		}
		ga := groupsByAgent[r.AgentID]
		ga.ID = r.AgentID
		ga.Name = an
		ga.Calls += r.Calls
		ga.Tokens += r.Tokens
		groupsByAgent[r.AgentID] = ga
	}
	for _, g := range groupsByProvider {
		s.ByProvider = append(s.ByProvider, g)
	}
	for _, g := range groupsByAgent {
		s.ByAgent = append(s.ByAgent, g)
	}
	return s, nil
}

func truncateStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// logChatResult 记录聊天请求结构化日志
func logChatResult(sessionID uint, mode string, started time.Time, chars int, err error) {
	if err != nil {
		global.Logger.Warn("ai chat failed",
			zap.Uint("session", sessionID), zap.String("mode", mode),
			zap.Duration("latency", time.Since(started)), zap.Error(err))
		return
	}
	global.Logger.Info("ai chat ok",
		zap.Uint("session", sessionID), zap.String("mode", mode),
		zap.Int("chars", chars), zap.Duration("latency", time.Since(started)))
}

// ChatAi 会话对话: 保存用户消息 -> MAF-Go Agent(自动工具调用) -> 保存助手回复
func ChatAi(sessionID uint, content string) (*model.AiMessage, error) {
	if err := saveUserMessage(sessionID, content); err != nil {
		return nil, err
	}
	messages, ag, systemPrompt, err := prepareChatContext(sessionID)
	if err != nil {
		return nil, err
	}
	return runAgentAndSave(sessionID, messages, ag, systemPrompt)
}

// RegenerateAiSession 重新生成最后一条助手回复
func RegenerateAiSession(sessionID uint) (*model.AiMessage, error) {
	var lastUser model.AiMessage
	if err := global.DB.Where("session_id = ? AND role = ?", sessionID, "user").Order("id desc").First(&lastUser).Error; err != nil {
		return nil, errors.New("没有可重新生成的消息")
	}
	if err := global.DB.Where("session_id = ? AND id > ?", sessionID, lastUser.ID).Delete(&model.AiMessage{}).Error; err != nil {
		return nil, err
	}
	messages, ag, systemPrompt, err := prepareChatContext(sessionID)
	if err != nil {
		return nil, err
	}
	return runAgentAndSave(sessionID, messages, ag, systemPrompt)
}

// ResendAiMessage 编辑用户消息后重发: 更新该消息并丢弃其后所有消息
func ResendAiMessage(sessionID, messageID uint, content string) (*model.AiMessage, error) {
	var msg model.AiMessage
	if err := global.DB.Where("id = ? AND session_id = ? AND role = ?", messageID, sessionID, "user").First(&msg).Error; err != nil {
		return nil, errors.New("消息不存在")
	}
	if err := global.DB.Model(&msg).Update("content", content).Error; err != nil {
		return nil, err
	}
	if err := global.DB.Where("session_id = ? AND id > ?", sessionID, messageID).Delete(&model.AiMessage{}).Error; err != nil {
		return nil, err
	}
	messages, ag, systemPrompt, err := prepareChatContext(sessionID)
	if err != nil {
		return nil, err
	}
	return runAgentAndSave(sessionID, messages, ag, systemPrompt)
}

// RenameAiSession 重命名会话
func RenameAiSession(sessionID uint, title string) error {
	title = strings.TrimSpace(title)
	if title == "" {
		return errors.New("标题不能为空")
	}
	return global.DB.Model(&model.AiSession{}).Where("id = ?", sessionID).Update("title", title).Error
}

// ClearAiSession 清空会话消息
func ClearAiSession(sessionID uint) error {
	return global.DB.Where("session_id = ?", sessionID).Delete(&model.AiMessage{}).Error
}

// StreamChatAiTo 流式对话(MAF-Go ResponseStream), 逐段回调 onDelta, 返回完整文本
func StreamChatAiTo(ctx context.Context, sessionID uint, content string, onDelta func(string)) (string, error) {
	return streamChatAiTo(ctx, sessionID, content, onDelta, true)
}

// StreamChatAiBotTo provides external chat without lending the server's tool
// credentials to an unmapped platform identity. Internal chat retains tools.
func StreamChatAiBotTo(ctx context.Context, sessionID uint, content string) (string, error) {
	return streamChatAiTo(ctx, sessionID, content, func(string) {}, false)
}

func streamChatAiTo(ctx context.Context, sessionID uint, content string, onDelta func(string), enableTools bool) (string, error) {
	started := time.Now()
	if err := saveUserMessage(sessionID, content); err != nil {
		logChatResult(sessionID, "", started, 0, err)
		return "", err
	}
	messages, ag, systemPrompt, err := prepareChatContext(sessionID)
	if err != nil {
		logChatResult(sessionID, "", started, 0, err)
		return "", err
	}
	provider, err := loadProvider(ag)
	if err != nil {
		logChatResult(sessionID, "", started, 0, err)
		return "", err
	}
	var allTools []tool.Tool
	if enableTools {
		mcpTools, closeMcp := buildMcpTools(ag)
		defer closeMcp()
		allTools = append(buildTools(ag), mcpTools...)
	}
	a, err := buildAgent(ag, provider, allTools, systemPrompt)
	if err != nil {
		logChatResult(sessionID, provider.APIMode, started, 0, err)
		return "", err
	}

	var full strings.Builder
	var usage message.UsageDetails
	for update, runErr := range a.Run(ctx, messages, agent.Stream(true)) {
		if runErr != nil {
			logChatResult(sessionID, provider.APIMode, started, full.Len(), runErr)
			return full.String(), runErr
		}
		if ctx.Err() != nil {
			logChatResult(sessionID, provider.APIMode, started, full.Len(), nil)
			return full.String(), nil
		}
		if update == nil {
			continue
		}
		usage.Add(update.Usage())
		delta := update.String()
		if delta != "" {
			full.WriteString(delta)
			onDelta(delta)
		}
	}
	recordAiUsage(sessionID, ag.ID, provider.ID, provider.Model, usage, time.Since(started))
	logChatResult(sessionID, provider.APIMode, started, full.Len(), nil)
	return full.String(), nil
}

// SaveAiAssistantMessage 保存助手消息(流式结束后调用)
func SaveAiAssistantMessage(sessionID uint, content string) error {
	if content == "" {
		return nil
	}
	return global.DB.Create(&model.AiMessage{SessionID: sessionID, Role: "assistant", Content: content}).Error
}
