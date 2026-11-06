package api

import (
	"net/http/httptest"
	"strings"
	"testing"

	"fde-admin/internal/global"
	"fde-admin/internal/model"
	"fde-admin/internal/utils"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestSessionOwnershipEnforcedBeforeMutation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err = db.AutoMigrate(&model.AiSession{}, &model.AiMessage{}); err != nil {
		t.Fatal(err)
	}
	previous := global.DB
	global.DB = db
	t.Cleanup(func() { global.DB = previous; raw, _ := db.DB(); _ = raw.Close() })
	session := model.AiSession{Title: "private", UserID: 7}
	if err = db.Create(&session).Error; err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct {
		path    string
		handler gin.HandlerFunc
		body    string
	}{
		{"/clear", ClearAiSession, `{}`}, {"/chat", ChatAi, `{"content":"write"}`}, {"/stream", StreamChatAi, `{"content":"write"}`}, {"/rename", RenameAiSession, `{"title":"changed"}`}, {"/messages", ListAiMessages, `{}`},
	} {
		r := gin.New()
		r.Use(func(c *gin.Context) { c.Set("claims", &utils.Claims{UserID: 8}) })
		r.POST("/:id"+item.path, item.handler)
		w := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/1"+item.path, strings.NewReader(item.body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		if w.Code != 403 {
			t.Fatalf("%s did not reject foreign session: %d %s", item.path, w.Code, w.Body.String())
		}
	}
	var got model.AiSession
	if err = db.First(&got, session.ID).Error; err != nil || got.Title != "private" {
		t.Fatalf("foreign mutation: %+v %v", got, err)
	}
	var messages int64
	db.Model(&model.AiMessage{}).Count(&messages)
	if messages != 0 {
		t.Fatal("rejected chat still persisted message")
	}
}

func TestServiceKeyCannotApplyCodegenBatch(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/api/mcp/formgen/apply", strings.NewReader(`{"batchId":"a","approve":true}`))
	c.Request.Header.Set("x-api-key", "service-only-key")
	FormgenApply(c)
	if c.Writer.Status() != 401 {
		t.Fatalf("service identity was promoted to user identity: %d", c.Writer.Status())
	}
}

func TestBoundProjectSessionRechecksAccessWithoutHeader(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err = db.AutoMigrate(&model.AiSession{}, &model.AiMessage{}, &model.DevProject{}, &model.User{}, &model.Role{}); err != nil {
		t.Fatal(err)
	}
	previous := global.DB
	global.DB = db
	t.Cleanup(func() { global.DB = previous; raw, _ := db.DB(); _ = raw.Close() })
	user := model.User{Base: model.Base{ID: 7}, Username: "previous-owner", Enabled: true}
	project := model.DevProject{Base: model.Base{ID: 2}, Key: "transferred-project", Name: "客户项目", OwnerID: 8}
	session := model.AiSession{UserID: 7, ProjectID: 2, Title: "old-project-conversation"}
	for _, value := range []any{&user, &project, &session} {
		if err = db.Create(value).Error; err != nil {
			t.Fatal(err)
		}
	}
	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set("claims", &utils.Claims{UserID: 7}) })
	r.GET("/:id/messages", ListAiMessages)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/1/messages", nil))
	if w.Code != 403 {
		t.Fatalf("omitted project header bypassed project authorization: %d %s", w.Code, w.Body.String())
	}
}
