package service

import (
	"bytes"
	"context"
	"log"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm/logger"

	"fde-admin/internal/global"
	"fde-admin/internal/model"
)

func setupBotService(t *testing.T) model.AiBot {
	t.Helper()
	setupTestDB(t)
	if err := global.DB.AutoMigrate(&model.AiBot{}, &model.AiBotConversation{}, &model.AiBotDelivery{}); err != nil {
		t.Fatal(err)
	}
	a := model.AiAgent{Name: "test", Enabled: true}
	if err := global.DB.Create(&a).Error; err != nil {
		t.Fatal(err)
	}
	b := model.AiBot{Name: "bot", AgentID: a.ID, Platform: "dingtalk", ClientID: "test-client", ClientSecret: "test-secret", Enabled: true}
	if err := global.DB.Create(&b).Error; err != nil {
		t.Fatal(err)
	}
	return b
}

func TestAiBotReceiptsOrderAndRestart(t *testing.T) {
	b := setupBotService(t)
	in := AiBotIncoming{MessageID: "1", ConversationID: "group:1", SenderID: "user:1", Text: "hello"}
	first, fresh, err := ClaimAiBotMessage(&b, in)
	if err != nil || !fresh {
		t.Fatalf("claim: %v %v", fresh, err)
	}
	duplicate, fresh, err := ClaimAiBotMessage(&b, in)
	if err != nil || fresh || duplicate.ID != first.ID {
		t.Fatalf("duplicate: %v %v", fresh, err)
	}
	in.MessageID = "2"
	second, _, err := ClaimAiBotMessage(&b, in)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if err := waitBotTurn(ctx, *second); err == nil {
		t.Fatal("later message overtook earlier message")
	}
	if err := global.DB.Model(first).Update("created_at", time.Now().Add(-3*time.Minute)).Error; err != nil {
		t.Fatal(err)
	}
	if err := waitBotTurn(context.Background(), *second); err != nil {
		t.Fatal(err)
	}
	if err := global.DB.First(first, first.ID).Error; err != nil {
		t.Fatal(err)
	}
	if first.Status != "interrupted" {
		t.Fatalf("restart receipt status = %q", first.Status)
	}
	_, fresh, err = ClaimAiBotMessage(&b, AiBotIncoming{MessageID: "1", ConversationID: "group:1", SenderID: "user:1", Text: "hello"})
	if err != nil || fresh {
		t.Fatalf("interrupted receipt replayed: %v %v", fresh, err)
	}
}

func TestAiBotCredentialSQLFailureDoesNotLogSecrets(t *testing.T) {
	b := setupBotService(t)
	if err := global.DB.Exec("CREATE TRIGGER reject_bot_insert BEFORE INSERT ON sys_ai_bots BEGIN SELECT RAISE(ABORT, 'forced credential write failure'); END").Error; err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	global.DB.Logger = logger.New(log.New(&output, "", 0), logger.Config{LogLevel: logger.Warn})
	secret := "unique-secret-that-must-not-appear-in-SQL-logs"
	_, err := SaveAiBot(0, AiBotInput{Name: "test", Platform: "dingtalk", AgentID: b.AgentID, ClientID: "new-client-for-write-failure", ClientSecret: secret})
	if err == nil {
		t.Fatal("expected database write failure")
	}
	if strings.Contains(output.String(), secret) {
		t.Fatal("credential leaked in failed SQL log")
	}
}

func TestAiBotPreventsDeletingItsAgentBeforeFirstMessage(t *testing.T) {
	b := setupBotService(t)
	if err := DeleteAiAgent(b.AgentID); err == nil || !strings.Contains(err.Error(), "机器人") {
		t.Fatalf("bound agent deletion: %v", err)
	}
	if err := DeleteAiBot(b.ID); err != nil {
		t.Fatal(err)
	}
	if err := DeleteAiAgent(b.AgentID); err != nil {
		t.Fatal(err)
	}
}

func TestAiBotCancellationDoesNotWaitForAnotherConversationLock(t *testing.T) {
	b := setupBotService(t)
	in := AiBotIncoming{MessageID: "cancel", ConversationID: "single:", SenderID: "alice", Text: "hello"}
	d, _, err := ClaimAiBotMessage(&b, in)
	if err != nil {
		t.Fatal(err)
	}
	lock := botConversationLock(botConversationKey(b, in))
	lock <- struct{}{}
	defer func() { <-lock }()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	if !startAiBotMessage(ctx, b, *d, in, nil, nil, func() { close(done) }) {
		t.Fatal("not admitted")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("canceled robot is waiting on another conversation")
	}
}

func TestWeComBotIDChangeSeparatesHistoryAndDuplicateReceipts(t *testing.T) {
	b := setupBotService(t)
	b.Platform, b.BotID = "wecom", "enterprise-a-bot"
	in := AiBotIncoming{MessageID: "same-message-id", ConversationID: "single:", SenderID: "alice", Text: "hello"}
	first, fresh, err := ClaimAiBotMessage(&b, in)
	if err != nil || !fresh {
		t.Fatalf("first receipt: %v %v", fresh, err)
	}
	firstSession, err := botSession(b, first.ConversationKey)
	if err != nil {
		t.Fatal(err)
	}
	if err := SaveAiAssistantMessage(firstSession, "private enterprise A history"); err != nil {
		t.Fatal(err)
	}
	b.Secret = "rotated-secret"
	if botConversationKey(b, in) != first.ConversationKey {
		t.Fatal("secret rotation discarded same-bot history")
	}
	b.BotID = "enterprise-b-bot"
	second, fresh, err := ClaimAiBotMessage(&b, in)
	if err != nil || !fresh || first.ID == second.ID {
		t.Fatalf("new external bot reused old receipt: %v %v", fresh, err)
	}
	secondSession, err := botSession(b, second.ConversationKey)
	if err != nil || firstSession == secondSession {
		t.Fatalf("new external bot reused old history: %v", err)
	}
	messages, err := ListAiMessages(secondSession)
	if err != nil || len(messages) != 0 {
		t.Fatalf("new bot sees old conversation: %v", err)
	}
}
