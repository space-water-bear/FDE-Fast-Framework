package service

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"fde-admin/internal/global"
	"fde-admin/internal/model"
)

func testWeComLimiter(t *testing.T) *wecomReplyLimiter {
	t.Helper()
	setupTestDB(t)
	if err := global.DB.AutoMigrate(&model.AiBotSendAttempt{}); err != nil {
		t.Fatal(err)
	}
	db, _ := global.DB.DB()
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	return newWeComReplyLimiter(global.DB)
}

func TestWeComQuotaSharesConversationAndSurvivesRestart(t *testing.T) {
	l := testWeComLimiter(t)
	b := model.AiBot{BotID: "bot-a"}
	a := wecomBotMessage{ChatType: "group", ChatID: "group-a"}
	a.From.UserID = "alice"
	bob := a
	bob.From.UserID = "bob"
	key := wecomChannelKey(b, a)
	if key != wecomChannelKey(b, bob) {
		t.Fatal("group members do not share the platform send budget")
	}
	for i := 0; i < 30; i++ {
		attempt, err := l.acquire(context.Background(), key, false)
		if err != nil {
			t.Fatal(err)
		}
		l.complete(attempt)
	}
	if _, err := l.acquire(context.Background(), wecomChannelKey(b, bob), false); !errors.Is(err, errWeComRateLimited) {
		t.Fatalf("31st frame admitted: %v", err)
	}
	restarted := newWeComReplyLimiter(global.DB)
	if _, err := restarted.acquire(context.Background(), key, false); !errors.Is(err, errWeComRateLimited) {
		t.Fatalf("restart reset quota: %v", err)
	}
	other := a
	other.ChatID = "group-b"
	if _, err := l.acquire(context.Background(), wecomChannelKey(b, other), false); err != nil {
		t.Fatalf("unrelated chat blocked: %v", err)
	}
	b.BotID = "bot-b"
	if _, err := l.acquire(context.Background(), wecomChannelKey(b, a), false); err != nil {
		t.Fatalf("different bot blocked: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := l.acquire(ctx, key, true); !errors.Is(err, context.Canceled) {
		t.Fatalf("wait ignored cancel: %v", err)
	}
}

func TestWeComQuotaEnforcesHourlyRollingWindow(t *testing.T) {
	l := testWeComLimiter(t)
	now := time.Now()
	l.now = func() time.Time { return now }
	for i := 0; i < 1000; i++ {
		if i > 0 && i%30 == 0 {
			now = now.Add(61 * time.Second)
		}
		attempt, err := l.acquire(context.Background(), "same-chat", false)
		if err != nil {
			t.Fatalf("frame %d: %v", i, err)
		}
		l.complete(attempt)
	}
	now = now.Add(61 * time.Second)
	if _, err := l.acquire(context.Background(), "same-chat", false); !errors.Is(err, errWeComRateLimited) {
		t.Fatalf("1001st hourly frame admitted: %v", err)
	}
	now = now.Add(time.Hour)
	if _, err := l.acquire(context.Background(), "same-chat", false); err != nil {
		t.Fatalf("expired window did not clear: %v", err)
	}
}

func TestWeComQuotaIsAtomicForConcurrentSenders(t *testing.T) {
	l := testWeComLimiter(t)
	var admitted atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := l.acquire(context.Background(), "shared", false); err == nil {
				admitted.Add(1)
			} else if !errors.Is(err, errWeComRateLimited) {
				t.Error(fmt.Errorf("quota storage failure: %w", err))
			}
		}()
	}
	wg.Wait()
	if admitted.Load() != 30 {
		t.Fatalf("concurrent accepted frames = %d, want 30", admitted.Load())
	}
}
