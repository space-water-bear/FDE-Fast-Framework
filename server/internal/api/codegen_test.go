package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"fde-admin/internal/config"
	"fde-admin/internal/global"
	"fde-admin/internal/model"
	"fde-admin/internal/service"
	"fde-admin/internal/utils"
)

func TestCodegenHTTPRequiresReviewedBatch(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "api.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	global.DB, global.Logger, global.Enforcer = db, zap.NewNop(), nil
	if err := db.AutoMigrate(&model.User{}, &model.Role{}, &model.Menu{}, &model.DevProject{}, &model.BizObject{}, &model.BizField{}, &model.FormSchema{}, &model.GenAudit{}); err != nil {
		t.Fatal(err)
	}
	user := model.User{Username: "reviewer", Enabled: true}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	project := model.DevProject{Base: model.Base{ID: model.DefaultProjectID}, Key: "default", Name: "默认", OwnerID: user.ID}
	if err := db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.Role{Name: "管理员", Code: "admin", Enabled: true}).Error; err != nil {
		t.Fatal(err)
	}
	tmp := t.TempDir()
	global.Config = &config.Config{Codegen: config.Codegen{ServerDir: filepath.Join(tmp, "server"), WebDir: filepath.Join(tmp, "web")}}
	for _, dir := range []string{"server/internal", "web/src"} {
		if err := os.MkdirAll(filepath.Join(tmp, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	router := gin.New()
	router.Use(func(c *gin.Context) {
		if c.GetHeader("X-Test-Login") == "yes" {
			c.Set("claims", &utils.Claims{UserID: user.ID})
		}
		c.Next()
	})
	router.POST("/preview", CodegenPreview)
	router.POST("/generate", CodegenGenerate)
	request := func(path, body string, login bool) map[string]json.RawMessage {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		if login {
			req.Header.Set("X-Test-Login", "yes")
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		var envelope map[string]json.RawMessage
		if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
			t.Fatalf("invalid response: %s", w.Body.String())
		}
		return envelope
	}
	code := func(envelope map[string]json.RawMessage) int {
		var n int
		if err := json.Unmarshal(envelope["code"], &n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	spec := `{"tableName":"biz_http_ticket","business":"登记","fields":[{"name":"number","title":"编号","type":"string","inForm":true,"inList":true}]}`
	if code(request("/generate", spec, true)) == 0 {
		t.Fatal("legacy one-step request wrote source")
	}
	if code(request("/preview", spec, false)) == 0 {
		t.Fatal("anonymous preview accepted")
	}
	preview := request("/preview", spec, true)
	if code(preview) != 0 {
		t.Fatalf("preview rejected: %s", preview["msg"])
	}
	var dry service.BusinessCodeResult
	if err := json.Unmarshal(preview["data"], &dry); err != nil {
		t.Fatal(err)
	}
	if !dry.OK || dry.BatchID == "" || len(dry.Files) != 6 {
		t.Fatalf("incomplete preview: %+v", dry)
	}
	if code(request("/generate", fmt.Sprintf(`{"batchId":%q}`, dry.BatchID), true)) == 0 {
		t.Fatal("batch applied without explicit confirmation")
	}
	if code(request("/generate", fmt.Sprintf(`{"batchId":%q,"approve":true}`, dry.BatchID), true)) != 0 {
		t.Fatal("reviewed batch apply failed")
	}
	if code(request("/generate", fmt.Sprintf(`{"batchId":%q,"approve":true}`, dry.BatchID), true)) == 0 {
		t.Fatal("batch applied twice")
	}
	if _, err := os.Stat(filepath.Join(tmp, "server/internal/model/http_ticket.go")); err != nil {
		t.Fatalf("reviewed source missing: %v", err)
	}
}
