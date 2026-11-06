package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/microsoft/agent-framework-go/tool/mcptool"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

const testAPIKey = "test-api-key"
const testAuthToken = "test-auth-token"

func newTestGateway(t *testing.T, upstream string) *Gateway {
	t.Helper()
	return NewGateway(Config{Addr: ":0", Upstream: upstream, APIKey: testAPIKey, AuthToken: testAuthToken})
}

func startGateway(t *testing.T, gw *Gateway) *httptest.Server {
	t.Helper()
	ts := httptest.NewServer(gw.Handler())
	t.Cleanup(ts.Close)
	return ts
}

func connectGateway(t *testing.T, ts *httptest.Server) *mcpsdk.ClientSession {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	httpClient := &http.Client{Transport: &headerTransport{base: http.DefaultTransport, name: "Authorization", value: "Bearer " + testAuthToken}}
	transport := &mcpsdk.StreamableClientTransport{Endpoint: ts.URL, HTTPClient: httpClient}
	session, err := mcptool.Connect(ctx, transport)
	if err != nil {
		t.Fatalf("connect mcp: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session
}

type headerTransport struct {
	base  http.RoundTripper
	name  string
	value string
}

func (t *headerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r2 := r.Clone(r.Context())
	r2.Header.Set(t.name, t.value)
	return t.base.RoundTrip(r2)
}

func connectGatewayWithHeader(t *testing.T, ts *httptest.Server, name, value string) *mcpsdk.ClientSession {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	httpClient := &http.Client{Transport: &headerTransport{base: http.DefaultTransport, name: name, value: value}}
	transport := &mcpsdk.StreamableClientTransport{Endpoint: ts.URL, HTTPClient: httpClient}
	session, err := mcptool.Connect(ctx, transport)
	if err != nil {
		t.Fatalf("connect mcp: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session
}

func TestMCPAuthRejected(t *testing.T) {
	gw := newTestGateway(t, "http://127.0.0.1:1")
	ts := startGateway(t, gw)
	cases := []struct{ name, key, value string }{
		{"missing", "", ""},
		{"wrong bearer", "Authorization", "Bearer wrong"},
		{"wrong x-api-key", "x-api-key", "wrong"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req, _ := http.NewRequest(http.MethodPost, ts.URL, strings.NewReader("{}"))
			if tc.key != "" {
				req.Header.Set(tc.key, tc.value)
			}
			resp, err := ts.Client().Do(req)
			if err != nil {
				t.Fatalf("do: %v", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401", resp.StatusCode)
			}
		})
	}
}

func TestMCPAuthXApiKeyAccepted(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp, _ := json.Marshal(map[string]any{"code": 0, "msg": "ok", "data": json.RawMessage(`{"ok":true}`)})
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(resp)
	}))
	defer upstream.Close()
	gw := newTestGateway(t, upstream.URL)
	ts := startGateway(t, gw)
	session := connectGatewayWithHeader(t, ts, "x-api-key", testAuthToken)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	res, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("tools/list with x-api-key: %v", err)
	}
	if len(res.Tools) != 3 {
		t.Fatalf("tools count = %d, want 3", len(res.Tools))
	}
}

func TestForwardPostsPathBodyAndAPIKey(t *testing.T) {
	var gotPath, gotKey string
	var gotBody map[string]any
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotKey = r.Header.Get("x-api-key")
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &gotBody)
		data, _ := json.Marshal(map[string]any{"ok": true, "schema": map[string]any{"objectKey": "biz_a"}})
		resp, _ := json.Marshal(map[string]any{"code": 0, "msg": "ok", "data": json.RawMessage(data)})
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(resp)
	}))
	defer upstream.Close()

	gw := newTestGateway(t, upstream.URL)
	reqBody, _ := json.Marshal(map[string]any{"objectKey": "biz_a", "dryRun": true})
	data, err := gw.forward(context.Background(), "/api/mcp/formgen/schema/preview", reqBody)
	if err != nil {
		t.Fatalf("forward: %v", err)
	}
	if gotPath != "/api/mcp/formgen/schema/preview" {
		t.Fatalf("path = %q, want /api/mcp/formgen/schema/preview", gotPath)
	}
	if gotKey != testAPIKey {
		t.Fatalf("x-api-key = %q, want %q", gotKey, testAPIKey)
	}
	if gotBody["objectKey"] != "biz_a" || gotBody["dryRun"] != true {
		t.Fatalf("body = %v", gotBody)
	}
	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("data not json: %s", string(data))
	}
	if result["ok"] != true || result["schema"].(map[string]any)["objectKey"] != "biz_a" {
		t.Fatalf("data = %v", result)
	}
}

func TestMCPListToolsAndCallGenerateFormSchema(t *testing.T) {
	var gotBody map[string]any
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/mcp/formgen/schema/preview" {
			t.Errorf("path = %q, want /api/mcp/formgen/schema/preview", r.URL.Path)
		}
		if r.Header.Get("x-api-key") != testAPIKey {
			t.Errorf("x-api-key = %q, want %q", r.Header.Get("x-api-key"), testAPIKey)
		}
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &gotBody)
		if gotBody["objectKey"] != "biz_mcp_gw" {
			t.Errorf("body = %s, want objectKey biz_mcp_gw", string(b))
		}
		data, _ := json.Marshal(map[string]any{"ok": true, "schema": map[string]any{"objectKey": "biz_mcp_gw"}})
		resp, _ := json.Marshal(map[string]any{"code": 0, "msg": "ok", "data": json.RawMessage(data)})
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(resp)
	}))
	defer upstream.Close()

	gw := newTestGateway(t, upstream.URL)
	session := connectGateway(t, startGateway(t, gw))

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	res, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("tools/list: %v", err)
	}
	if len(res.Tools) != 3 {
		t.Fatalf("tools count = %d, want 3", len(res.Tools))
	}
	byName := map[string]*mcpsdk.Tool{}
	for _, tool := range res.Tools {
		byName[tool.Name] = tool
	}
	for _, name := range []string{"generate_form_schema", "generate_business_code", "apply_files"} {
		tool := byName[name]
		if tool == nil {
			t.Fatalf("tools/list missing %s", name)
		}
		if tool.Description == "" {
			t.Fatalf("tool %s missing description", name)
		}
		schema := tool.InputSchema.(map[string]any)
		if schema["type"] != "object" {
			t.Fatalf("tool %s schema type = %v", name, schema["type"])
		}
		switch name {
		case "generate_form_schema", "generate_business_code":
			if schema["$defs"] == nil {
				t.Fatalf("tool %s schema missing $defs", name)
			}
		case "apply_files":
			props := schema["properties"].(map[string]any)
			approve := props["approve"].(map[string]any)
			if approve["const"] != true {
				t.Fatalf("tool apply_files approve.const = %v", approve["const"])
			}
		}
	}

	// 关键字段与设计文档 6.1 / 6.2 / 6.3 保持一致。
	formgen := byName["generate_form_schema"].InputSchema.(map[string]any)
	if required := formgen["required"].([]any); len(required) != 3 {
		t.Fatalf("generate_form_schema required = %v, want 3 items", required)
	}
	if enum := schemaAt(t, formgen, "$defs", "fieldDef", "properties", "type", "enum"); !containsString(enum, "relation") {
		t.Fatalf("generate_form_schema fieldDef enum missing relation: %v", enum)
	}
	codegen := byName["generate_business_code"].InputSchema.(map[string]any)
	if enum := schemaAt(t, codegen, "$defs", "fieldDef", "properties", "type", "enum"); containsString(enum, "relation") {
		t.Fatalf("generate_business_code fieldDef enum should not contain relation: %v", enum)
	}
	apply := byName["apply_files"].InputSchema.(map[string]any)
	if schemaAt(t, apply, "properties", "approve", "const") != true {
		t.Fatalf("apply_files approve.const missing or not true")
	}

	callRes, err := session.CallTool(ctx, &mcpsdk.CallToolParams{
		Name: "generate_form_schema",
		Arguments: map[string]any{
			"objectKey": "biz_mcp_gw",
			"name":      "网关演示对象",
			"fields": []map[string]any{
				{"name": "name", "title": "名称", "type": "string", "required": true},
			},
			"dryRun": true,
		},
	})
	if err != nil {
		t.Fatalf("tools/call generate_form_schema: %v", err)
	}
	if callRes == nil || len(callRes.Content) == 0 {
		t.Fatalf("tools/call result missing content")
	}
	text := callRes.Content[0].(*mcpsdk.TextContent).Text
	var result map[string]any
	if err := json.Unmarshal([]byte(text), &result); err != nil {
		t.Fatalf("tools/call result not json: %s", text)
	}
	if result["ok"] != true || result["schema"].(map[string]any)["objectKey"] != "biz_mcp_gw" {
		t.Fatalf("tools/call result = %v", result)
	}
}

func TestMCPCallReturnsBusinessErrorText(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp, _ := json.Marshal(map[string]any{"code": 40001, "msg": "字段 name 与保留列冲突"})
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(resp)
	}))
	defer upstream.Close()

	gw := newTestGateway(t, upstream.URL)
	session := connectGateway(t, startGateway(t, gw))

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	res, err := session.CallTool(ctx, &mcpsdk.CallToolParams{
		Name:      "generate_form_schema",
		Arguments: map[string]any{"objectKey": "biz_err"},
	})
	if err != nil {
		t.Fatalf("tools/call: %v", err)
	}
	if res == nil || len(res.Content) == 0 {
		t.Fatalf("tools/call result missing content")
	}
	if !res.IsError {
		t.Fatalf("expected IsError=true for business error envelope")
	}
	text := res.Content[0].(*mcpsdk.TextContent).Text
	if !strings.Contains(text, "字段 name 与保留列冲突") {
		t.Fatalf("error text missing upstream msg: %s", text)
	}
}

func TestLoadConfigEnvAndRequiredKey(t *testing.T) {
	noFile := func(string) ([]byte, error) { return nil, os.ErrNotExist }
	cfg, err := loadConfig("config.yaml", noFile, func(k string) string {
		switch k {
		case "GW_ADDR":
			return ":9000"
		case "GW_UPSTREAM":
			return "http://127.0.0.1:9000"
		case "GW_API_KEY":
			return "key-1"
		case "GW_AUTH_TOKEN":
			return "auth-1"
		}
		return ""
	})
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if cfg.Addr != ":9000" || cfg.Upstream != "http://127.0.0.1:9000" || cfg.APIKey != "key-1" || cfg.AuthToken != "auth-1" {
		t.Fatalf("cfg = %+v", cfg)
	}

	_, err = loadConfig("config.yaml", noFile, func(string) string { return "" })
	if err == nil || !strings.Contains(err.Error(), "GW_API_KEY") {
		t.Fatalf("expected GW_API_KEY required error, got %v", err)
	}
	_, err = loadConfig("config.yaml", noFile, func(k string) string {
		if k == "GW_API_KEY" {
			return "key-1"
		}
		return ""
	})
	if err == nil || !strings.Contains(err.Error(), "GW_AUTH_TOKEN") {
		t.Fatalf("expected GW_AUTH_TOKEN required error, got %v", err)
	}
}

func TestLoadConfigYAMLAndEnvOverride(t *testing.T) {
	yamlText := []byte(strings.Join([]string{
		"addr: :9000",
		"upstream: http://127.0.0.1:9000",
		"api_key: from-yaml",
		"auth_token: auth-from-yaml",
	}, string(rune(10))))
	cfg, err := loadConfig("config.yaml", func(string) ([]byte, error) { return yamlText, nil }, func(k string) string {
		if k == "GW_ADDR" {
			return ":9001"
		}
		return ""
	})
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if cfg.Addr != ":9001" {
		t.Fatalf("addr = %q, want env override :9001", cfg.Addr)
	}
	if cfg.Upstream != "http://127.0.0.1:9000" {
		t.Fatalf("upstream = %q", cfg.Upstream)
	}
	if cfg.APIKey != "from-yaml" {
		t.Fatalf("api_key = %q, want from-yaml", cfg.APIKey)
	}
	if cfg.AuthToken != "auth-from-yaml" {
		t.Fatalf("auth_token = %q, want auth-from-yaml", cfg.AuthToken)
	}
}

func schemaAt(t *testing.T, root map[string]any, path ...string) any {
	t.Helper()
	var cur any = root
	for _, key := range path {
		m, ok := cur.(map[string]any)
		if !ok {
			t.Fatalf("schema path %v: not an object at %q", path, key)
		}
		cur, ok = m[key]
		if !ok {
			t.Fatalf("schema path %v: missing %q", path, key)
		}
	}
	return cur
}

func containsString(list any, want string) bool {
	items, ok := list.([]any)
	if !ok {
		return false
	}
	for _, item := range items {
		if s, ok := item.(string); ok && s == want {
			return true
		}
	}
	return false
}
