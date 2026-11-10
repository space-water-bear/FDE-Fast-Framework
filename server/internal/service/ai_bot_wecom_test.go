package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"fde-admin/internal/global"
	"fde-admin/internal/model"
	"fde-admin/internal/wecomws"

	"gorm.io/gorm/logger"
)

type fakeWeComStream struct {
	reqID, streamID, content string
	finish                   bool
}

type fakeWeComConnection struct {
	cfg       wecomws.Config
	onMessage func(wecomws.Frame)
	onStatus  func(wecomws.Status)
	started   chan struct{}
	stopped   chan struct{}
	replies   chan fakeWeComStream
}

func (f *fakeWeComConnection) Run(ctx context.Context) error {
	close(f.started)
	f.onStatus(wecomws.Status{State: "connected", LastConnectedAt: time.Now()})
	<-ctx.Done()
	f.onStatus(wecomws.Status{State: "stopped"})
	close(f.stopped)
	return ctx.Err()
}

func (f *fakeWeComConnection) ReplyStream(ctx context.Context, reqID, streamID, content string, finish bool) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case f.replies <- fakeWeComStream{reqID, streamID, content, finish}:
		return nil
	}
}

type fakeWeComFactory struct {
	mu      sync.Mutex
	clients []*fakeWeComConnection
	created chan *fakeWeComConnection
}

func (f *fakeWeComFactory) create(cfg wecomws.Config, onMessage func(wecomws.Frame), onStatus func(wecomws.Status)) wecomConnection {
	c := &fakeWeComConnection{cfg: cfg, onMessage: onMessage, onStatus: onStatus, started: make(chan struct{}), stopped: make(chan struct{}), replies: make(chan fakeWeComStream, 64)}
	f.mu.Lock()
	f.clients = append(f.clients, c)
	f.mu.Unlock()
	f.created <- c
	return c
}

func (f *fakeWeComFactory) count() int { f.mu.Lock(); defer f.mu.Unlock(); return len(f.clients) }

func awaitWeComTest(t *testing.T, condition func() bool, label string) {
	t.Helper()
	deadline := time.NewTimer(3 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		if condition() {
			return
		}
		select {
		case <-deadline.C:
			t.Fatalf("timed out: %s", label)
		case <-ticker.C:
		}
	}
}

func nextFakeWeCom(t *testing.T, f *fakeWeComFactory) *fakeWeComConnection {
	t.Helper()
	select {
	case c := <-f.created:
		select {
		case <-c.started:
			return c
		case <-time.After(3 * time.Second):
			t.Fatal("fake connection did not start")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("manager did not create connection")
	}
	return nil
}

func setupWeComManagerTest(t *testing.T) (*wecomBotManager, *fakeWeComFactory) {
	t.Helper()
	setupTestDB(t)
	global.DB.Logger = global.DB.Logger.LogMode(logger.Silent)
	if err := global.DB.AutoMigrate(&model.AiBotConversation{}, &model.AiBotDelivery{}, &model.AiBotSendAttempt{}, &model.AiUsage{}); err != nil {
		t.Fatal(err)
	}
	db := global.DB
	t.Cleanup(func() {
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	})
	f := &fakeWeComFactory{created: make(chan *fakeWeComConnection, 16)}
	m := newWeComBotManager(context.Background(), db, f.create)
	wecomManagerState.Lock()
	previous := wecomManagerState.manager
	wecomManagerState.manager = m
	wecomManagerState.Unlock()
	t.Cleanup(func() {
		m.cancel()
		select {
		case <-m.done:
		case <-time.After(5 * time.Second):
			t.Error("manager did not stop all client/message tasks")
		}
		wecomManagerState.Lock()
		if wecomManagerState.manager == m {
			wecomManagerState.manager = previous
		}
		wecomManagerState.Unlock()
	})
	return m, f
}

func seedWeComManagerBot(t *testing.T, enabled bool, agentID uint) model.AiBot {
	t.Helper()
	if agentID == 0 {
		a := model.AiAgent{Name: "manager-agent", Enabled: true}
		if err := global.DB.Create(&a).Error; err != nil {
			t.Fatal(err)
		}
		agentID = a.ID
	}
	b := model.AiBot{Name: "WeCom QA", Platform: "wecom", AgentID: agentID, BotID: "bot-manager-qa", Secret: "secret-manager-qa", Enabled: enabled}
	if err := global.DB.Create(&b).Error; err != nil {
		t.Fatal(err)
	}
	return b
}

func TestWeComManagerConfigurationLifecycle(t *testing.T) {
	m, f := setupWeComManagerTest(t)
	b := seedWeComManagerBot(t, true, 0)
	go m.run()
	first := nextFakeWeCom(t, f)
	if first.cfg.BotID != b.BotID || first.cfg.Secret != b.Secret {
		t.Fatal("startup did not use stored credentials")
	}
	awaitWeComTest(t, func() bool { return WeComBotConnectionStatus(b).State == "connected" }, "connected status")
	input := AiBotInput{Name: b.Name, Platform: b.Platform, AgentID: b.AgentID, BotID: b.BotID, Secret: "rotated-secret", Enabled: true}
	updated, err := SaveAiBot(b.ID, input)
	if err != nil {
		t.Fatal(err)
	}
	second := nextFakeWeCom(t, f)
	select {
	case <-first.stopped:
	default:
		t.Fatal("credential update opened new connection before closing old connection")
	}
	if second.cfg.Secret != input.Secret {
		t.Fatal("credential update reused old secret")
	}
	input.Enabled, input.Secret = false, ""
	updated, err = SaveAiBot(b.ID, input)
	if err != nil {
		t.Fatal(err)
	}
	awaitWeComTest(t, func() bool {
		select {
		case <-second.stopped:
			return true
		default:
			return false
		}
	}, "disabled connection stop")
	if WeComBotConnectionStatus(*updated).State != "disabled" {
		t.Fatal("disabled bot did not report disabled")
	}
	input.Enabled = true
	if _, err := SaveAiBot(b.ID, input); err != nil {
		t.Fatal(err)
	}
	third := nextFakeWeCom(t, f)
	if err := DeleteAiBot(b.ID); err != nil {
		t.Fatal(err)
	}
	awaitWeComTest(t, func() bool {
		select {
		case <-third.stopped:
			return true
		default:
			return false
		}
	}, "deleted connection stop")
	awaitWeComTest(t, func() bool { m.mu.RLock(); defer m.mu.RUnlock(); return len(m.entries) == 0 }, "deleted entry removed")
}

func TestWeComManagerTerminalStateDoesNotReconnect(t *testing.T) {
	for _, terminal := range []struct{ transport, visible string }{{"auth_failed", "auth_failed"}, {"superseded", "conflict"}} {
		t.Run(terminal.transport, func(t *testing.T) {
			m, f := setupWeComManagerTest(t)
			b := seedWeComManagerBot(t, true, 0)
			go m.run()
			client := nextFakeWeCom(t, f)
			client.onStatus(wecomws.Status{State: terminal.transport})
			awaitWeComTest(t, func() bool { return WeComBotConnectionStatus(b).State == terminal.visible }, "terminal status preserved")
			awaitWeComTest(t, func() bool {
				select {
				case <-client.stopped:
					return true
				default:
					return false
				}
			}, "terminal client stopped")
			// Every wake uses the same reconciliation path as the periodic tick.
			for i := 0; i < 3; i++ {
				notifyWeComBots()
				time.Sleep(20 * time.Millisecond)
			}
			if f.count() != 1 {
				t.Fatal("terminal state was automatically reconnected")
			}
			if WeComBotConnectionStatus(b).State != terminal.visible {
				t.Fatal("stopped callback erased terminal status")
			}
		})
	}
}

func emitWeComText(client *fakeWeComConnection, reqID, messageID, sender, chatType, chatID string) {
	client.onMessage(wecomTextFrame(client, reqID, messageID, sender, chatType, chatID))
}

func wecomTextFrame(client *fakeWeComConnection, reqID, messageID, sender, chatType, chatID string) wecomws.Frame {
	body, _ := json.Marshal(map[string]any{"msgid": messageID, "aibotid": client.cfg.BotID, "chattype": chatType, "chatid": chatID, "from": map[string]string{"userid": sender}, "msgtype": "text", "text": map[string]string{"content": "hello"}})
	return wecomws.Frame{Cmd: "aibot_msg_callback", Headers: wecomws.Headers{ReqID: reqID}, Body: body}
}

func wecomTestEntry(t *testing.T, m *wecomBotManager, id uint) *wecomBotEntry {
	t.Helper()
	m.mu.RLock()
	e := m.entries[id]
	m.mu.RUnlock()
	if e == nil {
		t.Fatal("missing robot entry")
	}
	return e
}

func assertNoWeComReply(t *testing.T, client *fakeWeComConnection) {
	t.Helper()
	select {
	case reply := <-client.replies:
		t.Fatalf("unexpected duplicate WebSocket reply: %+v", reply)
	default:
	}
}

func receiveWeComFinal(t *testing.T, client *fakeWeComConnection, reqID string) []fakeWeComStream {
	t.Helper()
	var replies []fakeWeComStream
	deadline := time.NewTimer(3 * time.Second)
	defer deadline.Stop()
	for {
		select {
		case reply := <-client.replies:
			if reply.reqID != reqID {
				t.Fatalf("reply routed to %q, want %q", reply.reqID, reqID)
			}
			replies = append(replies, reply)
			if reply.finish {
				return replies
			}
		case <-deadline.C:
			t.Fatal("no final WebSocket reply")
		}
	}
}

func TestWeComManagerTextRoundTripDedupAndIsolation(t *testing.T) {
	m, f := setupWeComManagerTest(t)
	var modelCalls, mcpCalls atomic.Int32
	mcp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mcpCalls.Add(1)
		http.Error(w, "MCP must not be called", http.StatusForbidden)
	}))
	t.Cleanup(mcp.Close)
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var request map[string]json.RawMessage
		if err := json.Unmarshal(body, &request); err != nil {
			t.Errorf("invalid model request: %v", err)
		}
		if tools := strings.TrimSpace(string(request["tools"])); tools != "" && tools != "null" && tools != "[]" {
			t.Errorf("robot exposed tools: %s", tools)
		}
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("unexpected model path: %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		modelCalls.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"id\":\"ws-qa\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"qa\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"WebSocket 回复成功\"}}]}\n\ndata: [DONE]\n\n")
	}))
	t.Cleanup(gateway.Close)
	p := model.AiProvider{Name: "WS Mock", BaseURL: gateway.URL + "/v1", APIKey: "test-only", APIMode: "chat", Model: "qa", Enabled: true}
	if err := global.DB.Create(&p).Error; err != nil {
		t.Fatal(err)
	}
	a := model.AiAgent{Name: "WS Agent", ProviderID: p.ID, Enabled: true, Tools: []model.AiTool{{Name: "apply_files", Schema: `{"type":"object","properties":{}}`, Enabled: true}}, McpServers: []model.AiMcpServer{{Name: "Forbidden MCP", Transport: "streamable", URL: mcp.URL, Enabled: true}}}
	if err := global.DB.Create(&a).Error; err != nil {
		t.Fatal(err)
	}
	b := seedWeComManagerBot(t, true, a.ID)
	go m.run()
	client := nextFakeWeCom(t, f)
	for i, turn := range []struct{ message, sender, chatType, chatID string }{
		{"msg-1", "user-a", "group", "group-1"},
		{"msg-2", "user-a", "group", "group-1"}, // same context
		{"msg-3", "user-b", "group", "group-1"}, // other sender
		{"msg-4", "user-a", "group", "group-2"}, // other group
		{"msg-5", "user-a", "single", ""},
	} {
		reqID := fmt.Sprintf("req-%d", i)
		emitWeComText(client, reqID, turn.message, turn.sender, turn.chatType, turn.chatID)
		replies := receiveWeComFinal(t, client, reqID)
		last := replies[len(replies)-1]
		if last.content != "WebSocket 回复成功" || last.streamID == "" {
			t.Fatalf("invalid final reply: %+v", last)
		}
		if len(replies) < 2 || replies[0].finish || replies[0].streamID != last.streamID {
			t.Fatalf("missing initial stream or stream changed: %+v", replies)
		}
		awaitWeComTest(t, func() bool {
			var d model.AiBotDelivery
			return global.DB.Where("bot_id = ? AND message_key = ?", b.ID, botMessageKey(b, turn.message)).First(&d).Error == nil && d.Status == "completed"
		}, "durable completion")
		// The final stream has been ACKed. A duplicate must not attempt to
		// update that finished stream or produce another visible message.
		m.handleMessage(wecomTestEntry(t, m, b.ID), wecomTextFrame(client, "duplicate-"+reqID, turn.message, turn.sender, turn.chatType, turn.chatID))
		assertNoWeComReply(t, client)
	}
	if modelCalls.Load() != 5 {
		t.Fatalf("model calls = %d, want 5", modelCalls.Load())
	}
	if mcpCalls.Load() != 0 {
		t.Fatalf("robot called MCP %d times", mcpCalls.Load())
	}
	var bindings []model.AiBotConversation
	if err := global.DB.Where("bot_id = ?", b.ID).Find(&bindings).Error; err != nil {
		t.Fatal(err)
	}
	if len(bindings) != 4 {
		t.Fatalf("conversation bindings = %d, want 4", len(bindings))
	}
	var userMessages, assistants int64
	if err := global.DB.Model(&model.AiMessage{}).Where("role = ?", "user").Count(&userMessages).Error; err != nil {
		t.Fatal(err)
	}
	if err := global.DB.Model(&model.AiMessage{}).Where("role = ?", "assistant").Count(&assistants).Error; err != nil {
		t.Fatal(err)
	}
	if userMessages != 5 || assistants != 5 {
		t.Fatalf("saved user/assistant messages = %d/%d", userMessages, assistants)
	}
}

func seedWeComHTTPTestAgent(t *testing.T, gatewayURL string) model.AiAgent {
	t.Helper()
	p := model.AiProvider{Name: "WS lifecycle mock", BaseURL: gatewayURL + "/v1", APIKey: "test-only", APIMode: "chat", Model: "qa", Enabled: true}
	if err := global.DB.Create(&p).Error; err != nil {
		t.Fatal(err)
	}
	a := model.AiAgent{Name: "WS lifecycle agent", ProviderID: p.ID, Enabled: true}
	if err := global.DB.Create(&a).Error; err != nil {
		t.Fatal(err)
	}
	return a
}

func writeWeComTestStream(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/event-stream")
	fmt.Fprint(w, "data: {\"id\":\"ws-qa\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"qa\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"WebSocket 回复成功\"}}]}\n\ndata: [DONE]\n\n")
}

func TestWeComManagerProcessingDuplicateDoesNotRefreshOrFinishTwice(t *testing.T) {
	m, f := setupWeComManagerTest(t)
	var calls atomic.Int32
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	var releaseOnce sync.Once
	gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		select {
		case started <- struct{}{}:
		default:
		}
		select {
		case <-release:
			writeWeComTestStream(w)
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(gw.Close)
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
	a := seedWeComHTTPTestAgent(t, gw.URL)
	b := seedWeComManagerBot(t, true, a.ID)
	go m.run()
	client := nextFakeWeCom(t, f)
	emitWeComText(client, "original", "processing-msg", "alice", "single", "")
	select {
	case initial := <-client.replies:
		if initial.reqID != "original" || initial.finish {
			t.Fatalf("invalid initial stream: %+v", initial)
		}
	case <-time.After(time.Second):
		t.Fatal("no initial stream")
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("model did not start")
	}
	e := wecomTestEntry(t, m, b.ID)
	duplicateDone := make(chan struct{})
	go func() {
		m.handleMessage(e, wecomTextFrame(client, "processing-retry", "processing-msg", "alice", "single", ""))
		close(duplicateDone)
	}()
	select {
	case <-duplicateDone:
	case <-time.After(time.Second):
		t.Fatal("processing duplicate did not return promptly")
	}
	assertNoWeComReply(t, client)
	if calls.Load() != 1 {
		t.Fatalf("processing retry called model %d times", calls.Load())
	}
	releaseOnce.Do(func() { close(release) })
	replies := receiveWeComFinal(t, client, "original")
	if len(replies) != 1 || replies[0].content != "WebSocket 回复成功" {
		t.Fatalf("duplicate final streams: %+v", replies)
	}
	awaitWeComTest(t, func() bool {
		var d model.AiBotDelivery
		return global.DB.Where("bot_id = ? AND message_key = ?", b.ID, botMessageKey(b, "processing-msg")).First(&d).Error == nil && d.Status == "completed"
	}, "original stream completion")
	m.handleMessage(wecomTestEntry(t, m, b.ID), wecomTextFrame(client, "terminal-retry", "processing-msg", "alice", "single", ""))
	assertNoWeComReply(t, client)
	if calls.Load() != 1 {
		t.Fatalf("terminal retry called model %d times", calls.Load())
	}
}

func TestWeComManagerBusyRepliesUseDistinctStreamsAndDeduplicate(t *testing.T) {
	m, f := setupWeComManagerTest(t)
	b := seedWeComManagerBot(t, true, 0)
	go m.run()
	client := nextFakeWeCom(t, f)
	e := wecomTestEntry(t, m, b.ID)
	first := wecomTextFrame(client, "busy-req-1", "busy-msg-1", "alice", "group", "shared-group")
	second := wecomTextFrame(client, "busy-req-2", "busy-msg-2", "bob", "group", "shared-group")
	m.sendBusy(e, first)
	firstReplies := receiveWeComFinal(t, client, "busy-req-1")
	m.sendBusy(e, second)
	secondReplies := receiveWeComFinal(t, client, "busy-req-2")
	if len(firstReplies) != 1 || len(secondReplies) != 1 || firstReplies[0].streamID == "" || secondReplies[0].streamID == "" || firstReplies[0].streamID == secondReplies[0].streamID {
		t.Fatalf("busy callbacks reused stream IDs: first=%+v second=%+v", firstReplies, secondReplies)
	}
	// A retried overloaded message must not update its already-finished stream.
	m.sendBusy(e, wecomTextFrame(client, "busy-req-retry", "busy-msg-1", "alice", "group", "shared-group"))
	assertNoWeComReply(t, client)
	var receipts, messages int64
	if err := global.DB.Model(&model.AiBotDelivery{}).Where("bot_id = ?", b.ID).Count(&receipts).Error; err != nil {
		t.Fatal(err)
	}
	if err := global.DB.Model(&model.AiMessage{}).Count(&messages).Error; err != nil {
		t.Fatal(err)
	}
	if receipts != 2 || messages != 0 {
		t.Fatalf("busy handling created extra work: receipts=%d AI messages=%d", receipts, messages)
	}
}

func TestWeComManagerIgnoresCompatInfoInsteadOfProcessingOrdinaryText(t *testing.T) {
	m, f := setupWeComManagerTest(t)
	var calls atomic.Int32
	gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); writeWeComTestStream(w) }))
	t.Cleanup(gw.Close)
	a := seedWeComHTTPTestAgent(t, gw.URL)
	b := seedWeComManagerBot(t, true, a.ID)
	go m.run()
	client := nextFakeWeCom(t, f)
	e := wecomTestEntry(t, m, b.ID)
	for i, value := range []any{nil, map[string]any{}, map[string]any{"source": "ai_assistant"}, ""} {
		frame := wecomTextFrame(client, fmt.Sprintf("compat-%d", i), fmt.Sprintf("compat-msg-%d", i), "alice", "single", "")
		var body map[string]any
		if err := json.Unmarshal(frame.Body, &body); err != nil {
			t.Fatal(err)
		}
		body["compat_info"] = value
		frame.Body, _ = json.Marshal(body)
		m.handleMessage(e, frame)
		assertNoWeComReply(t, client)
	}
	var receipts int64
	if err := global.DB.Model(&model.AiBotDelivery{}).Count(&receipts).Error; err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 0 || receipts != 0 {
		t.Fatalf("compat_info was treated as ordinary chat: model=%d receipts=%d", calls.Load(), receipts)
	}
	// A normal callback still goes through the active handler, proving the
	// compatibility filter did not disable all text processing.
	m.handleMessage(e, wecomTextFrame(client, "ordinary", "ordinary-msg", "alice", "single", ""))
	replies := receiveWeComFinal(t, client, "ordinary")
	if len(replies) < 2 || calls.Load() != 1 {
		t.Fatalf("ordinary text is not processed: replies=%d calls=%d", len(replies), calls.Load())
	}
}
