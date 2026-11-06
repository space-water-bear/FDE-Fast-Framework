package router

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"fde-admin/internal/config"
	"fde-admin/internal/global"
	"fde-admin/internal/model"
)

type envelope struct {
	Code int             `json:"code"`
	Msg  string          `json:"msg"`
	Data json.RawMessage `json:"data"`
}

func setupMcpRouter(t *testing.T, apiKey string) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)

	db, err := gorm.Open(sqlite.Open("file:"+strings.ReplaceAll(t.Name(), "/", "_")+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&model.OperationLog{}, &model.BizObject{}, &model.BizField{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	global.DB = db
	global.Logger = zap.NewNop()
	global.Config = &config.Config{
		Upload: config.Upload{Dir: t.TempDir()},
		MCP:    config.MCP{APIKey: apiKey},
	}
	return NewRouter()
}

func postMcp(t *testing.T, r *gin.Engine, path, apiKey, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		req.Header.Set("x-api-key", apiKey)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestMcpFormgenSchemaPreviewValidKey(t *testing.T) {
	r := setupMcpRouter(t, "test-key-123")
	w := postMcp(t, r, "/api/mcp/formgen/schema/preview", "test-key-123",
		`{"objectKey":"biz_mcp_preview","name":"预览对象","fields":[{"name":"name","title":"名称","type":"string","required":true}]}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	var env envelope
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode envelope: %v, body=%s", err, w.Body.String())
	}
	if env.Code != 0 {
		t.Fatalf("code = %d, want 0, body=%s", env.Code, w.Body.String())
	}
	var data struct {
		OK bool `json:"ok"`
	}
	if err := json.Unmarshal(env.Data, &data); err != nil {
		t.Fatalf("decode data: %v, body=%s", err, w.Body.String())
	}
	if !data.OK {
		t.Fatalf("data.ok = false, body=%s", w.Body.String())
	}
}

func TestMcpFormgenSchemaPreviewMissingKey(t *testing.T) {
	r := setupMcpRouter(t, "test-key-123")
	w := postMcp(t, r, "/api/mcp/formgen/schema/preview", "",
		`{"objectKey":"biz_mcp_preview","name":"预览对象","fields":[{"name":"name","title":"名称","type":"string"}]}`)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401, body = %s", w.Code, w.Body.String())
	}
	var env envelope
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if env.Code != 40004 {
		t.Fatalf("code = %d, want 40004, body=%s", env.Code, w.Body.String())
	}
}

func TestMcpFormgenSchemaPreviewWrongKey(t *testing.T) {
	r := setupMcpRouter(t, "test-key-123")
	w := postMcp(t, r, "/api/mcp/formgen/schema/preview", "wrong-key",
		`{"objectKey":"biz_mcp_preview","name":"预览对象","fields":[{"name":"name","title":"名称","type":"string"}]}`)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401, body = %s", w.Code, w.Body.String())
	}
	var env envelope
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if env.Code != 40004 {
		t.Fatalf("code = %d, want 40004, body=%s", env.Code, w.Body.String())
	}
}

func TestMcpFormgenSchemaPreviewEmptyConfigKey(t *testing.T) {
	r := setupMcpRouter(t, "")
	w := postMcp(t, r, "/api/mcp/formgen/schema/preview", "any-key",
		`{"objectKey":"biz_mcp_preview","name":"预览对象","fields":[{"name":"name","title":"名称","type":"string"}]}`)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401, body = %s", w.Code, w.Body.String())
	}
	var env envelope
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if env.Code != 40004 {
		t.Fatalf("code = %d, want 40004, body=%s", env.Code, w.Body.String())
	}
}
