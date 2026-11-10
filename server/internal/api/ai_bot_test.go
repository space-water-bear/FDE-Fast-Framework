package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"go.uber.org/zap"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"fde-admin/internal/global"
	"fde-admin/internal/model"
)

var botTestLoggerOnce sync.Once

func setupBotAPITest(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	global.DB = db
	botTestLoggerOnce.Do(func() { global.Logger = zap.NewNop() })
	if err := db.AutoMigrate(
		&model.AiBot{}, &model.AiBotConversation{}, &model.AiBotDelivery{},
		&model.AiProvider{}, &model.AiAgent{}, &model.AiSkill{}, &model.AiTool{},
		&model.AiMcpServer{}, &model.AiSession{}, &model.AiMessage{}, &model.AiUsage{},
	); err != nil {
		t.Fatal(err)
	}
	r := gin.New()
	r.GET("/bot/list", ListAiBots)
	r.POST("/bot", SaveAiBot)
	r.PUT("/bot/:id", SaveAiBot)
	r.GET("/callback/:id", AiBotCallback)
	r.POST("/callback/:id", AiBotCallback)
	return r
}

func botJSONRequest(t *testing.T, r http.Handler, method, path string, data any) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func seedBotAgent(t *testing.T, gatewayURL string) model.AiAgent {
	t.Helper()
	p := model.AiProvider{Name: "test-provider", BaseURL: gatewayURL + "/v1", APIKey: "test-key", APIMode: "chat", Model: "test-model", Enabled: true}
	if err := global.DB.Create(&p).Error; err != nil {
		t.Fatal(err)
	}
	a := model.AiAgent{Name: "test-agent", ProviderID: p.ID, Enabled: true}
	if err := global.DB.Create(&a).Error; err != nil {
		t.Fatal(err)
	}
	return a
}

func TestAiBotManagementCredentialsAreWriteOnly(t *testing.T) {
	r := setupBotAPITest(t)
	a := seedBotAgent(t, "http://unused.invalid")
	w := botJSONRequest(t, r, http.MethodPost, "/bot", gin.H{
		"name": "Ding", "platform": "dingtalk", "agentId": a.ID, "clientId": "client-one", "clientSecret": "secret-not-in-response",
	})
	var response struct {
		Code int `json:"code"`
		Data struct {
			ID              uint   `json:"id"`
			Enabled         bool   `json:"enabled"`
			HasClientSecret bool   `json:"hasClientSecret"`
			CallbackPath    string `json:"callbackPath"`
			ClientID        string `json:"clientId"`
			Connection      struct {
				State string `json:"state"`
			} `json:"connection"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Code != 0 || response.Data.ID == 0 || response.Data.Enabled || !response.Data.HasClientSecret || response.Data.CallbackPath != "" || response.Data.ClientID != "client-one" || response.Data.Connection.State != "disabled" {
		t.Fatalf("unexpected response: %s", w.Body.String())
	}
	if strings.Contains(w.Body.String(), "secret-not-in-response") || strings.Contains(w.Body.String(), `"clientSecret"`) {
		t.Fatal("credential leaked in create response")
	}
	w = botJSONRequest(t, r, http.MethodPut, fmt.Sprintf("/bot/%d", response.Data.ID), gin.H{
		"name": "Renamed", "platform": "dingtalk", "agentId": a.ID, "enabled": true, "clientId": "client-one", "clientSecret": "",
	})
	if strings.Contains(w.Body.String(), `"code":40001`) {
		t.Fatalf("update failed: %s", w.Body.String())
	}
	updated := parseBotManagementResponse(t, w)
	if updated.Data.CallbackPath != "" || updated.Data.Connection.State != "disconnected" || !updated.Data.HasClientSecret {
		t.Fatalf("unexpected DingTalk connection state: %s", w.Body.String())
	}
	var stored model.AiBot
	if err := global.DB.First(&stored, response.Data.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.ClientSecret != "secret-not-in-response" || stored.ClientID != "client-one" || !stored.Enabled {
		t.Fatalf("blank update did not preserve credential or enable: %+v", stored)
	}
	w = botJSONRequest(t, r, http.MethodGet, "/bot/list", nil)
	if strings.Contains(w.Body.String(), "secret-not-in-response") || strings.Contains(w.Body.String(), `"clientSecret"`) {
		t.Fatal("credential leaked in list")
	}
	w = botJSONRequest(t, r, http.MethodPost, "/bot", gin.H{"name": "Later", "platform": "feishu", "agentId": a.ID})
	if !strings.Contains(w.Body.String(), `"code":40001`) {
		t.Fatalf("unimplemented platform accepted: %s", w.Body.String())
	}
}

type botManagementResponse struct {
	Code int `json:"code"`
	Data struct {
		ID              uint   `json:"id"`
		BotID           string `json:"botId"`
		ClientID        string `json:"clientId"`
		HasSecret       bool   `json:"hasSecret"`
		HasClientSecret bool   `json:"hasClientSecret"`
		CallbackPath    string `json:"callbackPath"`
		Connection      struct {
			State string `json:"state"`
		} `json:"connection"`
	} `json:"data"`
}

func parseBotManagementResponse(t *testing.T, w *httptest.ResponseRecorder) botManagementResponse {
	t.Helper()
	var result botManagementResponse
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("invalid management response: %s", w.Body.String())
	}
	return result
}

func TestWeComManagementUsesBotIDAndWriteOnlySecret(t *testing.T) {
	r := setupBotAPITest(t)
	a := seedBotAgent(t, "http://unused.invalid")
	for _, fields := range []gin.H{
		{"botId": "", "secret": "secret-one"},
		{"botId": "bot-one", "secret": ""},
		{"botId": strings.Repeat("b", 129), "secret": "secret-one"},
		{"botId": "bot-one", "secret": strings.Repeat("s", 256)},
	} {
		fields["name"], fields["platform"], fields["agentId"] = "WeCom", "wecom", a.ID
		w := botJSONRequest(t, r, http.MethodPost, "/bot", fields)
		if result := parseBotManagementResponse(t, w); result.Code != 40001 {
			t.Fatalf("invalid credentials accepted: %s", w.Body.String())
		}
	}
	w := botJSONRequest(t, r, http.MethodPost, "/bot", gin.H{
		"name": "WeCom", "platform": "wecom", "agentId": a.ID, "botId": "bot-one", "secret": "wecom-write-only-secret-one",
	})
	created := parseBotManagementResponse(t, w)
	if created.Code != 0 || created.Data.ID == 0 || created.Data.BotID != "bot-one" || !created.Data.HasSecret || created.Data.CallbackPath != "" || created.Data.Connection.State != "disabled" {
		t.Fatalf("unexpected WeCom create response: %s", w.Body.String())
	}
	if strings.Contains(w.Body.String(), "wecom-write-only-secret-one") || strings.Contains(w.Body.String(), `"secret"`) {
		t.Fatal("WeCom secret leaked in response")
	}
	path := fmt.Sprintf("/bot/%d", created.Data.ID)
	w = botJSONRequest(t, r, http.MethodPut, path, gin.H{
		"name": "WeCom renamed", "platform": "wecom", "agentId": a.ID, "botId": "bot-one", "secret": "", "enabled": true,
	})
	updated := parseBotManagementResponse(t, w)
	if updated.Code != 0 || !updated.Data.HasSecret || updated.Data.Connection.State != "disconnected" {
		t.Fatalf("unexpected WeCom update: %s", w.Body.String())
	}
	var stored model.AiBot
	if err := global.DB.First(&stored, created.Data.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Secret != "wecom-write-only-secret-one" || stored.BotID != "bot-one" || !stored.Enabled {
		t.Fatalf("blank secret update lost stored configuration: %+v", stored)
	}
	w = botJSONRequest(t, r, http.MethodPut, path, gin.H{
		"name": "WeCom renamed", "platform": "wecom", "agentId": a.ID, "botId": "bot-two", "secret": "", "enabled": true,
	})
	if result := parseBotManagementResponse(t, w); result.Code != 40001 {
		t.Fatalf("Bot ID changed without a new secret: %s", w.Body.String())
	}
	if err := global.DB.First(&stored, created.Data.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.BotID != "bot-one" || stored.Secret != "wecom-write-only-secret-one" {
		t.Fatal("failed edit changed persisted credentials")
	}
	w = botJSONRequest(t, r, http.MethodPost, "/bot", gin.H{
		"name": "Duplicate", "platform": "wecom", "agentId": a.ID, "botId": "bot-one", "secret": "another-secret",
	})
	if result := parseBotManagementResponse(t, w); result.Code != 40001 {
		t.Fatalf("duplicate Bot ID accepted: %s", w.Body.String())
	}
	w = botJSONRequest(t, r, http.MethodPut, path, gin.H{
		"name": "WeCom renamed", "platform": "wecom", "agentId": a.ID, "botId": "bot-two", "secret": "wecom-write-only-secret-two", "enabled": true,
	})
	if result := parseBotManagementResponse(t, w); result.Code != 0 || result.Data.BotID != "bot-two" {
		t.Fatalf("credential rotation failed: %s", w.Body.String())
	}
	w = botJSONRequest(t, r, http.MethodGet, "/bot/list", nil)
	if strings.Contains(w.Body.String(), "wecom-write-only-secret") || strings.Contains(w.Body.String(), `"secret"`) {
		t.Fatal("WeCom secret leaked in list")
	}
	var list struct {
		Code int `json:"code"`
		Data struct {
			List []struct {
				CallbackPath string `json:"callbackPath"`
				HasSecret    bool   `json:"hasSecret"`
				Connection   struct {
					State string `json:"state"`
				} `json:"connection"`
			} `json:"list"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if list.Code != 0 || len(list.Data.List) != 1 || list.Data.List[0].CallbackPath != "" || !list.Data.List[0].HasSecret || list.Data.List[0].Connection.State != "disconnected" {
		t.Fatalf("unexpected list state: %s", w.Body.String())
	}
}

func TestDingTalkManagementRequiresClientIDAndNewSecretForRotation(t *testing.T) {
	r := setupBotAPITest(t)
	a := seedBotAgent(t, "http://unused.invalid")
	for _, fields := range []gin.H{
		{"clientId": "", "clientSecret": "secret-one"},
		{"clientId": "client-one", "clientSecret": ""},
		{"clientId": strings.Repeat("c", 129), "clientSecret": "secret-one"},
		{"clientId": "client-one", "clientSecret": strings.Repeat("s", 256)},
	} {
		fields["name"], fields["platform"], fields["agentId"] = "Ding", "dingtalk", a.ID
		w := botJSONRequest(t, r, http.MethodPost, "/bot", fields)
		if result := parseBotManagementResponse(t, w); result.Code != 40001 {
			t.Fatalf("invalid DingTalk credentials accepted: %s", w.Body.String())
		}
	}
	w := botJSONRequest(t, r, http.MethodPost, "/bot", gin.H{
		"name": "Ding", "platform": "dingtalk", "agentId": a.ID, "clientId": "client-one", "clientSecret": "dingtalk-write-only-secret-one",
	})
	created := parseBotManagementResponse(t, w)
	if created.Code != 0 || created.Data.ID == 0 || created.Data.ClientID != "client-one" || !created.Data.HasClientSecret || created.Data.CallbackPath != "" || created.Data.Connection.State != "disabled" {
		t.Fatalf("unexpected DingTalk create response: %s", w.Body.String())
	}
	w = botJSONRequest(t, r, http.MethodPost, "/bot", gin.H{
		"name": "Duplicate", "platform": "dingtalk", "agentId": a.ID, "clientId": "client-one", "clientSecret": "another-secret",
	})
	if result := parseBotManagementResponse(t, w); result.Code != 40001 {
		t.Fatalf("duplicate Client ID accepted: %s", w.Body.String())
	}
	path := fmt.Sprintf("/bot/%d", created.Data.ID)
	w = botJSONRequest(t, r, http.MethodPut, path, gin.H{
		"name": "Ding renamed", "platform": "dingtalk", "agentId": a.ID, "clientId": "client-two", "clientSecret": "", "enabled": true,
	})
	if result := parseBotManagementResponse(t, w); result.Code != 40001 {
		t.Fatalf("Client ID changed without a new secret: %s", w.Body.String())
	}
	var stored model.AiBot
	if err := global.DB.First(&stored, created.Data.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.ClientID != "client-one" || stored.ClientSecret != "dingtalk-write-only-secret-one" || stored.Enabled {
		t.Fatal("failed edit changed persisted DingTalk credentials or state")
	}
	w = botJSONRequest(t, r, http.MethodPut, path, gin.H{
		"name": "Ding renamed", "platform": "dingtalk", "agentId": a.ID, "clientId": "client-two", "clientSecret": "dingtalk-write-only-secret-two", "enabled": true,
	})
	updated := parseBotManagementResponse(t, w)
	if updated.Code != 0 || updated.Data.ClientID != "client-two" || !updated.Data.HasClientSecret || updated.Data.CallbackPath != "" || updated.Data.Connection.State != "disconnected" {
		t.Fatalf("DingTalk credential rotation failed: %s", w.Body.String())
	}
	if strings.Contains(w.Body.String(), "dingtalk-write-only-secret") || strings.Contains(w.Body.String(), `"clientSecret"`) {
		t.Fatal("DingTalk secret leaked in credential rotation response")
	}
	if err := global.DB.First(&stored, created.Data.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.ClientID != "client-two" || stored.ClientSecret != "dingtalk-write-only-secret-two" || !stored.Enabled {
		t.Fatal("successful rotation did not persist new credentials")
	}
}

func TestBotUnconfiguredStatesAndLegacyHTTPCallbacksUnavailable(t *testing.T) {
	r := setupBotAPITest(t)
	for _, platform := range []string{"wecom", "dingtalk"} {
		b := model.AiBot{Name: "Unconfigured " + platform, Platform: platform}
		if err := global.DB.Create(&b).Error; err != nil {
			t.Fatal(err)
		}
		w := botJSONRequest(t, r, http.MethodGet, "/bot/list", nil)
		var response struct {
			Data struct {
				List []struct {
					ID           uint   `json:"id"`
					CallbackPath string `json:"callbackPath"`
					Connection   struct {
						State string `json:"state"`
					} `json:"connection"`
				} `json:"list"`
			} `json:"data"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		found := false
		for _, item := range response.Data.List {
			if item.ID == b.ID {
				found = true
				if item.CallbackPath != "" || item.Connection.State != "unconfigured" {
					t.Fatalf("unexpected %s unconfigured state: %s", platform, w.Body.String())
				}
			}
		}
		if !found {
			t.Fatalf("missing %s in list", platform)
		}
		for _, enabled := range []bool{false, true} {
			if err := global.DB.Model(&b).Update("enabled", enabled).Error; err != nil {
				t.Fatal(err)
			}
			for _, method := range []string{http.MethodGet, http.MethodPost} {
				w := botJSONRequest(t, r, method, fmt.Sprintf("/callback/%d", b.ID), gin.H{"encrypt": "retired-http-message"})
				want := http.StatusNotFound
				if enabled {
					want = http.StatusGone
				}
				if w.Code != want {
					t.Fatalf("legacy %s callback enabled=%v method=%s status=%d, want %d", platform, enabled, method, w.Code, want)
				}
			}
		}
	}
	var deliveries int64
	if err := global.DB.Model(&model.AiBotDelivery{}).Count(&deliveries).Error; err != nil {
		t.Fatal(err)
	}
	if deliveries != 0 {
		t.Fatalf("retired HTTP callback accepted messages: %d", deliveries)
	}
}
