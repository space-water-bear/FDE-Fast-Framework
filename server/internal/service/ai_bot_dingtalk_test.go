package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"fde-admin/internal/dingtalkstream"
	"fde-admin/internal/global"
	"fde-admin/internal/model"
	"gorm.io/gorm/logger"
)

type fakeDingTalkConnection struct {
	cfg              dingtalkstream.Config
	onMessage        func(dingtalkstream.Frame) bool
	onStatus         func(dingtalkstream.Status)
	started, stopped chan struct{}
}

func (f *fakeDingTalkConnection) Run(ctx context.Context) error {
	f.onStatus(dingtalkstream.Status{State: "connected", LastConnectedAt: time.Now()})
	close(f.started)
	<-ctx.Done()
	f.onStatus(dingtalkstream.Status{State: "stopped"})
	close(f.stopped)
	return ctx.Err()
}

type fakeDingTalkFactory struct {
	mu      sync.Mutex
	clients []*fakeDingTalkConnection
	created chan *fakeDingTalkConnection
}

func (f *fakeDingTalkFactory) create(cfg dingtalkstream.Config, message func(dingtalkstream.Frame) bool, status func(dingtalkstream.Status)) dingTalkConnection {
	c := &fakeDingTalkConnection{cfg: cfg, onMessage: message, onStatus: status, started: make(chan struct{}), stopped: make(chan struct{})}
	f.mu.Lock()
	f.clients = append(f.clients, c)
	f.mu.Unlock()
	f.created <- c
	return c
}

func setupDingTalkManagerTest(t *testing.T) (*dingTalkBotManager, *fakeDingTalkFactory) {
	t.Helper()
	setupTestDB(t)
	global.DB.Logger = global.DB.Logger.LogMode(logger.Silent)
	if err := global.DB.AutoMigrate(&model.AiBotConversation{}, &model.AiBotDelivery{}, &model.AiUsage{}); err != nil {
		t.Fatal(err)
	}
	db := global.DB
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	f := &fakeDingTalkFactory{created: make(chan *fakeDingTalkConnection, 16)}
	m := newDingTalkBotManager(context.Background(), db, f.create)
	dingTalkManagerState.Lock()
	previous := dingTalkManagerState.manager
	dingTalkManagerState.manager = m
	dingTalkManagerState.Unlock()
	t.Cleanup(func() {
		m.cancel()
		select {
		case <-m.done:
		case <-time.After(5 * time.Second):
			t.Error("DingTalk manager did not stop")
		}
		dingTalkManagerState.Lock()
		if dingTalkManagerState.manager == m {
			dingTalkManagerState.manager = previous
		}
		dingTalkManagerState.Unlock()
	})
	return m, f
}

func seedDingTalkManagerBot(t *testing.T, agentID uint) model.AiBot {
	t.Helper()
	if agentID == 0 {
		a := model.AiAgent{Name: "Ding manager agent", Enabled: true}
		if err := global.DB.Create(&a).Error; err != nil {
			t.Fatal(err)
		}
		agentID = a.ID
	}
	b := model.AiBot{Name: "Ding QA", Platform: "dingtalk", AgentID: agentID, ClientID: "app-manager-qa", ClientSecret: "secret-manager-qa", Enabled: true}
	if err := global.DB.Create(&b).Error; err != nil {
		t.Fatal(err)
	}
	return b
}

func nextDingTalkClient(t *testing.T, f *fakeDingTalkFactory) *fakeDingTalkConnection {
	t.Helper()
	select {
	case c := <-f.created:
		select {
		case <-c.started:
			return c
		case <-time.After(3 * time.Second):
			t.Fatal("DingTalk client did not start")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("manager did not create DingTalk client")
	}
	return nil
}

func awaitDingTalk(t *testing.T, condition func() bool, label string) {
	t.Helper()
	deadline := time.NewTimer(3 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(5 * time.Millisecond)
	defer tick.Stop()
	for {
		if condition() {
			return
		}
		select {
		case <-deadline.C:
			t.Fatalf("timed out: %s", label)
		case <-tick.C:
		}
	}
}

func dingTalkTextFrame(messageID, conversation, sender string) dingtalkstream.Frame {
	body, _ := json.Marshal(map[string]any{
		"msgId": messageID, "conversationId": conversation, "conversationType": "2", "senderId": sender, "msgtype": "text",
		"text": map[string]string{"content": "hello"}, "sessionWebhook": "https://oapi.dingtalk.com/robot/sendBySession?session=local-test-only",
		"sessionWebhookExpiredTime": time.Now().Add(5 * time.Minute).UnixMilli(),
	})
	return dingtalkstream.Frame{SpecVersion: "1.0", Type: "CALLBACK", Headers: map[string]string{"topic": dingtalkstream.RobotTopic, "messageId": "delivery-" + messageID}, Data: string(body)}
}

func TestDingTalkManagerConfigurationLifecycle(t *testing.T) {
	m, f := setupDingTalkManagerTest(t)
	b := seedDingTalkManagerBot(t, 0)
	go m.run()
	first := nextDingTalkClient(t, f)
	if first.cfg.ClientID != b.ClientID || first.cfg.ClientSecret != b.ClientSecret {
		t.Fatal("stored credentials not used")
	}
	if DingTalkBotConnectionStatus(b).State != "connected" {
		t.Fatal("connected status absent")
	}
	in := AiBotInput{Name: b.Name, Platform: b.Platform, AgentID: b.AgentID, ClientID: b.ClientID, ClientSecret: "rotated-secret", Enabled: true}
	updated, err := SaveAiBot(b.ID, in)
	if err != nil {
		t.Fatal(err)
	}
	second := nextDingTalkClient(t, f)
	select {
	case <-first.stopped:
	default:
		t.Fatal("old client not stopped before replacement")
	}
	if second.cfg.ClientSecret != in.ClientSecret {
		t.Fatal("replacement did not use rotated secret")
	}
	in.Enabled = false
	in.ClientSecret = ""
	updated, err = SaveAiBot(b.ID, in)
	if err != nil {
		t.Fatal(err)
	}
	awaitDingTalk(t, func() bool {
		select {
		case <-second.stopped:
			return true
		default:
			return false
		}
	}, "disabled connection closes")
	if DingTalkBotConnectionStatus(*updated).State != "disabled" {
		t.Fatal("disabled status absent")
	}
	in.Enabled = true
	if _, err := SaveAiBot(b.ID, in); err != nil {
		t.Fatal(err)
	}
	third := nextDingTalkClient(t, f)
	if err := DeleteAiBot(b.ID); err != nil {
		t.Fatal(err)
	}
	awaitDingTalk(t, func() bool {
		select {
		case <-third.stopped:
			return true
		default:
			return false
		}
	}, "deleted connection closes")
}

func TestDingTalkManagerAuthFailureIsTerminal(t *testing.T) {
	m, f := setupDingTalkManagerTest(t)
	b := seedDingTalkManagerBot(t, 0)
	go m.run()
	c := nextDingTalkClient(t, f)
	c.onStatus(dingtalkstream.Status{State: "auth_failed"})
	awaitDingTalk(t, func() bool {
		select {
		case <-c.stopped:
			return true
		default:
			return false
		}
	}, "auth failure closes client")
	if DingTalkBotConnectionStatus(b).State != "auth_failed" {
		t.Fatal("auth failure overwritten by stopped")
	}
	notifyDingTalkBots()
	select {
	case <-f.created:
		t.Fatal("unchanged failed credential was retried")
	case <-time.After(30 * time.Millisecond):
	}
}

func TestDingTalkManagerDurableACKAsyncReplyAndDedup(t *testing.T) {
	m, f := setupDingTalkManagerTest(t)
	var modelCalls atomic.Int32
	started := make(chan struct{}, 8)
	release := make(chan struct{})
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(release) }) })
	gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var data map[string]json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
			t.Error(err)
		}
		if tools := strings.TrimSpace(string(data["tools"])); tools != "" && tools != "null" && tools != "[]" {
			t.Errorf("external bot received internal tools: %s", tools)
		}
		modelCalls.Add(1)
		started <- struct{}{}
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"id\":\"ding-qa\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"qa\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"DingTalk reply success\"}}]}\n\ndata: [DONE]\n\n")
	}))
	t.Cleanup(gw.Close)
	t.Cleanup(m.cancel)
	p := model.AiProvider{Name: "Ding mock", BaseURL: gw.URL + "/v1", APIKey: "test-only", APIMode: "chat", Model: "qa", Enabled: true}
	if err := global.DB.Create(&p).Error; err != nil {
		t.Fatal(err)
	}
	a := model.AiAgent{Name: "Ding agent", ProviderID: p.ID, Enabled: true, Tools: []model.AiTool{{Name: "apply_files", Schema: `{"type":"object","properties":{}}`, Enabled: true}}, McpServers: []model.AiMcpServer{{Name: "must-not-connect", Transport: "streamable", URL: "http://127.0.0.1:1/no", Enabled: true}}}
	if err := global.DB.Create(&a).Error; err != nil {
		t.Fatal(err)
	}
	b := seedDingTalkManagerBot(t, a.ID)
	replies := make(chan string, 8)
	m.reply = func(ctx context.Context, webhook, text string) error {
		select {
		case replies <- text:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	go m.run()
	c := nextDingTalkClient(t, f)
	frame := dingTalkTextFrame("message-1", "group-1", "alice")
	acked := make(chan bool, 1)
	go func() { acked <- c.onMessage(frame) }()
	select {
	case ok := <-acked:
		if !ok {
			t.Fatal("valid callback not ACKed")
		}
	case <-time.After(time.Second):
		t.Fatal("ACK blocked on model")
	}
	var receipts int64
	global.DB.Model(&model.AiBotDelivery{}).Count(&receipts)
	if receipts != 1 {
		t.Fatalf("ACK preceded durable receipt: %d", receipts)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("model not started")
	}
	if !c.onMessage(frame) {
		t.Fatal("duplicate not ACKed")
	}
	select {
	case <-replies:
		t.Fatal("model still blocked but reply sent")
	default:
	}
	once.Do(func() { close(release) })
	select {
	case text := <-replies:
		if text != "DingTalk reply success" {
			t.Fatalf("reply=%q", text)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("reply missing")
	}
	awaitDingTalk(t, func() bool {
		var d model.AiBotDelivery
		return global.DB.Where("bot_id = ?", b.ID).First(&d).Error == nil && d.Status == "completed"
	}, "durable completion")
	if !c.onMessage(frame) {
		t.Fatal("completed duplicate not ACKed")
	}
	select {
	case <-replies:
		t.Fatal("duplicate caused second HTTP reply")
	case <-time.After(20 * time.Millisecond):
	}
	if modelCalls.Load() != 1 {
		t.Fatalf("model calls=%d", modelCalls.Load())
	}
	in := AiBotInput{Name: b.Name, Platform: b.Platform, AgentID: b.AgentID, ClientID: "other-app", ClientSecret: "other-secret", Enabled: true}
	updated, err := SaveAiBot(b.ID, in)
	if err != nil {
		t.Fatal(err)
	}
	other := nextDingTalkClient(t, f)
	if !other.onMessage(frame) {
		t.Fatal("new app callback rejected")
	}
	select {
	case <-replies:
	case <-time.After(3 * time.Second):
		t.Fatal("new app reused old duplicate receipt")
	}
	awaitDingTalk(t, func() bool {
		var n int64
		return global.DB.Model(&model.AiBotConversation{}).Where("bot_id = ?", updated.ID).Count(&n).Error == nil && n == 2
	}, "new Client ID isolates history")
	if modelCalls.Load() != 2 {
		t.Fatalf("expected one call per app, got %d", modelCalls.Load())
	}
}

func TestDingTalkManagerIgnoresUnsafeOrUnsupportedCallbacks(t *testing.T) {
	m, f := setupDingTalkManagerTest(t)
	b := seedDingTalkManagerBot(t, 0)
	m.reply = func(context.Context, string, string) error { t.Error("unexpected reply"); return nil }
	go m.run()
	c := nextDingTalkClient(t, f)
	for _, change := range []map[string]any{{"sessionWebhook": "http://127.0.0.1/private"}, {"sessionWebhookExpiredTime": time.Now().Add(-time.Second).UnixMilli()}, {"msgtype": "picture"}, {"senderId": ""}, {"errorCode": 20001, "text": nil}} {
		frame := dingTalkTextFrame("ignored", "group", "alice")
		var body map[string]any
		_ = json.Unmarshal([]byte(frame.Data), &body)
		for key, value := range change {
			body[key] = value
		}
		data, _ := json.Marshal(body)
		frame.Data = string(data)
		if !c.onMessage(frame) {
			t.Fatal("invalid callback should be consumed without retry")
		}
	}
	var count int64
	global.DB.Model(&model.AiBotDelivery{}).Where("bot_id = ?", b.ID).Count(&count)
	if count != 0 {
		t.Fatal("unsupported callback triggered a model receipt")
	}
}

func TestDingTalkManagerBusyIsDurableAndNotRepliedTwice(t *testing.T) {
	m, f := setupDingTalkManagerTest(t)
	b := seedDingTalkManagerBot(t, 0)
	var replies atomic.Int32
	m.reply = func(context.Context, string, string) error { replies.Add(1); return nil }
	go m.run()
	c := nextDingTalkClient(t, f)
	for i := 0; i < cap(botQueue); i++ {
		botQueue <- struct{}{}
	}
	defer func() {
		for i := 0; i < cap(botQueue); i++ {
			<-botQueue
		}
	}()
	frame := dingTalkTextFrame("busy", "group", "alice")
	if !c.onMessage(frame) {
		t.Fatal("busy message not durably accepted")
	}
	var d model.AiBotDelivery
	if err := global.DB.Where("bot_id = ?", b.ID).First(&d).Error; err != nil {
		t.Fatalf("busy receipt lost: %v", err)
	}
	if d.Status != "failed" {
		t.Fatalf("busy status=%s", d.Status)
	}
	if !c.onMessage(frame) {
		t.Fatal("busy duplicate not consumed")
	}
	if replies.Load() != 1 {
		t.Fatalf("busy replies=%d", replies.Load())
	}
}

func TestDingTalkManagerParentCancellationStopsInflightModel(t *testing.T) {
	m, f := setupDingTalkManagerTest(t)
	started, requestCanceled := make(chan struct{}, 1), make(chan struct{}, 1)
	gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		started <- struct{}{}
		<-r.Context().Done()
		requestCanceled <- struct{}{}
	}))
	t.Cleanup(gw.Close)
	t.Cleanup(m.cancel)
	p := model.AiProvider{Name: "cancel provider", BaseURL: gw.URL + "/v1", APIKey: "test-only", APIMode: "chat", Model: "qa", Enabled: true}
	if err := global.DB.Create(&p).Error; err != nil {
		t.Fatal(err)
	}
	a := model.AiAgent{Name: "cancel agent", ProviderID: p.ID, Enabled: true}
	if err := global.DB.Create(&a).Error; err != nil {
		t.Fatal(err)
	}
	seedDingTalkManagerBot(t, a.ID)
	var sent atomic.Int32
	m.reply = func(ctx context.Context, _, _ string) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		sent.Add(1)
		return nil
	}
	go m.run()
	c := nextDingTalkClient(t, f)
	if !c.onMessage(dingTalkTextFrame("cancel", "group", "alice")) {
		t.Fatal("callback not accepted")
	}
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("model not started")
	}
	m.cancel()
	select {
	case <-m.done:
	case <-time.After(3 * time.Second):
		t.Fatal("parent cancellation did not stop tasks")
	}
	select {
	case <-requestCanceled:
	case <-time.After(time.Second):
		t.Fatal("model HTTP request did not receive cancellation")
	}
	if sent.Load() != 0 {
		t.Fatal("canceled task sent an answer")
	}
}

type dingTalkTestRoundTripper func(*http.Request) (*http.Response, error)

func (f dingTalkTestRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestDingTalkReplyRequiresBusinessSuccessAndHonorsContext(t *testing.T) {
	previous := dingTalkReplyClient
	t.Cleanup(func() { dingTalkReplyClient = previous })
	webhook := "https://oapi.dingtalk.com/robot/sendBySession?session=test-only"
	for _, body := range []string{`{}`, `{"errcode":123,"errmsg":"rejected"}`, `not-json`} {
		dingTalkReplyClient = &http.Client{Transport: dingTalkTestRoundTripper(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
		})}
		if err := sendDingTalkReply(context.Background(), webhook, "hello"); err == nil {
			t.Errorf("invalid response accepted: %s", body)
		}
	}
	dingTalkReplyClient = &http.Client{Transport: dingTalkTestRoundTripper(func(r *http.Request) (*http.Response, error) {
		var body struct {
			Type string `json:"msgtype"`
			Text struct {
				Content string `json:"content"`
			} `json:"text"`
		}
		if json.NewDecoder(r.Body).Decode(&body) != nil || body.Type != "text" || body.Text.Content != "hello" {
			t.Error("invalid outgoing text body")
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"errcode":0,"errmsg":"ok"}`))}, nil
	})}
	if err := sendDingTalkReply(context.Background(), webhook, "hello"); err != nil {
		t.Fatal(err)
	}
	var canceled atomic.Bool
	dingTalkReplyClient = &http.Client{Transport: dingTalkTestRoundTripper(func(r *http.Request) (*http.Response, error) {
		<-r.Context().Done()
		canceled.Store(true)
		return nil, r.Context().Err()
	})}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := sendDingTalkReply(ctx, webhook, "hello"); err == nil || !canceled.Load() {
		t.Fatal("reply ignored caller cancellation")
	}
	if !errors.Is(previous.CheckRedirect(nil, nil), http.ErrUseLastResponse) {
		t.Fatal("reply client follows redirects")
	}
}
