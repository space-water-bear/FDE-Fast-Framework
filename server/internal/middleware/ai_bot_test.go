package middleware

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"fde-admin/internal/global"
	"fde-admin/internal/model"
)

func TestAiBotOperationLogHidesCredentialsAndPreservesCallbacks(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	global.DB = db
	if err := db.AutoMigrate(&model.OperationLog{}); err != nil {
		t.Fatal(err)
	}
	r := gin.New()
	r.Use(OperationLog())
	var received string
	handler := func(c *gin.Context) {
		data, _ := io.ReadAll(c.Request.Body)
		received = string(data)
		c.JSON(http.StatusOK, gin.H{"code": 0})
	}
	r.POST("/api/ai/bot", handler)
	r.PUT("/api/ai/bot/:id", handler)
	r.POST("/api/ai/bot/callback/:id", handler)
	secret := `{"botId":"test-bot","secret":"must-not-log-wecom-secret","clientSecret":"must-not-log-dingtalk-secret"}`
	for _, request := range []struct{ method, path string }{
		{http.MethodPost, "/api/ai/bot"},
		{http.MethodPut, "/api/ai/bot/1"},
	} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(request.method, request.path, strings.NewReader(secret)))
		if received != secret {
			t.Fatal("management handler did not receive complete credential payload")
		}
	}
	var logs []model.OperationLog
	if err := db.Find(&logs).Error; err != nil {
		t.Fatal(err)
	}
	if len(logs) != 2 {
		t.Fatalf("expected management metadata logs, got %d", len(logs))
	}
	for _, log := range logs {
		if log.Body != "" {
			t.Fatalf("bot credential body logged: %q", log.Body)
		}
	}
	callbackBody := `{"text":{"content":"` + strings.Repeat("A", 9000) + `"}}`
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/ai/bot/callback/1?msg_signature=temporary-signature", strings.NewReader(callbackBody)))
	if received != callbackBody {
		t.Fatalf("callback body truncated: got %d, want %d", len(received), len(callbackBody))
	}
	var count int64
	db.Model(&model.OperationLog{}).Count(&count)
	if count != 2 {
		t.Fatalf("external callback query/body logged: rows=%d", count)
	}
}
