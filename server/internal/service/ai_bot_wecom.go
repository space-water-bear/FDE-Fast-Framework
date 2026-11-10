package service

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"time"

	"fde-admin/internal/botprotocol"
	"fde-admin/internal/global"
	"fde-admin/internal/model"
	"fde-admin/internal/wecomws"

	"gorm.io/gorm"
)

type AiBotConnection struct {
	State            string     `json:"state"`
	ReconnectAttempt int        `json:"reconnectAttempt"`
	LastConnectedAt  *time.Time `json:"lastConnectedAt,omitempty"`
	LastHeartbeatAt  *time.Time `json:"lastHeartbeatAt,omitempty"`
}

type wecomConnection interface {
	Run(context.Context) error
	ReplyStream(context.Context, string, string, string, bool) error
}

type wecomFactory func(wecomws.Config, func(wecomws.Frame), func(wecomws.Status)) wecomConnection

type wecomBotManager struct {
	ctx       context.Context
	cancel    context.CancelFunc
	db        *gorm.DB
	newClient wecomFactory
	limiter   *wecomReplyLimiter
	wake      chan struct{}
	done      chan struct{}
	mu        sync.RWMutex
	entries   map[uint]*wecomBotEntry
}

type wecomBotEntry struct {
	bot        model.AiBot
	ctx        context.Context
	cancel     context.CancelFunc
	client     wecomConnection
	frames     chan wecomws.Frame
	loopDone   chan struct{}
	clientDone chan struct{}
	tasks      sync.WaitGroup
	mu         sync.RWMutex
	status     wecomws.Status
}

var wecomManagerState struct {
	sync.RWMutex
	manager *wecomBotManager
}

// StartWeComBots owns the long connections for this server process. Stopping it
// closes connections and waits for canceled message tasks before returning.
func StartWeComBots(ctx context.Context) func() {
	m := newWeComBotManager(ctx, global.DB, func(cfg wecomws.Config, message func(wecomws.Frame), status func(wecomws.Status)) wecomConnection {
		return wecomws.NewClient(cfg, message, status)
	})
	wecomManagerState.Lock()
	wecomManagerState.manager = m
	wecomManagerState.Unlock()
	go m.run()
	var once sync.Once
	return func() {
		once.Do(func() {
			m.cancel()
			<-m.done
			wecomManagerState.Lock()
			if wecomManagerState.manager == m {
				wecomManagerState.manager = nil
			}
			wecomManagerState.Unlock()
		})
	}
}

func newWeComBotManager(ctx context.Context, db *gorm.DB, factory wecomFactory) *wecomBotManager {
	ctx, cancel := context.WithCancel(ctx)
	return &wecomBotManager{ctx: ctx, cancel: cancel, db: db, newClient: factory, limiter: newWeComReplyLimiter(db), wake: make(chan struct{}, 1), done: make(chan struct{}), entries: map[uint]*wecomBotEntry{}}
}

func notifyWeComBots() {
	wecomManagerState.RLock()
	m := wecomManagerState.manager
	wecomManagerState.RUnlock()
	if m != nil {
		select {
		case m.wake <- struct{}{}:
		default:
		}
	}
}

func WeComBotConnectionStatus(b model.AiBot) AiBotConnection {
	if b.Platform != "wecom" {
		return AiBotConnection{State: "http"}
	}
	if b.BotID == "" || b.Secret == "" {
		return AiBotConnection{State: "unconfigured"}
	}
	if !b.Enabled {
		return AiBotConnection{State: "disabled"}
	}
	wecomManagerState.RLock()
	m := wecomManagerState.manager
	wecomManagerState.RUnlock()
	if m == nil {
		return AiBotConnection{State: "disconnected"}
	}
	m.mu.RLock()
	e := m.entries[b.ID]
	m.mu.RUnlock()
	if e == nil || !sameWeComConfig(e.bot, b) {
		return AiBotConnection{State: "connecting"}
	}
	e.mu.RLock()
	s := e.status
	e.mu.RUnlock()
	state := s.State
	if state == "superseded" {
		state = "conflict"
	}
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

func (m *wecomBotManager) run() {
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
	m.entries = map[uint]*wecomBotEntry{}
	m.mu.Unlock()
	for _, e := range entries {
		stopWeComEntry(e)
	}
}

func sameWeComConfig(a, b model.AiBot) bool {
	return a.BotID == b.BotID && a.Secret == b.Secret && a.AgentID == b.AgentID && a.Name == b.Name && a.Enabled == b.Enabled && a.UpdatedAt.Equal(b.UpdatedAt)
}

func (m *wecomBotManager) reconcile() {
	var bots []model.AiBot
	if err := m.db.WithContext(m.ctx).Where("platform = ?", "wecom").Order("id asc").Find(&bots).Error; err != nil {
		return
	}
	wanted := map[uint]model.AiBot{}
	seen := map[string]bool{}
	for _, b := range bots {
		if b.Enabled && b.BotID != "" && b.Secret != "" && !seen[b.BotID] {
			wanted[b.ID] = b
			seen[b.BotID] = true
		}
	}
	m.mu.Lock()
	var stopped []*wecomBotEntry
	for id, e := range m.entries {
		b, ok := wanted[id]
		if !ok || !sameWeComConfig(e.bot, b) {
			delete(m.entries, id)
			stopped = append(stopped, e)
		}
	}
	m.mu.Unlock()
	for _, e := range stopped {
		stopWeComEntry(e)
	}
	if m.ctx.Err() != nil {
		return
	}
	for id, b := range wanted {
		m.mu.RLock()
		existing := m.entries[id]
		m.mu.RUnlock()
		if existing != nil {
			continue
		}
		ctx, cancel := context.WithCancel(m.ctx)
		e := &wecomBotEntry{bot: b, ctx: ctx, cancel: cancel, frames: make(chan wecomws.Frame, 64), loopDone: make(chan struct{}), clientDone: make(chan struct{}), status: wecomws.Status{State: "connecting"}}
		e.client = m.newClient(wecomws.Config{BotID: b.BotID, Secret: b.Secret}, func(frame wecomws.Frame) {
			select {
			case e.frames <- frame:
			case <-ctx.Done():
			default:
			}
		}, func(status wecomws.Status) {
			e.mu.Lock()
			terminal := e.status.State == "auth_failed" || e.status.State == "superseded"
			if !terminal {
				e.status = status
			}
			e.mu.Unlock()
			if status.State == "auth_failed" || status.State == "superseded" {
				e.cancel()
			}
		})
		m.mu.Lock()
		m.entries[id] = e
		m.mu.Unlock()
		go func() { defer close(e.clientDone); _ = e.client.Run(ctx) }()
		go m.dispatch(e)
	}
}

func stopWeComEntry(e *wecomBotEntry) {
	e.cancel()
	<-e.clientDone
	<-e.loopDone
	e.tasks.Wait()
}

func (m *wecomBotManager) dispatch(e *wecomBotEntry) {
	defer close(e.loopDone)
	gate := make(chan struct{}, 32)
	for {
		select {
		case <-e.ctx.Done():
			return
		case frame := <-e.frames:
			if frame.Cmd != "aibot_msg_callback" {
				continue
			}
			select {
			case gate <- struct{}{}:
				e.tasks.Add(1)
				go func() { defer e.tasks.Done(); defer func() { <-gate }(); m.handleMessage(e, frame) }()
			default:
				// Reply within the same bounded dispatch loop when overloaded.
				m.sendBusy(e, frame)
			}
		}
	}
}

type wecomBotMessage struct {
	MessageID   string          `json:"msgid"`
	BotID       string          `json:"aibotid"`
	ChatID      string          `json:"chatid"`
	ChatType    string          `json:"chattype"`
	MessageType string          `json:"msgtype"`
	CompatInfo  json.RawMessage `json:"compat_info"`
	From        struct {
		UserID string `json:"userid"`
	} `json:"from"`
	Text struct {
		Content string `json:"content"`
	} `json:"text"`
}

func parseWeComIncoming(b model.AiBot, frame wecomws.Frame) (wecomBotMessage, AiBotIncoming, bool) {
	var msg wecomBotMessage
	if len(frame.Headers.ReqID) > 512 || frame.Headers.ReqID == "" || json.Unmarshal(frame.Body, &msg) != nil || msg.MessageType != "text" || msg.BotID != b.BotID || len(msg.CompatInfo) != 0 {
		return msg, AiBotIncoming{}, false
	}
	conversation := "single:"
	if msg.ChatType == "group" {
		if msg.ChatID == "" {
			return msg, AiBotIncoming{}, false
		}
		conversation = "group:" + msg.ChatID
	} else if msg.ChatType != "single" {
		return msg, AiBotIncoming{}, false
	}
	return msg, AiBotIncoming{MessageID: msg.MessageID, ConversationID: conversation, SenderID: msg.From.UserID, Text: strings.TrimSpace(msg.Text.Content)}, true
}

func wecomChannelKey(b model.AiBot, msg wecomBotMessage) string {
	chat := msg.ChatID
	if msg.ChatType == "single" {
		chat = msg.From.UserID
	}
	// Sending quota belongs to the chat, while AI context also includes sender.
	return botHash("wecom", b.BotID, msg.ChatType, chat)
}

func (m *wecomBotManager) currentWeComEntry(e *wecomBotEntry) bool {
	current, err := GetAiBot(e.bot.ID, true)
	return err == nil && sameWeComConfig(*current, e.bot) && e.ctx.Err() == nil
}

func (m *wecomBotManager) replyWeCom(ctx context.Context, e *wecomBotEntry, frame wecomws.Frame, msg wecomBotMessage, streamID, content string, finish, wait bool) error {
	attempt, err := m.limiter.acquire(ctx, wecomChannelKey(e.bot, msg), wait)
	if err != nil {
		return err
	}
	defer m.limiter.complete(attempt)
	sendCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return e.client.ReplyStream(sendCtx, frame.Headers.ReqID, streamID, botprotocol.ClipUTF8(content, 20480), finish)
}

func (m *wecomBotManager) sendBusy(e *wecomBotEntry, frame wecomws.Frame) {
	msg, in, ok := parseWeComIncoming(e.bot, frame)
	if !ok || !m.currentWeComEntry(e) {
		return
	}
	d, fresh, err := ClaimAiBotMessage(&e.bot, in)
	if err != nil || !fresh {
		return
	}
	ctx, cancel := context.WithTimeout(e.ctx, 3*time.Second)
	defer cancel()
	reply := "当前请求较多，请稍后重新发送消息。"
	status := "failed"
	if err := m.replyWeCom(ctx, e, frame, msg, d.StreamID, reply, true, true); err != nil {
		status = "reply_failed"
	}
	finishBotDelivery(d.ID, status, reply)
}

func (m *wecomBotManager) handleMessage(e *wecomBotEntry, frame wecomws.Frame) {
	msg, in, ok := parseWeComIncoming(e.bot, frame)
	if !ok || !m.currentWeComEntry(e) {
		return
	}
	d, fresh, err := ClaimAiBotMessage(&e.bot, in)
	// A callback retry has no independent stream lifecycle. In particular, a
	// stream acknowledged with finish=true must never be refreshed again.
	if err != nil || !fresh {
		return
	}
	deadline := d.CreatedAt.Add(9 * time.Minute)
	if d.ExpiresAt != nil {
		deadline = *d.ExpiresAt
	}
	ctx, cancel := context.WithDeadline(e.ctx, deadline)
	defer cancel()
	send := func(content string, finish, wait bool) error {
		return m.replyWeCom(ctx, e, frame, msg, d.StreamID, content, finish, wait)
	}
	if err := send("正在思考，请稍候…", false, true); err != nil {
		finishBotDelivery(d.ID, "reply_failed", AiBotFailureReply)
		return
	}
	done := make(chan struct{})
	var content strings.Builder
	lastProgress := time.Now()
	progress := func(delta string) {
		content.WriteString(delta)
		if time.Since(lastProgress) >= 5*time.Second {
			_ = send(content.String(), false, false)
			lastProgress = time.Now()
		}
	}
	if !startAiBotMessage(ctx, e.bot, *d, in, func(reply string) error { return send(reply, true, true) }, progress, func() { close(done) }) {
		reply := "当前请求较多，请稍后重新发送消息。"
		status := "failed"
		if err := send(reply, true, true); err != nil {
			status = "reply_failed"
		}
		finishBotDelivery(d.ID, status, reply)
		return
	}
	<-done
}
