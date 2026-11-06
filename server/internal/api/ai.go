package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"fde-admin/internal/model"
	"fde-admin/internal/service"
	"fde-admin/internal/utils"
)

// ---------- 供应商 ----------

type aiProviderReq struct {
	Name        string  `json:"name" binding:"required"`
	BaseURL     string  `json:"baseUrl"`
	APIKey      string  `json:"apiKey"`
	APIMode     string  `json:"apiMode"`
	Model       string  `json:"model"`
	Temperature float64 `json:"temperature"`
	Enabled     *bool   `json:"enabled"`
	Remark      string  `json:"remark"`
}

func providerToMap(p model.AiProvider) gin.H {
	models := []string{}
	if p.Models != "" {
		_ = json.Unmarshal([]byte(p.Models), &models)
	}
	return gin.H{
		"id": p.ID, "name": p.Name, "baseUrl": p.BaseURL, "apiKey": p.APIKey,
		"apiMode": p.APIMode, "model": p.Model, "models": models,
		"temperature": p.Temperature, "enabled": p.Enabled, "remark": p.Remark,
		"createdAt": p.CreatedAt,
	}
}

func ListAiProviders(c *gin.Context) {
	list, err := service.ListAiProviders(c.Query("keyword"))
	if err != nil {
		utils.Fail(c, err.Error())
		return
	}
	items := make([]gin.H, 0, len(list))
	for _, p := range list {
		items = append(items, providerToMap(p))
	}
	utils.OK(c, gin.H{"list": items, "total": len(items)})
}

func FetchAiProviderModels(c *gin.Context) {
	id, err := utils.ParseID(c)
	if err != nil {
		utils.Fail(c, "参数错误")
		return
	}
	models, err := service.FetchAiProviderModels(id)
	if err != nil {
		utils.Fail(c, err.Error())
		return
	}
	utils.OK(c, gin.H{"models": models})
}

func CreateAiProvider(c *gin.Context) {
	var req aiProviderReq
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.Fail(c, "参数错误")
		return
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	if err := service.CreateAiProvider(req.Name, req.BaseURL, req.APIKey, req.APIMode, req.Model, req.Temperature, enabled, req.Remark); err != nil {
		utils.Fail(c, err.Error())
		return
	}
	utils.OK(c, nil)
}

func UpdateAiProvider(c *gin.Context) {
	id, err := utils.ParseID(c)
	if err != nil {
		utils.Fail(c, "参数错误")
		return
	}
	var req aiProviderReq
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.Fail(c, "参数错误")
		return
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	if err := service.UpdateAiProvider(id, req.Name, req.BaseURL, req.APIKey, req.APIMode, req.Model, req.Temperature, enabled, req.Remark); err != nil {
		utils.Fail(c, err.Error())
		return
	}
	utils.OK(c, nil)
}

func DeleteAiProvider(c *gin.Context) {
	id, err := utils.ParseID(c)
	if err != nil {
		utils.Fail(c, "参数错误")
		return
	}
	if err := service.DeleteAiProvider(id); err != nil {
		utils.Fail(c, err.Error())
		return
	}
	utils.OK(c, nil)
}

// ---------- 智能体 ----------

type aiAgentReq struct {
	Name         string  `json:"name" binding:"required"`
	ProviderID   uint    `json:"providerId" binding:"required"`
	SystemPrompt string  `json:"systemPrompt"`
	Temperature  float64 `json:"temperature"`
	Enabled      *bool   `json:"enabled"`
	Remark       string  `json:"remark"`
	SkillIDs     []uint  `json:"skillIds"`
	ToolIDs      []uint  `json:"toolIds"`
	McpIDs       []uint  `json:"mcpIds"`
}

func ListAiAgents(c *gin.Context) {
	list, err := service.ListAiAgents(c.Query("keyword"))
	if err != nil {
		utils.Fail(c, err.Error())
		return
	}
	utils.OK(c, gin.H{"list": list, "total": len(list)})
}

func CreateAiAgent(c *gin.Context) {
	var req aiAgentReq
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.Fail(c, "参数错误")
		return
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	if err := service.CreateAiAgent(req.Name, req.SystemPrompt, req.ProviderID, req.Temperature, enabled, req.Remark, req.SkillIDs, req.ToolIDs, req.McpIDs); err != nil {
		utils.Fail(c, err.Error())
		return
	}
	utils.OK(c, nil)
}

func UpdateAiAgent(c *gin.Context) {
	id, err := utils.ParseID(c)
	if err != nil {
		utils.Fail(c, "参数错误")
		return
	}
	var req aiAgentReq
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.Fail(c, "参数错误")
		return
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	if err := service.UpdateAiAgent(id, req.Name, req.SystemPrompt, req.ProviderID, req.Temperature, enabled, req.Remark, req.SkillIDs, req.ToolIDs, req.McpIDs); err != nil {
		utils.Fail(c, err.Error())
		return
	}
	utils.OK(c, nil)
}

func DeleteAiAgent(c *gin.Context) {
	id, err := utils.ParseID(c)
	if err != nil {
		utils.Fail(c, "参数错误")
		return
	}
	if err := service.DeleteAiAgent(id); err != nil {
		utils.Fail(c, err.Error())
		return
	}
	utils.OK(c, nil)
}

// ---------- 技能 ----------

type aiSkillReq struct {
	Name        string `json:"name" binding:"required"`
	Description string `json:"description"`
	Prompt      string `json:"prompt"`
	Remark      string `json:"remark"`
}

func ListAiSkills(c *gin.Context) {
	list, err := service.ListAiSkills(c.Query("keyword"))
	if err != nil {
		utils.Fail(c, err.Error())
		return
	}
	utils.OK(c, gin.H{"list": list, "total": len(list)})
}

func SaveAiSkill(c *gin.Context) {
	id, _ := utils.ParseID(c)
	var req aiSkillReq
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.Fail(c, "参数错误")
		return
	}
	if err := service.SaveAiSkill(id, req.Name, req.Description, req.Prompt, req.Remark); err != nil {
		utils.Fail(c, err.Error())
		return
	}
	utils.OK(c, nil)
}

func DeleteAiSkill(c *gin.Context) {
	id, err := utils.ParseID(c)
	if err != nil {
		utils.Fail(c, "参数错误")
		return
	}
	if err := service.DeleteAiSkill(id); err != nil {
		utils.Fail(c, err.Error())
		return
	}
	utils.OK(c, nil)
}

// ---------- 工具 ----------

type aiToolReq struct {
	Name        string `json:"name" binding:"required"`
	Description string `json:"description"`
	Schema      string `json:"schema"`
	Enabled     *bool  `json:"enabled"`
	Remark      string `json:"remark"`
}

func ListAiTools(c *gin.Context) {
	list, err := service.ListAiTools(c.Query("keyword"))
	if err != nil {
		utils.Fail(c, err.Error())
		return
	}
	utils.OK(c, gin.H{"list": list, "total": len(list)})
}

func SaveAiTool(c *gin.Context) {
	id, _ := utils.ParseID(c)
	var req aiToolReq
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.Fail(c, "参数错误")
		return
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	if err := service.SaveAiTool(id, req.Name, req.Description, req.Schema, enabled, req.Remark); err != nil {
		utils.Fail(c, err.Error())
		return
	}
	utils.OK(c, nil)
}

func DeleteAiTool(c *gin.Context) {
	id, err := utils.ParseID(c)
	if err != nil {
		utils.Fail(c, "参数错误")
		return
	}
	if err := service.DeleteAiTool(id); err != nil {
		utils.Fail(c, err.Error())
		return
	}
	utils.OK(c, nil)
}

// ---------- MCP 服务 ----------

type aiMcpReq struct {
	Name      string `json:"name" binding:"required"`
	Transport string `json:"transport"`
	URL       string `json:"url"`
	Command   string `json:"command"`
	Args      string `json:"args"`
	Headers   string `json:"headers"`
	Enabled   *bool  `json:"enabled"`
	Remark    string `json:"remark"`
}

func ListAiMcpServers(c *gin.Context) {
	list, err := service.ListAiMcpServers(c.Query("keyword"))
	if err != nil {
		utils.Fail(c, err.Error())
		return
	}
	utils.OK(c, gin.H{"list": list, "total": len(list)})
}

func SaveAiMcpServer(c *gin.Context) {
	id, _ := utils.ParseID(c)
	var req aiMcpReq
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.Fail(c, "参数错误")
		return
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	if err := service.SaveAiMcpServer(id, req.Name, req.Transport, req.URL, req.Command, req.Args, req.Headers, enabled, req.Remark); err != nil {
		utils.Fail(c, err.Error())
		return
	}
	utils.OK(c, nil)
}

func DeleteAiMcpServer(c *gin.Context) {
	id, err := utils.ParseID(c)
	if err != nil {
		utils.Fail(c, "参数错误")
		return
	}
	if err := service.DeleteAiMcpServer(id); err != nil {
		utils.Fail(c, err.Error())
		return
	}
	utils.OK(c, nil)
}

// ---------- 会话 ----------

type aiSessionReq struct {
	Title   string `json:"title"`
	AgentID uint   `json:"agentId" binding:"required"`
}

type aiChatReq struct {
	Content string `json:"content" binding:"required"`
}

func ListAiSessions(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("pageSize", "20"))
	claims := utils.GetClaims(c)
	if claims == nil {
		utils.NoAuth(c, "未登录")
		return
	}
	list, total, err := service.ListAiSessionsForUser(claims.UserID, c.Query("keyword"), page, pageSize)
	if err != nil {
		utils.Fail(c, err.Error())
		return
	}
	utils.OK(c, gin.H{"list": list, "total": total})
}

func CreateAiSession(c *gin.Context) {
	var req aiSessionReq
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.Fail(c, "参数错误")
		return
	}
	claims := utils.GetClaims(c)
	if claims == nil {
		utils.NoAuth(c, "未登录")
		return
	}
	scope, ok := requestEngineeringIdentity(c)
	if !ok {
		return
	}
	if scope.ProjectID != model.DefaultProjectID {
		if _, err := service.EngineeringProject(scope); err != nil {
			utils.Forbidden(c, err.Error())
			return
		}
	}
	session, err := service.CreateAiSessionInProject(req.Title, req.AgentID, claims.UserID, scope.ProjectID)
	if err != nil {
		utils.Fail(c, err.Error())
		return
	}
	utils.OK(c, session)
}

func DeleteAiSession(c *gin.Context) {
	id, err := utils.ParseID(c)
	if err != nil {
		utils.Fail(c, "参数错误")
		return
	}
	if _, ok := ownedAiSessionContext(c, id); !ok {
		return
	}
	if err := service.DeleteAiSession(id); err != nil {
		utils.Fail(c, err.Error())
		return
	}
	utils.OK(c, nil)
}

func ListAiMessages(c *gin.Context) {
	id, err := utils.ParseID(c)
	if err != nil {
		utils.Fail(c, "参数错误")
		return
	}
	if _, ok := ownedAiSessionContext(c, id); !ok {
		return
	}
	list, err := service.ListAiMessages(id)
	if err != nil {
		utils.Fail(c, err.Error())
		return
	}
	utils.OK(c, gin.H{"list": list})
}

func ChatAi(c *gin.Context) {
	id, err := utils.ParseID(c)
	if err != nil {
		utils.Fail(c, "参数错误")
		return
	}
	ctx, ok := ownedAiSessionContext(c, id)
	if !ok {
		return
	}
	var req aiChatReq
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.Fail(c, "请输入内容")
		return
	}
	msg, err := service.ChatAiWithContext(ctx, id, req.Content)
	if err != nil {
		utils.Fail(c, err.Error())
		return
	}
	utils.OK(c, msg)
}

type aiRenameReq struct {
	Title string `json:"title" binding:"required"`
}

func RenameAiSession(c *gin.Context) {
	id, err := utils.ParseID(c)
	if err != nil {
		utils.Fail(c, "参数错误")
		return
	}
	if _, ok := ownedAiSessionContext(c, id); !ok {
		return
	}
	var req aiRenameReq
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.Fail(c, "标题不能为空")
		return
	}
	if err := service.RenameAiSession(id, req.Title); err != nil {
		utils.Fail(c, err.Error())
		return
	}
	utils.OK(c, nil)
}

func ClearAiSession(c *gin.Context) {
	id, err := utils.ParseID(c)
	if err != nil {
		utils.Fail(c, "参数错误")
		return
	}
	if _, ok := ownedAiSessionContext(c, id); !ok {
		return
	}
	if err := service.ClearAiSession(id); err != nil {
		utils.Fail(c, err.Error())
		return
	}
	utils.OK(c, nil)
}

func RegenerateAiSession(c *gin.Context) {
	id, err := utils.ParseID(c)
	if err != nil {
		utils.Fail(c, "参数错误")
		return
	}
	ctx, ok := ownedAiSessionContext(c, id)
	if !ok {
		return
	}
	msg, err := service.RegenerateAiSessionWithContext(ctx, id)
	if err != nil {
		utils.Fail(c, err.Error())
		return
	}
	utils.OK(c, msg)
}

func ResendAiMessage(c *gin.Context) {
	id, err := utils.ParseID(c)
	if err != nil {
		utils.Fail(c, "参数错误")
		return
	}
	ctx, ok := ownedAiSessionContext(c, id)
	if !ok {
		return
	}
	mid64, err := strconv.ParseUint(c.Param("mid"), 10, 64)
	if err != nil {
		utils.Fail(c, "参数错误")
		return
	}
	mid := uint(mid64)
	var req aiChatReq
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.Fail(c, "请输入内容")
		return
	}
	msg, err := service.ResendAiMessageWithContext(ctx, id, mid, req.Content)
	if err != nil {
		utils.Fail(c, err.Error())
		return
	}
	utils.OK(c, msg)
}

func ListAiUsages(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("pageSize", "20"))
	agentID, _ := strconv.ParseUint(c.Query("agentId"), 10, 64)
	providerID, _ := strconv.ParseUint(c.Query("providerId"), 10, 64)
	list, total, err := service.ListAiUsages(page, pageSize, uint(agentID), uint(providerID))
	if err != nil {
		utils.Fail(c, err.Error())
		return
	}
	utils.OK(c, gin.H{"list": list, "total": total})
}

func AiUsageSummary(c *gin.Context) {
	s, err := service.GetAiUsageSummary()
	if err != nil {
		utils.Fail(c, err.Error())
		return
	}
	utils.OK(c, s)
}

// StreamChatAi SSE 流式对话
func StreamChatAi(c *gin.Context) {
	id, err := utils.ParseID(c)
	if err != nil {
		utils.Fail(c, "参数错误")
		return
	}
	ctx, ok := ownedAiSessionContext(c, id)
	if !ok {
		return
	}
	var req aiChatReq
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.Fail(c, "请输入内容")
		return
	}

	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.Header().Set("X-Accel-Buffering", "no")
	flusher, streamOK := c.Writer.(http.Flusher)
	if !streamOK {
		utils.Fail(c, "当前服务器不支持流式输出")
		return
	}

	full, err := service.StreamChatAiTo(ctx, id, req.Content, func(delta string) {
		b, _ := json.Marshal(gin.H{"delta": delta})
		fmt.Fprintf(c.Writer, "data: %s\n\n", b)
		flusher.Flush()
	})
	if err != nil {
		b, _ := json.Marshal(gin.H{"error": err.Error()})
		fmt.Fprintf(c.Writer, "data: %s\n\n", b)
		flusher.Flush()
		return
	}
	_ = service.SaveAiAssistantMessage(id, full)
	fmt.Fprint(c.Writer, "data: [DONE]\n\n")
	flusher.Flush()
}
