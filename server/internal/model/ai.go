package model

// AiProvider AI 供应商(OpenAI 兼容接口)
type AiProvider struct {
	Base
	Name        string  `json:"name" gorm:"size:128"`
	BaseURL     string  `json:"baseUrl" gorm:"size:255;comment:OpenAI兼容接口地址, 如 https://api.deepseek.com/v1"`
	APIKey      string  `json:"apiKey" gorm:"size:255"`
	APIMode     string  `json:"apiMode" gorm:"size:16;default:responses;comment:responses|chat"`
	Model       string  `json:"model" gorm:"size:128"`
	Models      string  `json:"-" gorm:"type:text;comment:模型列表(JSON数组)"`
	Temperature float64 `json:"temperature" gorm:"default:0.7"`
	Enabled     bool    `json:"enabled" gorm:"default:true"`
	Remark      string  `json:"remark" gorm:"size:255"`
}

func (AiProvider) TableName() string { return "sys_ai_providers" }

// AiAgent 智能体
type AiAgent struct {
	Base
	Name         string        `json:"name" gorm:"size:128"`
	ProviderID   uint          `json:"providerId" gorm:"index"`
	SystemPrompt string        `json:"systemPrompt" gorm:"type:text"`
	Temperature  float64       `json:"temperature" gorm:"default:0.7"`
	Enabled      bool          `json:"enabled" gorm:"default:true"`
	Remark       string        `json:"remark" gorm:"size:255"`
	Skills       []AiSkill     `json:"skills" gorm:"many2many:sys_ai_agent_skills;"`
	Tools        []AiTool      `json:"tools" gorm:"many2many:sys_ai_agent_tools;"`
	McpServers   []AiMcpServer `json:"mcpServers" gorm:"many2many:sys_ai_agent_mcp;"`
}

func (AiAgent) TableName() string { return "sys_ai_agents" }

// AiSkill 技能(提示词片段/能力说明)
type AiSkill struct {
	Base
	Name        string `json:"name" gorm:"size:128"`
	Description string `json:"description" gorm:"size:255"`
	Prompt      string `json:"prompt" gorm:"type:text"`
	Remark      string `json:"remark" gorm:"size:255"`
}

func (AiSkill) TableName() string { return "sys_ai_skills" }

// AiTool 工具(JSON Schema 定义, 供智能体后续挂载)
type AiTool struct {
	Base
	Name        string `json:"name" gorm:"size:128"`
	Description string `json:"description" gorm:"size:255"`
	Schema      string `json:"schema" gorm:"type:text;comment:JSON Schema"`
	Enabled     bool   `json:"enabled" gorm:"default:true"`
	Remark      string `json:"remark" gorm:"size:255"`
}

func (AiTool) TableName() string { return "sys_ai_tools" }

// AiMcpServer MCP 服务
type AiMcpServer struct {
	Base
	Name      string `json:"name" gorm:"size:128"`
	Transport string `json:"transport" gorm:"size:32;comment:stdio|sse|streamable"`
	URL       string `json:"url" gorm:"size:255"`
	Command   string `json:"command" gorm:"size:255"`
	Args      string `json:"args" gorm:"size:255"`
	Headers   string `json:"headers" gorm:"size:512"`
	Enabled   bool   `json:"enabled" gorm:"default:true"`
	Remark    string `json:"remark" gorm:"size:255"`
}

func (AiMcpServer) TableName() string { return "sys_ai_mcp_servers" }

// AiSession 会话
type AiSession struct {
	Base
	ProjectID uint   `json:"projectId" gorm:"index;default:1"`
	Title     string `json:"title" gorm:"size:128"`
	AgentID   uint   `json:"agentId" gorm:"index"`
	UserID    uint   `json:"userId" gorm:"index"`
}

func (AiSession) TableName() string { return "sys_ai_sessions" }

// AiMessage 会话消息
type AiMessage struct {
	Base
	SessionID uint   `json:"sessionId" gorm:"index"`
	Role      string `json:"role" gorm:"size:16;comment:user|assistant"`
	Content   string `json:"content" gorm:"type:text"`
}

func (AiMessage) TableName() string { return "sys_ai_messages" }

// AiUsage AI 调用用量统计
type AiUsage struct {
	Base
	SessionID        uint   `json:"sessionId" gorm:"index"`
	AgentID          uint   `json:"agentId" gorm:"index"`
	ProviderID       uint   `json:"providerId" gorm:"index"`
	Model            string `json:"model" gorm:"size:128"`
	PromptTokens     int64  `json:"promptTokens"`
	CompletionTokens int64  `json:"completionTokens"`
	TotalTokens      int64  `json:"totalTokens"`
	LatencyMs        int64  `json:"latencyMs"`
}

func (AiUsage) TableName() string { return "sys_ai_usages" }
