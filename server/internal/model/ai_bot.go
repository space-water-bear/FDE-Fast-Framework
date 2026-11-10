package model

import "time"

// AiBot binds an external conversational robot to an existing AI agent.
// Credentials are write-only in the management API.
type AiBot struct {
	Base
	Name         string `json:"name" gorm:"size:128"`
	Platform     string `json:"platform" gorm:"size:16;index"`
	AgentID      uint   `json:"agentId" gorm:"index"`
	Enabled      bool   `json:"enabled"`
	Remark       string `json:"remark" gorm:"size:255"`
	BotID        string `json:"botId" gorm:"size:128;index"`
	Secret       string `json:"-" gorm:"size:255"`
	ClientID     string `json:"clientId" gorm:"size:128;index"`
	ClientSecret string `json:"-" gorm:"size:255"`
}

func (AiBot) TableName() string { return "sys_ai_bots" }

// AiBotConversation isolates context by robot, agent, conversation and sender.
type AiBotConversation struct {
	Base
	BotID           uint   `gorm:"uniqueIndex:idx_bot_conversation"`
	ConversationKey string `gorm:"size:64;uniqueIndex:idx_bot_conversation"`
	SessionID       uint
}

func (AiBotConversation) TableName() string { return "sys_ai_bot_conversations" }

// AiBotDelivery is a durable receipt: platform retries must never execute tools twice.
// No temporary reply webhook or raw incoming credentials are stored here.
type AiBotDelivery struct {
	Base
	BotID           uint   `gorm:"uniqueIndex:idx_bot_message;index:idx_bot_stream"`
	MessageKey      string `gorm:"size:64;uniqueIndex:idx_bot_message"`
	ConversationKey string `gorm:"size:64;index"`
	StreamID        string `gorm:"size:64;index:idx_bot_stream"`
	Status          string `gorm:"size:24"`
	Reply           string `gorm:"type:text"`
	ExpiresAt       *time.Time
}

func (AiBotDelivery) TableName() string { return "sys_ai_bot_deliveries" }

// AiBotSendAttempt preserves the per-conversation WebSocket send budget across
// credential updates and process restarts. It contains no message or credential.
type AiBotSendAttempt struct {
	ID         uint      `gorm:"primarykey"`
	ChannelKey string    `gorm:"size:64;index:idx_ai_bot_send_window,priority:1"`
	SentAt     time.Time `gorm:"index:idx_ai_bot_send_window,priority:2;index"`
}

func (AiBotSendAttempt) TableName() string { return "sys_ai_bot_send_attempts" }
