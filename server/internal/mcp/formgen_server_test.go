package mcp

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/microsoft/agent-framework-go/tool/mcptool"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"gorm.io/gorm"

	"fde-admin/internal/global"
	"fde-admin/internal/model"
)

func setupMcpFormgenDB(t *testing.T) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	global.DB = db
	if err := db.AutoMigrate(&model.BizObject{}, &model.BizField{}, &model.FormSchema{}, &model.GenAudit{}); err != nil {
		t.Fatalf("migrate formgen: %v", err)
	}
}

func connectFormgenMCP(t *testing.T) *mcpsdk.ClientSession {
	t.Helper()
	ts := httptest.NewServer(FormgenHandler())
	t.Cleanup(ts.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	transport := &mcpsdk.StreamableClientTransport{Endpoint: ts.URL, HTTPClient: ts.Client()}
	session, err := mcptool.Connect(ctx, transport)
	if err != nil {
		t.Fatalf("connect mcp: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session
}

func TestFormgenServerListTools(t *testing.T) {
	setupMcpFormgenDB(t)
	session := connectFormgenMCP(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	res, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("tools/list: %v", err)
	}
	if len(res.Tools) != 3 {
		t.Fatalf("tools count = %d, want 3", len(res.Tools))
	}
	names := map[string]bool{}
	for _, tool := range res.Tools {
		names[tool.Name] = true
		if tool.Description == "" {
			t.Fatalf("tool %s missing description", tool.Name)
		}
		b, err := json.Marshal(tool.InputSchema)
		if err != nil {
			t.Fatalf("tool %s input schema not marshalable: %v", tool.Name, err)
		}
		var schema map[string]any
		if err := json.Unmarshal(b, &schema); err != nil {
			t.Fatalf("tool %s input schema not parseable: %v", tool.Name, err)
		}
		if schema["type"] != "object" {
			t.Fatalf("tool %s schema missing type: %s", tool.Name, string(b))
		}
		switch tool.Name {
		case "generate_form_schema", "generate_business_code":
			if schema["$defs"] == nil {
				t.Fatalf("tool %s schema missing $defs: %s", tool.Name, string(b))
			}
		case "apply_files":
			props, _ := schema["properties"].(map[string]any)
			if props == nil || props["approve"] == nil || !strings.Contains(tool.Description, "APPROVAL_REQUIRED") {
				t.Fatalf("tool apply_files schema missing approve: %s", string(b))
			}
		}
	}
	for _, want := range []string{"generate_form_schema", "generate_business_code", "apply_files"} {
		if !names[want] {
			t.Fatalf("tools/list missing %s, got %v", want, names)
		}
	}
}

func TestFormgenServerCallGenerateFormSchemaDryRun(t *testing.T) {
	setupMcpFormgenDB(t)
	session := connectFormgenMCP(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	res, err := session.CallTool(ctx, &mcpsdk.CallToolParams{
		Name: "generate_form_schema",
		Arguments: map[string]any{
			"objectKey": "biz_mcp_demo",
			"name":      "演示对象",
			"fields": []map[string]any{
				{"name": "name", "title": "名称", "type": "string", "required": true},
			},
			"dryRun": true,
		},
	})
	if err != nil {
		t.Fatalf("tools/call generate_form_schema: %v", err)
	}
	if res == nil || len(res.Content) == 0 {
		t.Fatalf("tools/call result missing content")
	}
	text := res.Content[0].(*mcpsdk.TextContent).Text
	if !strings.Contains(text, "\"ok\":true") {
		t.Fatalf("tools/call result not ok: %s", text)
	}
	if !strings.Contains(text, "biz_mcp_demo") {
		t.Fatalf("tools/call result missing objectKey: %s", text)
	}
}

func TestFormgenServerCallUnknownTool(t *testing.T) {
	setupMcpFormgenDB(t)
	session := connectFormgenMCP(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := session.CallTool(ctx, &mcpsdk.CallToolParams{Name: "unknown_tool"})
	if err == nil || !strings.Contains(err.Error(), "unknown tool") {
		t.Fatalf("expected unknown tool error, got %v", err)
	}
}

func TestMCPServiceSchemaContractSupportsFullPreviewWithoutWriting(t *testing.T) {
	setupMcpFormgenDB(t)
	session := connectFormgenMCP(t)
	schema := map[string]any{"objectKey": "biz_full_mcp", "name": "Full schema", "fields": map[string]any{"name": map[string]any{"type": "string", "x-fde": map[string]any{"type": "string", "inForm": true}}}}
	result, err := session.CallTool(context.Background(), &mcpsdk.CallToolParams{Name: "generate_form_schema", Arguments: map[string]any{"schema": schema, "dryRun": true}})
	if err != nil || result.IsError {
		t.Fatalf("full schema preview: %v %+v", err, result)
	}
	text := result.Content[0].(*mcpsdk.TextContent).Text
	if !strings.Contains(text, `"ok":true`) {
		t.Fatalf("preview failed: %s", text)
	}
	result, err = session.CallTool(context.Background(), &mcpsdk.CallToolParams{Name: "generate_form_schema", Arguments: map[string]any{"schema": schema, "dryRun": false}})
	if err == nil && !result.IsError {
		denial := result.Content[0].(*mcpsdk.TextContent).Text
		if !strings.Contains(denial, `"ok":false`) || !strings.Contains(denial, "ENGINEERING_SCOPE_REQUIRED") {
			t.Fatalf("service write was not rejected: %s", denial)
		}
	}
	var n int64
	if err := global.DB.Model(&model.BizObject{}).Count(&n).Error; err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("service identity wrote %d objects", n)
	}
}
