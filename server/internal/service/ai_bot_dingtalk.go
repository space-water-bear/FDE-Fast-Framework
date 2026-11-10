package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"fde-admin/internal/botprotocol"
	"fde-admin/internal/dingtalkstream"
	"fde-admin/internal/global"
	"fde-admin/internal/model"
	"gorm.io/gorm"
)

type dingTalkConnection interface{ Run(context.Context) error }
type dingTalkFactory func(dingtalkstream.Config, func(dingtalkstream.Frame) bool, func(dingtalkstream.Status)) dingTalkConnection

type dingTalkBotManager struct {
	ctx       context.Context
	cancel    context.CancelFunc
	db        *gorm.DB
	newClient dingTalkFactory
	reply     func(context.Context, string, string) error
	wake      chan struct{}
	done      chan struct{}
	mu        sync.RWMutex
	entries   map[uint]*dingTalkBotEntry
}

type dingTalkBotEntry struct {
	bot        model.AiBot
	ctx        context.Context
	cancel     context.CancelFunc
	client     dingTalkConnection
	clientDone chan struct{}
	mu         sync.Mutex
	closed     bool
	status     dingtalkstream.Status
	tasks      sync.WaitGroup
}

var dingTalkManagerState struct {
	sync.RWMutex
	manager *dingTalkBotManager
}

func StartDingTalkBots(ctx context.Context) func() {
	m := newDingTalkBotManager(ctx, global.DB, func(cfg dingtalkstream.Config, message func(dingtalkstream.Frame) bool, status func(dingtalkstream.Status)) dingTalkConnection {
		return dingtalkstream.NewClient(cfg, message, status)
	})
	dingTalkManagerState.Lock()
	dingTalkManagerState.manager = m
	dingTalkManagerState.Unlock()
	go m.run()
	var once sync.Once
	return func() {
		once.Do(func() {
			m.cancel()
			<-m.done
			dingTalkManagerState.Lock()
			if dingTalkManagerState.manager == m {
				dingTalkManagerState.manager = nil
			}
			dingTalkManagerState.Unlock()
		})
	}
}

func newDingTalkBotManager(ctx context.Context, db *gorm.DB, factory dingTalkFactory) *dingTalkBotManager {
	ctx, cancel := context.WithCancel(ctx)
	return &dingTalkBotManager{ctx: ctx, cancel: cancel, db: db, newClient: factory, reply: sendDingTalkReply, wake: make(chan struct{}, 1), done: make(chan struct{}), entries: map[uint]*dingTalkBotEntry{}}
}

func notifyDingTalkBots() {
	dingTalkManagerState.RLock()
	m := dingTalkManagerState.manager
	dingTalkManagerState.RUnlock()
	if m != nil {
		select {
		case m.wake <- struct{}{}:
		default:
		}
	}
}

func DingTalkBotConnectionStatus(b model.AiBot) AiBotConnection {
	if b.ClientID == "" || b.ClientSecret == "" {
		return AiBotConnection{State: "unconfigured"}
	}
	if !b.Enabled {
		return AiBotConnection{State: "disabled"}
	}
	dingTalkManagerState.RLock()
	m := dingTalkManagerState.manager
	dingTalkManagerState.RUnlock()
	if m == nil {
		return AiBotConnection{State: "disconnected"}
	}
	m.mu.RLock()
	e := m.entries[b.ID]
	m.mu.RUnlock()
	if e == nil || !sameDingTalkConfig(e.bot, b) {
		return AiBotConnection{State: "connecting"}
	}
	e.mu.Lock()
	s := e.status
	e.mu.Unlock()
	state := s.State
	if state == "stopped" {
		state = "disconnected"
	}
	result := AiBotConnection{State: state, ReconnectAttempt: s.ReconnectAttempt}
	if !s.LastConnectedAt.IsZero() {
		t := s.LastConnectedAt
		result.LastConnectedAt = &t
	}
	if !s.LastHeartbeatAt.IsZero() {
		t := s.LastHeartbeatAt
		result.LastHeartbeatAt = &t
	}
	return result
}

func sameDingTalkConfig(a, b model.AiBot) bool {
	return a.ClientID == b.ClientID && a.ClientSecret == b.ClientSecret && a.AgentID == b.AgentID && a.Name == b.Name && a.Enabled == b.Enabled && a.UpdatedAt.Equal(b.UpdatedAt)
}

func (m *dingTalkBotManager) run() {
	defer close(m.done)
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		if m.ctx.Err() != nil {
			break
		}
		m.reconcile()
		select {
		case <-m.ctx.Done():
		case <-m.wake:
		case <-ticker.C:
		}
	}
	m.mu.Lock()
	entries := m.entries
	m.entries = map[uint]*dingTalkBotEntry{}
	m.mu.Unlock()
	for _, e := range entries {
		stopDingTalkEntry(e)
	}
}

func (m *dingTalkBotManager) reconcile() {
	var bots []model.AiBot
	if err := m.db.WithContext(m.ctx).Where("platform = ?", "dingtalk").Order("id asc").Find(&bots).Error; err != nil {
		return
	}
	wanted := map[uint]model.AiBot{}
	seen := map[string]bool{}
	for _, b := range bots {
		if b.Enabled && b.ClientID != "" && b.ClientSecret != "" && !seen[b.ClientID] {
			wanted[b.ID] = b
			seen[b.ClientID] = true
		}
	}
	m.mu.Lock()
	var stopped []*dingTalkBotEntry
	for id, e := range m.entries {
		b, ok := wanted[id]
		if !ok || !sameDingTalkConfig(e.bot, b) {
			delete(m.entries, id)
			stopped = append(stopped, e)
		}
	}
	m.mu.Unlock()
	for _, e := range stopped {
		stopDingTalkEntry(e)
	}
	if m.ctx.Err() != nil {
		return
	}
	for id, b := range wanted {
		m.mu.RLock()
		old := m.entries[id]
		m.mu.RUnlock()
		if old != nil {
			continue
		}
		ctx, cancel := context.WithCancel(m.ctx)
		e := &dingTalkBotEntry{bot: b, ctx: ctx, cancel: cancel, clientDone: make(chan struct{}), status: dingtalkstream.Status{State: "connecting"}}
		e.client = m.newClient(dingtalkstream.Config{ClientID: b.ClientID, ClientSecret: b.ClientSecret}, func(frame dingtalkstream.Frame) bool { return m.accept(e, frame) }, func(status dingtalkstream.Status) {
			e.mu.Lock()
			terminal := e.status.State == "auth_failed"
			if !terminal {
				e.status = status
			}
			e.mu.Unlock()
			if status.State == "auth_failed" {
				e.cancel()
			}
		})
		m.mu.Lock()
		m.entries[id] = e
		m.mu.Unlock()
		go func() { defer close(e.clientDone); _ = e.client.Run(ctx) }()
	}
}

func stopDingTalkEntry(e *dingTalkBotEntry) {
	e.mu.Lock()
	e.closed = true
	e.cancel()
	e.mu.Unlock()
	<-e.clientDone
	e.tasks.Wait()
}

type dingTalkBotMessage struct {
	MessageID        string `json:"msgId"`
	ConversationID   string `json:"conversationId"`
	ConversationType string `json:"conversationType"`
	SenderID         string `json:"senderId"`
	MessageType      string `json:"msgtype"`
	Text             struct {
		Content string `json:"content"`
	} `json:"text"`
	SessionWebhook            string `json:"sessionWebhook"`
	SessionWebhookExpiredTime int64  `json:"sessionWebhookExpiredTime"`
}

// accept persists a receipt before the Stream callback is ACKed. Chatbot Stream
// delivery is fire-and-forget; an ACK failure is never treated as a retry queue.
func (m *dingTalkBotManager) accept(e *dingTalkBotEntry, frame dingtalkstream.Frame) bool {
	e.mu.Lock()
	if e.closed || e.ctx.Err() != nil {
		e.mu.Unlock()
		return false
	}
	e.tasks.Add(1)
	e.mu.Unlock()
	defer e.tasks.Done()
	if frame.Type != "CALLBACK" || frame.Headers["topic"] != "/v1.0/im/bot/messages/get" {
		return true
	}
	var msg dingTalkBotMessage
	if json.Unmarshal([]byte(frame.Data), &msg) != nil || msg.MessageType != "text" {
		return true
	}
	if (msg.ConversationType != "1" && msg.ConversationType != "2") || msg.ConversationID == "" || msg.MessageID == "" || msg.SenderID == "" || strings.TrimSpace(msg.Text.Content) == "" {
		return true
	}
	if len(msg.Text.Content) > 16000 || len(msg.MessageID) > 512 || len(msg.ConversationID) > 500 || len(msg.SenderID) > 512 {
		return true
	}
	if botprotocol.ValidateDingTalkWebhook(msg.SessionWebhook) != nil || msg.SessionWebhookExpiredTime <= time.Now().UnixMilli() {
		return true
	}
	current, err := GetAiBot(e.bot.ID, true)
	if err != nil || !sameDingTalkConfig(*current, e.bot) || e.ctx.Err() != nil {
		return false
	}
	in := AiBotIncoming{MessageID: msg.MessageID, ConversationID: msg.ConversationType + ":" + msg.ConversationID, SenderID: msg.SenderID, Text: strings.TrimSpace(msg.Text.Content)}
	d, fresh, err := ClaimAiBotMessage(&e.bot, in)
	if err != nil {
		return false
	}
	if !fresh {
		return true
	}
	send := func(reply string) error {
		deadline := time.UnixMilli(msg.SessionWebhookExpiredTime)
		if !time.Now().Before(deadline) {
			return errors.New("DingTalk reply expired")
		}
		ctx, cancel := context.WithDeadline(e.ctx, deadline)
		defer cancel()
		return m.reply(ctx, msg.SessionWebhook, reply)
	}
	e.tasks.Add(1)
	if !startAiBotMessage(e.ctx, e.bot, *d, in, send, nil, func() { e.tasks.Done() }) {
		e.tasks.Done()
		reply := "当前请求较多，请稍后重新发送消息。"
		status := "failed"
		if err := send(reply); err != nil {
			status = "reply_failed"
		}
		finishBotDelivery(d.ID, status, reply)
	}
	return true
}

var dingTalkReplyClient = &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

func sendDingTalkReply(ctx context.Context, webhook, text string) error {
	if err := botprotocol.ValidateDingTalkWebhook(webhook); err != nil {
		return err
	}
	body, _ := json.Marshal(map[string]any{"msgtype": "text", "text": map[string]string{"content": botprotocol.ClipUTF8(text, 16000)}})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, webhook, bytes.NewReader(body))
	if err != nil {
		return errors.New("invalid DingTalk reply destination")
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := dingTalkReplyClient.Do(req)
	if err != nil {
		return errors.New("DingTalk reply request failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return errors.New("DingTalk reply HTTP failure")
	}
	var result struct {
		ErrCode *int `json:"errcode"`
	}
	if json.NewDecoder(io.LimitReader(resp.Body, 65536)).Decode(&result) != nil || result.ErrCode == nil || *result.ErrCode != 0 {
		return errors.New("DingTalk reply rejected")
	}
	return nil
}
