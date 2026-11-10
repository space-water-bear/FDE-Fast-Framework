package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"go.uber.org/zap"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"

	"fde-admin/internal/global"
	"fde-admin/internal/model"
)

const AiBotFailureReply = "本次处理未完成，请稍后重新发送消息。"

type AiBotInput struct {
	Name         string `json:"name"`
	Platform     string `json:"platform"`
	AgentID      uint   `json:"agentId"`
	Enabled      bool   `json:"enabled"`
	Remark       string `json:"remark"`
	BotID        string `json:"botId"`
	Secret       string `json:"secret"`
	ClientID     string `json:"clientId"`
	ClientSecret string `json:"clientSecret"`
}

func ListAiBots(keyword string) ([]model.AiBot, error) {
	list := []model.AiBot{}
	q := global.DB.Model(&model.AiBot{})
	if keyword != "" {
		q = q.Where("name LIKE ? OR remark LIKE ?", "%"+keyword+"%", "%"+keyword+"%")
	}
	err := q.Order("id asc").Find(&list).Error
	return list, err
}

func SaveAiBot(id uint, in AiBotInput) (*model.AiBot, error) {
	b := model.AiBot{}
	if id != 0 {
		if err := global.DB.First(&b, id).Error; err != nil {
			return nil, errors.New("机器人不存在")
		}
		if b.Platform != in.Platform {
			return nil, errors.New("创建后不能更换平台，请新增机器人")
		}
	}
	b.Name, b.Platform, b.AgentID = strings.TrimSpace(in.Name), in.Platform, in.AgentID
	b.Enabled, b.Remark = in.Enabled, strings.TrimSpace(in.Remark)
	if b.Name == "" || utf8.RuneCountInString(b.Name) > 128 || utf8.RuneCountInString(b.Remark) > 255 {
		return nil, errors.New("请输入有效的名称和备注")
	}
	if b.Platform != "wecom" && b.Platform != "dingtalk" {
		return nil, errors.New("当前支持企业微信和钉钉，飞书将在后续接入")
	}
	var ag model.AiAgent
	if err := global.DB.First(&ag, b.AgentID).Error; err != nil {
		return nil, errors.New("请选择有效的智能体")
	}
	if b.Enabled && !ag.Enabled {
		return nil, errors.New("绑定的智能体已禁用")
	}
	if b.Platform == "wecom" {
		botID, secret := strings.TrimSpace(in.BotID), strings.TrimSpace(in.Secret)
		if b.BotID != botID && secret == "" {
			return nil, errors.New("更换 Bot ID 时请重新填写 Secret")
		}
		b.BotID = botID
		if secret != "" {
			b.Secret = secret
		}
		if b.BotID == "" || len(b.BotID) > 128 || b.Secret == "" || len(b.Secret) > 255 {
			return nil, errors.New("请填写有效的 Bot ID 和 Secret")
		}
		var duplicates int64
		if err := global.DB.Model(&model.AiBot{}).Where("platform = ? AND bot_id = ? AND id <> ?", "wecom", b.BotID, b.ID).Count(&duplicates).Error; err != nil {
			return nil, err
		}
		if duplicates > 0 {
			return nil, errors.New("该 Bot ID 已配置，请编辑现有机器人，避免重复连接")
		}
		b.ClientID, b.ClientSecret = "", ""
	} else {
		clientID, secret := strings.TrimSpace(in.ClientID), strings.TrimSpace(in.ClientSecret)
		if b.ClientID != clientID && secret == "" {
			return nil, errors.New("更换 Client ID 时请重新填写 Client Secret")
		}
		b.ClientID = clientID
		if secret != "" {
			b.ClientSecret = secret
		}
		if b.ClientID == "" || len(b.ClientID) > 128 || b.ClientSecret == "" || len(b.ClientSecret) > 255 {
			return nil, errors.New("请填写有效的 Client ID 和 Client Secret")
		}
		var duplicates int64
		if err := global.DB.Model(&model.AiBot{}).Where("platform = ? AND client_id = ? AND id <> ?", "dingtalk", b.ClientID, b.ID).Count(&duplicates).Error; err != nil {
			return nil, err
		}
		if duplicates > 0 {
			return nil, errors.New("该 Client ID 已配置，请编辑现有机器人")
		}
		b.BotID, b.Secret = "", ""
	}
	// GORM's error logger otherwise renders credential values in failed SQL.
	if err := global.DB.Session(&gorm.Session{Logger: global.DB.Logger.LogMode(logger.Silent)}).Save(&b).Error; err != nil {
		return nil, errors.New("保存机器人失败")
	}
	notifyWeComBots()
	notifyDingTalkBots()
	return &b, nil
}

func GetAiBot(id uint, requireEnabled bool) (*model.AiBot, error) {
	var b model.AiBot
	if err := global.DB.First(&b, id).Error; err != nil {
		return nil, errors.New("机器人不可用")
	}
	if requireEnabled && !b.Enabled {
		return nil, errors.New("机器人不可用")
	}
	return &b, nil
}

func DeleteAiBot(id uint) error {
	err := global.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("bot_id = ?", id).Delete(&model.AiBotConversation{}).Error; err != nil {
			return err
		}
		if err := tx.Where("bot_id = ?", id).Delete(&model.AiBotDelivery{}).Error; err != nil {
			return err
		}
		return tx.Delete(&model.AiBot{}, id).Error
	})
	if err == nil {
		notifyWeComBots()
		notifyDingTalkBots()
	}
	return err
}

type AiBotIncoming struct {
	MessageID, ConversationID, SenderID, Text string
}

func botHash(parts ...string) string {
	data, _ := json.Marshal(parts)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func botConversationKey(b model.AiBot, in AiBotIncoming) string {
	parts := []string{fmt.Sprint(b.AgentID), in.ConversationID, in.SenderID}
	if b.Platform == "wecom" {
		parts = append([]string{"wecom", b.BotID}, parts...)
	} else if b.Platform == "dingtalk" && b.ClientID != "" {
		parts = append([]string{"dingtalk", b.ClientID}, parts...)
	}
	return botHash(parts...)
}

func botMessageKey(b model.AiBot, messageID string) string {
	if b.Platform == "wecom" {
		return botHash("wecom", b.BotID, messageID)
	}
	if b.Platform == "dingtalk" && b.ClientID != "" {
		return botHash("dingtalk", b.ClientID, messageID)
	}
	return botHash(messageID)
}

// ClaimAiBotMessage persists a receipt before any AI/tool execution. Failed and
// interrupted receipts are intentionally not retried automatically.
func ClaimAiBotMessage(b *model.AiBot, in AiBotIncoming) (*model.AiBotDelivery, bool, error) {
	if in.MessageID == "" || in.ConversationID == "" || in.SenderID == "" || strings.TrimSpace(in.Text) == "" {
		return nil, false, errors.New("消息缺少必要字段")
	}
	if len(in.Text) > 16000 || len(in.MessageID) > 512 || len(in.SenderID) > 512 || len(in.ConversationID) > 512 {
		return nil, false, errors.New("消息过长")
	}
	key := botMessageKey(*b, in.MessageID)
	var existing model.AiBotDelivery
	if err := global.DB.Where("bot_id = ? AND message_key = ?", b.ID, key).First(&existing).Error; err == nil {
		return refreshBotDelivery(&existing), false, nil
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, false, err
	}
	random := make([]byte, 24)
	if _, err := rand.Read(random); err != nil {
		return nil, false, err
	}
	d := model.AiBotDelivery{BotID: b.ID, MessageKey: key, ConversationKey: botConversationKey(*b, in), StreamID: hex.EncodeToString(random), Status: "processing"}
	if b.Platform == "wecom" {
		deadline := time.Now().Add(9 * time.Minute)
		d.ExpiresAt = &deadline
	}
	result := global.DB.Clauses(clause.OnConflict{DoNothing: true}).Create(&d)
	if result.Error != nil {
		return nil, false, result.Error
	}
	if result.RowsAffected == 0 {
		if err := global.DB.Where("bot_id = ? AND message_key = ?", b.ID, key).First(&existing).Error; err != nil {
			return nil, false, err
		}
		return refreshBotDelivery(&existing), false, nil
	}
	return &d, true, nil
}

func refreshBotDelivery(d *model.AiBotDelivery) *model.AiBotDelivery {
	expired := time.Since(d.CreatedAt) > 2*time.Minute
	if d.ExpiresAt != nil {
		expired = time.Now().After(*d.ExpiresAt)
	}
	if d.Status == "processing" && expired {
		_ = global.DB.Model(d).Where("status = ?", "processing").Updates(map[string]any{"status": "interrupted", "reply": AiBotFailureReply}).Error
		d.Status, d.Reply = "interrupted", AiBotFailureReply
	}
	return d
}

func GetAiBotStream(botID uint, streamID string) (*model.AiBotDelivery, error) {
	var d model.AiBotDelivery
	if streamID == "" || len(streamID) > 64 {
		return nil, errors.New("无效的回复标识")
	}
	if err := global.DB.Where("bot_id = ? AND stream_id = ?", botID, streamID).First(&d).Error; err != nil {
		return nil, err
	}
	return refreshBotDelivery(&d), nil
}

// A small bounded queue and striped locks bound resource use and serialize each
// conversation without retaining an unbounded in-memory lock map.
var botQueue = make(chan struct{}, 32)
var botWorkers = make(chan struct{}, 8)
var botConversationLocks = newBotConversationLocks()

func newBotConversationLocks() [64]chan struct{} {
	var locks [64]chan struct{}
	for i := range locks {
		locks[i] = make(chan struct{}, 1)
	}
	return locks
}

func botConversationLock(key string) chan struct{} {
	return botConversationLocks[(int(key[0])<<4^int(key[1]))%len(botConversationLocks)]
}

// StartAiBotMessage returns immediately; platform delivery runs asynchronously.
func StartAiBotMessage(b model.AiBot, d model.AiBotDelivery, in AiBotIncoming, send func(string) error) bool {
	return startAiBotMessage(context.Background(), b, d, in, send, nil, nil)
}

func startAiBotMessage(parent context.Context, b model.AiBot, d model.AiBotDelivery, in AiBotIncoming, send func(string) error, progress func(string), onDone func()) bool {
	select {
	case botQueue <- struct{}{}:
	default:
		// Both Stream channels retain their receipt for one final busy reply;
		// callback ACKs are not a reliable redelivery queue for chatbot messages.
		return false
	}
	go func() {
		if onDone != nil {
			defer onDone()
		}
		complete := func(status, reply string) {
			if send != nil {
				if err := send(reply); err != nil {
					status = "reply_failed"
				}
			}
			finishBotDelivery(d.ID, status, reply)
		}
		defer func() {
			<-botQueue
			if recover() != nil {
				complete("failed", AiBotFailureReply)
			}
		}()
		ctx, cancel := context.WithTimeout(parent, 90*time.Second)
		defer cancel()
		// Wait before taking a worker slot so later turns cannot overtake an
		// earlier accepted message, including when callbacks hit two processes.
		if err := waitBotTurn(ctx, d); err != nil {
			complete("failed", AiBotFailureReply)
			return
		}
		select {
		case botWorkers <- struct{}{}:
			defer func() { <-botWorkers }()
		case <-ctx.Done():
			complete("failed", AiBotFailureReply)
			return
		}
		key := botConversationKey(b, in)
		lock := botConversationLock(key)
		select {
		case lock <- struct{}{}:
			defer func() { <-lock }()
		case <-ctx.Done():
			complete("failed", AiBotFailureReply)
			return
		}
		if ctx.Err() != nil {
			complete("failed", AiBotFailureReply)
			return
		}
		current, err := GetAiBot(b.ID, true)
		if err != nil || current.AgentID != b.AgentID {
			complete("failed", AiBotFailureReply)
			return
		}
		sessionID, err := botSession(b, key)
		var reply string
		if err == nil {
			if progress == nil {
				progress = func(string) {}
			}
			reply, err = streamChatAiTo(ctx, sessionID, in.Text, progress, false)
			if err == nil {
				err = ctx.Err()
			}
			if err == nil && strings.TrimSpace(reply) == "" {
				err = errors.New("empty reply")
			}
			if err == nil {
				err = SaveAiAssistantMessage(sessionID, reply)
			}
		}
		status := "completed"
		if err != nil {
			status, reply = "failed", AiBotFailureReply
		}
		complete(status, reply)
	}()
	return true
}

func waitBotTurn(ctx context.Context, d model.AiBotDelivery) error {
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		q := global.DB.Model(&model.AiBotDelivery{}).Where("bot_id = ? AND conversation_key = ? AND id < ? AND status = ?", d.BotID, d.ConversationKey, d.ID, "processing")
		if err := q.Where("((expires_at IS NOT NULL AND expires_at < ?) OR (expires_at IS NULL AND created_at < ?))", time.Now(), time.Now().Add(-2*time.Minute)).Updates(map[string]any{"status": "interrupted", "reply": AiBotFailureReply}).Error; err != nil {
			return err
		}
		var earlier int64
		if err := global.DB.Model(&model.AiBotDelivery{}).Where("bot_id = ? AND conversation_key = ? AND id < ? AND status = ?", d.BotID, d.ConversationKey, d.ID, "processing").Count(&earlier).Error; err != nil {
			return err
		}
		if earlier == 0 {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func finishBotDelivery(id uint, status, reply string) {
	_ = global.DB.Model(&model.AiBotDelivery{}).Where("id = ? AND status = ?", id, "processing").Updates(map[string]any{"status": status, "reply": reply}).Error
	if global.Logger != nil {
		global.Logger.Info("ai_bot_delivery", zap.Uint("deliveryId", id), zap.String("status", status))
	}
}

func botSession(b model.AiBot, key string) (uint, error) {
	var binding model.AiBotConversation
	err := global.DB.Where("bot_id = ? AND conversation_key = ?", b.ID, key).First(&binding).Error
	if err == nil {
		var session model.AiSession
		if e := global.DB.First(&session, binding.SessionID).Error; e == nil {
			return session.ID, nil
		} else if !errors.Is(e, gorm.ErrRecordNotFound) {
			return 0, e
		}
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, err
	}
	var session model.AiSession
	err = global.DB.Transaction(func(tx *gorm.DB) error {
		session = model.AiSession{Title: b.Name + " · 机器人会话", AgentID: b.AgentID, UserID: 0}
		if err := tx.Create(&session).Error; err != nil {
			return err
		}
		if binding.ID != 0 {
			return tx.Model(&binding).Update("session_id", session.ID).Error
		}
		return tx.Create(&model.AiBotConversation{BotID: b.ID, ConversationKey: key, SessionID: session.ID}).Error
	})
	return session.ID, err
}
