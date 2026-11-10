package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/casbin/casbin/v2"
	casbinmodel "github.com/casbin/casbin/v2/model"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"fde-admin/internal/config"
	"fde-admin/internal/global"
	"fde-admin/internal/initialize"
	"fde-admin/internal/middleware"
	"fde-admin/internal/model"
	"fde-admin/internal/utils"
)

func TestAiBotHistoryRequiresAdminAndCannotExecuteWebTools(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "history.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	oldDB, oldEnforcer, oldConfig := global.DB, global.Enforcer, global.Config
	t.Cleanup(func() {
		global.DB, global.Enforcer, global.Config = oldDB, oldEnforcer, oldConfig
		raw, _ := db.DB()
		_ = raw.Close()
	})
	cfg := &config.Config{JWT: config.JWT{Secret: "local-test-signing-key-for-bot-history"}}
	global.DB, global.Config = db, cfg
	if err := db.AutoMigrate(&model.User{}, &model.Role{}, &model.AiSession{}, &model.AiMessage{}, &model.AiBotConversation{}, &model.ActiveToken{}, &model.JwtBlacklist{}); err != nil {
		t.Fatal(err)
	}
	m, err := casbinmodel.NewModelFromString(initialize.CasbinModelText)
	if err != nil {
		t.Fatal(err)
	}
	global.Enforcer, err = casbin.NewEnforcer(m)
	if err != nil {
		t.Fatal(err)
	}
	adminRole := model.Role{Code: "admin", Name: "Admin", Enabled: true}
	if err := db.Create(&adminRole).Error; err != nil {
		t.Fatal(err)
	}
	createUser := func(name string, roles []model.Role) (model.User, string) {
		user := model.User{Username: name, Enabled: true, Roles: roles}
		if err := db.Create(&user).Error; err != nil {
			t.Fatal(err)
		}
		token, err := utils.GenerateToken(user.ID, "admin", cfg.JWT.Secret, 1, "test")
		if err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&model.ActiveToken{UserID: user.ID, TokenHash: utils.HashToken(token), ExpiresAt: time.Now().Add(time.Hour)}).Error; err != nil {
			t.Fatal(err)
		}
		if _, err := global.Enforcer.AddGroupingPolicy(fmt.Sprint(user.ID), "session-ui"); err != nil {
			t.Fatal(err)
		}
		return user, token
	}
	admin, adminToken := createUser("actual-admin", []model.Role{adminRole})
	user, userToken := createUser("ordinary-user", nil)
	if _, err := global.Enforcer.AddPolicy("session-ui", "/api/ai/session/*", "*"); err != nil {
		t.Fatal(err)
	}
	createSession := func(title string, userID, projectID uint) model.AiSession {
		session := model.AiSession{Title: title, UserID: userID, ProjectID: projectID}
		if err := db.Create(&session).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&model.AiMessage{SessionID: session.ID, Role: "user", Content: "fixture history"}).Error; err != nil {
			t.Fatal(err)
		}
		return session
	}
	own := createSession("admin private", admin.ID, 1)
	other := createSession("ordinary private", user.ID, 1)
	linked := createSession("bot attached", 0, 1)
	orphan := createSession("renamed legacy history", 0, 1)
	if err := db.Model(&orphan).Update("project_id", 0).Error; err != nil {
		t.Fatal(err)
	}
	foreign := createSession("other project ownerless", 0, 2)
	if err := db.Create(&model.AiBotConversation{BotID: 91, ConversationKey: "fixture-conversation", SessionID: linked.ID}).Error; err != nil {
		t.Fatal(err)
	}
	r := gin.New()
	g := r.Group("/api/ai/session", middleware.JWTAuth(), middleware.CasbinAuth())
	g.GET("/list", ListAiSessions)
	g.GET("/:id/messages", ListAiMessages)
	g.DELETE("/:id", DeleteAiSession)
	g.POST("/:id/chat", ChatAi)
	g.POST("/:id/chat/stream", StreamChatAi)
	g.POST("/:id/regenerate", RegenerateAiSession)
	g.POST("/:id/messages/:mid/resend", ResendAiMessage)
	g.PUT("/:id", RenameAiSession)
	g.DELETE("/:id/messages", ClearAiSession)
	type response struct {
		Code int             `json:"code"`
		Data json.RawMessage `json:"data"`
	}
	request := func(method, path, token string, want int) response {
		t.Helper()
		req := httptest.NewRequest(method, "/api/ai/session"+path, strings.NewReader(`{"content":"test","title":"test"}`))
		req.Header.Set("Content-Type", "application/json")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		var out response
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil || out.Code != want {
			t.Fatalf("%s %s wanted %d: HTTP %d %s", method, path, want, w.Code, w.Body.String())
		}
		return out
	}
	request(http.MethodGet, "/list", "", utils.CodeNoAuth)
	assertList := func(token string, want map[uint]bool) {
		t.Helper()
		out := request(http.MethodGet, "/list", token, utils.CodeSuccess)
		var data struct {
			List []struct {
				ID        uint `json:"id"`
				CanDelete bool `json:"canDelete"`
			} `json:"list"`
			Total int `json:"total"`
		}
		if err := json.Unmarshal(out.Data, &data); err != nil {
			t.Fatal(err)
		}
		if len(data.List) != len(want) || data.Total != len(want) {
			t.Fatalf("unexpected list scope: %s", out.Data)
		}
		for _, item := range data.List {
			allowed, ok := want[item.ID]
			if !ok || allowed != item.CanDelete {
				t.Fatalf("unexpected session or delete permission: %+v", item)
			}
		}
	}
	assertList(adminToken, map[uint]bool{own.ID: true, linked.ID: false, orphan.ID: true})
	assertList(userToken, map[uint]bool{other.ID: true})
	path := func(session model.AiSession, suffix string) string { return fmt.Sprintf("/%d%s", session.ID, suffix) }
	for _, session := range []model.AiSession{linked, orphan} {
		history := request(http.MethodGet, path(session, "/messages"), adminToken, utils.CodeSuccess)
		var data struct {
			List []model.AiMessage `json:"list"`
		}
		if err := json.Unmarshal(history.Data, &data); err != nil || len(data.List) != 1 || data.List[0].SessionID != session.ID || data.List[0].Content != "fixture history" {
			t.Fatalf("wrong robot message history: %s", history.Data)
		}
		request(http.MethodGet, path(session, "/messages"), userToken, utils.CodeForbidden)
		for _, suffix := range []string{"/chat", "/chat/stream", "/regenerate", "/messages/1/resend"} {
			request(http.MethodPost, path(session, suffix), adminToken, utils.CodeForbidden)
		}
		request(http.MethodPut, path(session, ""), adminToken, utils.CodeForbidden)
		request(http.MethodDelete, path(session, "/messages"), adminToken, utils.CodeForbidden)
	}
	request(http.MethodGet, path(other, "/messages"), adminToken, utils.CodeForbidden)
	request(http.MethodGet, path(foreign, "/messages"), adminToken, utils.CodeForbidden)
	request(http.MethodGet, path(linked, "/messages?projectId=2"), adminToken, utils.CodeForbidden)
	request(http.MethodGet, path(other, "/messages"), userToken, utils.CodeSuccess)
	request(http.MethodDelete, path(linked, ""), adminToken, utils.CodeForbidden)
	request(http.MethodDelete, path(orphan, ""), userToken, utils.CodeForbidden)
	request(http.MethodDelete, path(orphan, ""), adminToken, utils.CodeSuccess)
	var messages int64
	if err := db.Model(&model.AiMessage{}).Where("session_id = ?", orphan.ID).Count(&messages).Error; err != nil || messages != 0 {
		t.Fatalf("deleted history left %d messages: %v", messages, err)
	}
	request(http.MethodGet, path(orphan, "/messages"), adminToken, utils.CodeForbidden)
	request(http.MethodDelete, path(other, ""), adminToken, utils.CodeForbidden)
	request(http.MethodDelete, path(other, ""), userToken, utils.CodeSuccess)
	cfg.App.Mode = "release"
	request(http.MethodGet, path(linked, "/messages"), adminToken, utils.CodeForbidden)
	assertList(adminToken, map[uint]bool{own.ID: true})
	cfg.App.Mode = "debug"
	if err := db.Model(&adminRole).Update("enabled", false).Error; err != nil {
		t.Fatal(err)
	}
	request(http.MethodGet, path(linked, "/messages"), adminToken, utils.CodeForbidden)
	assertList(adminToken, map[uint]bool{own.ID: true})
}
