package middleware

import (
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"fde-admin/internal/global"
	"fde-admin/internal/model"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestOperationLogPreservesRequestBeyondSampleLimit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.OperationLog{}); err != nil {
		t.Fatal(err)
	}
	previous := global.DB
	global.DB = db
	t.Cleanup(func() { global.DB = previous; raw, _ := db.DB(); _ = raw.Close() })
	r := gin.New()
	r.Use(OperationLog())
	r.POST("/api/formgen/schema/preview", func(c *gin.Context) {
		body, err := io.ReadAll(c.Request.Body)
		if err != nil {
			t.Error(err)
		}
		c.String(200, string(body))
	})
	body := strings.Repeat("x", maxLogBody+2048)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("POST", "/api/formgen/schema/preview", strings.NewReader(body)))
	if w.Body.String() != body {
		t.Fatalf("request body truncated: %d != %d", w.Body.Len(), len(body))
	}
}
