package service

import (
	"context"
	"errors"
	"sync"
	"time"

	"fde-admin/internal/model"
	"gorm.io/gorm"
)

var errWeComRateLimited = errors.New("WeCom conversation reply budget exhausted")

// We conservatively count every stream update because the official document
// gives no streaming exemption from 30/minute and 1000/hour per conversation.
type wecomReplyLimiter struct {
	mu        sync.Mutex
	db        *gorm.DB
	now       func() time.Time
	lastPrune time.Time
}

func newWeComReplyLimiter(db *gorm.DB) *wecomReplyLimiter {
	return &wecomReplyLimiter{db: db, now: time.Now}
}

func (l *wecomReplyLimiter) acquire(ctx context.Context, key string, wait bool) (*model.AiBotSendAttempt, error) {
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		l.mu.Lock()
		now := l.now().UTC()
		if l.lastPrune.IsZero() || now.Sub(l.lastPrune) >= time.Minute {
			if err := l.db.WithContext(ctx).Where("sent_at <= ?", now.Add(-time.Hour)).Delete(&model.AiBotSendAttempt{}).Error; err != nil {
				l.mu.Unlock()
				return nil, err
			}
			l.lastPrune = now
		}
		var attempts []model.AiBotSendAttempt
		err := l.db.WithContext(ctx).Where("channel_key = ? AND sent_at > ?", key, now.Add(-time.Hour)).Order("sent_at asc").Find(&attempts).Error
		if err != nil {
			l.mu.Unlock()
			return nil, err
		}
		minuteStart := len(attempts)
		for i, attempt := range attempts {
			if attempt.SentAt.After(now.Add(-time.Minute)) {
				minuteStart = i
				break
			}
		}
		var next time.Time
		if len(attempts)-minuteStart >= 30 {
			next = attempts[len(attempts)-30].SentAt.Add(time.Minute)
		}
		if len(attempts) >= 1000 {
			hourNext := attempts[len(attempts)-1000].SentAt.Add(time.Hour)
			if hourNext.After(next) {
				next = hourNext
			}
		}
		if next.IsZero() {
			// A canceled process may not record the final send time. Reserving
			// through the 10s outbound deadline keeps a crash conservative.
			attempt := &model.AiBotSendAttempt{ChannelKey: key, SentAt: now.Add(10 * time.Second)}
			err := l.db.WithContext(ctx).Create(attempt).Error
			l.mu.Unlock()
			return attempt, err
		}
		l.mu.Unlock()
		if !wait {
			return nil, errWeComRateLimited
		}
		timer := time.NewTimer(max(time.Millisecond, next.Sub(now)))
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}

func (l *wecomReplyLimiter) complete(attempt *model.AiBotSendAttempt) {
	// Count failed/uncertain attempts too. The server may already have accepted
	// the frame, and a missing ACK must never grant an extra send slot.
	l.mu.Lock()
	defer l.mu.Unlock()
	_ = l.db.Model(&model.AiBotSendAttempt{}).Where("id = ?", attempt.ID).Update("sent_at", l.now().UTC()).Error
}
