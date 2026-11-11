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
	"fde-admin/internal/service"
	"fde-admin/internal/utils"
)

func TestP2APIRequiresActorProjectPermissionAndExplicitConfirmation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "p2-api.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	oldDB, oldConfig, oldEnforcer := global.DB, global.Config, global.Enforcer
	t.Cleanup(func() {
		global.DB, global.Config, global.Enforcer = oldDB, oldConfig, oldEnforcer
		raw, _ := db.DB()
		_ = raw.Close()
	})
	cfg := &config.Config{App: config.App{Mode: "debug"}, JWT: config.JWT{Secret: "p2-api-test-signing-secret"}, Development: config.Development{TargetDir: t.TempDir()}}
	global.DB, global.Config = db, cfg
	if err := db.AutoMigrate(&model.User{}, &model.Role{}, &model.DevProject{}, &model.DevRun{}, &model.DevRunEvent{}, &model.DevRelease{}, &model.DevDeployment{}, &model.ActiveToken{}, &model.JwtBlacklist{}); err != nil {
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
	createActor := func(name string) (model.User, string) {
		actor := model.User{Username: name, Enabled: true}
		if err := db.Create(&actor).Error; err != nil {
			t.Fatal(err)
		}
		token, err := utils.GenerateToken(actor.ID, name, cfg.JWT.Secret, 1, "test")
		if err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&model.ActiveToken{UserID: actor.ID, TokenHash: utils.HashToken(token), ExpiresAt: time.Now().Add(time.Hour)}).Error; err != nil {
			t.Fatal(err)
		}
		if _, err := global.Enforcer.AddGroupingPolicy(fmt.Sprint(actor.ID), "workbench"); err != nil {
			t.Fatal(err)
		}
		return actor, token
	}
	owner, ownerToken := createActor("p2-owner")
	other, otherToken := createActor("p2-other")
	if _, err := global.Enforcer.AddPolicy("workbench", "/api/dev/*", "*"); err != nil {
		t.Fatal(err)
	}
	project := model.DevProject{Key: "p2-customer", Name: "Customer", OwnerID: owner.ID}
	if err := db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	foreignRun := model.DevRun{Key: "foreign-run", ActorID: other.ID, ProjectID: project.ID, State: "succeeded"}
	if err := db.Create(&foreignRun).Error; err != nil {
		t.Fatal(err)
	}
	foreignRelease := model.DevRelease{Key: "foreign-release", ActorID: other.ID, ProjectID: project.ID, RunID: foreignRun.ID, Status: "ready"}
	if err := db.Create(&foreignRelease).Error; err != nil {
		t.Fatal(err)
	}
	r := gin.New()
	g := r.Group("/api/dev", middleware.JWTAuth(), middleware.CasbinAuth())
	g.POST("/runs/:key/promote", PromoteDevRun)
	g.POST("/releases/:releaseId/upgrade", UpgradeDevelopmentRelease)
	g.POST("/releases/:releaseId/rollback", RollbackDevelopmentRelease)
	g.GET("/targets/:targetId", GetDevelopmentTarget)
	type envelope struct {
		Code int             `json:"code"`
		Data json.RawMessage `json:"data"`
	}
	request := func(method, path, token, body string, want int) envelope {
		t.Helper()
		req := httptest.NewRequest(method, fmt.Sprintf("/api/dev%s?projectId=%d", path, project.ID), strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		if token != "" {
			req.Header.Set("x-token", token)
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		var result envelope
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil || result.Code != want {
			t.Fatalf("%s %s wanted %d: HTTP %d %s", method, path, want, w.Code, w.Body.String())
		}
		return result
	}
	for _, path := range []string{"/runs/foreign-run/promote", "/releases/foreign-release/upgrade", "/releases/foreign-release/rollback"} {
		request(http.MethodPost, path, "", `{"approve":true,"targetId":"local-linux","expectedCurrentReleaseId":"v1"}`, utils.CodeNoAuth)
		request(http.MethodPost, path, otherToken, `{"approve":true,"targetId":"local-linux","expectedCurrentReleaseId":"v1"}`, utils.CodeForbidden)
		request(http.MethodPost, path, ownerToken, `{"approve":false,"targetId":"local-linux","expectedCurrentReleaseId":"v1"}`, utils.CodeError)
		request(http.MethodPost, path, ownerToken, `{}`, utils.CodeError)
		request(http.MethodPost, path, ownerToken, `{"approve":true,"targetId":"local-linux","expectedCurrentReleaseId":"v1"}`, utils.CodeError)
	}
	for _, path := range []string{"/releases/foreign-release/upgrade", "/releases/foreign-release/rollback"} {
		request(http.MethodPost, path, ownerToken, `{"approve":true,"targetId":"local-linux"}`, utils.CodeError)
	}
	request(http.MethodGet, "/targets/local-linux", "", "", utils.CodeNoAuth)
	request(http.MethodGet, "/targets/local-linux", otherToken, "", utils.CodeForbidden)
	status := request(http.MethodGet, "/targets/local-linux", ownerToken, "", utils.CodeSuccess)
	var target service.DevelopmentTargetStatus
	if err := json.Unmarshal(status.Data, &target); err != nil || target.TargetID != "local-linux" || target.Status != "empty" || target.CurrentReleaseID != "" {
		t.Fatalf("unexpected initial target: %s", status.Data)
	}
	if _, err := global.Enforcer.RemovePolicy("workbench", "/api/dev/*", "*"); err != nil {
		t.Fatal(err)
	}
	request(http.MethodPost, "/runs/foreign-run/promote", ownerToken, `{"approve":true}`, utils.CodeForbidden)
	var persisted model.DevRun
	if err := db.First(&persisted, foreignRun.ID).Error; err != nil || persisted.PromotedCommit != "" {
		t.Fatalf("unauthorized request changed another actor's run: %+v %v", persisted, err)
	}
}

func TestP2TransitionResponsesExposeOnlyDeploymentMetadata(t *testing.T) {
	dto := developmentTransitionDTO(&service.DevelopmentDeploymentResult{Status: "deployed", ReleaseID: "v2", TargetID: "local-linux", URL: "http://127.0.0.1:18080", Credentials: map[string]string{"password": "private-test-value"}})
	encoded, err := json.Marshal(dto)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "credentials") || strings.Contains(string(encoded), "password") || strings.Contains(string(encoded), "private-test-value") {
		t.Fatal("P2 transition exposed initialization credentials")
	}
	if dto["releaseId"] != "v2" || dto["targetId"] != "local-linux" {
		t.Fatal("deployment metadata lost")
	}
}
