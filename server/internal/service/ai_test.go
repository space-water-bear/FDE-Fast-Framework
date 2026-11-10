package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"fde-admin/internal/global"
	"fde-admin/internal/model"
)

func setupTestDB(t *testing.T) {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("database handle: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	global.DB = db
	global.Logger = zap.NewNop()
	if err := db.AutoMigrate(
		&model.AiProvider{}, &model.AiAgent{}, &model.AiSkill{}, &model.AiTool{},
		&model.AiMcpServer{}, &model.AiSession{}, &model.AiMessage{},
		&model.AiBot{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
}

// newMockGateway 模拟 OpenAI 兼容网关(chat/completions + responses, 含流式与工具调用)
func newMockGateway() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var data map[string]any
		_ = json.Unmarshal(body, &data)

		if r.URL.Path == "/v1/chat/completions" {
			if stream, _ := data["stream"].(bool); stream {
				w.Header().Set("Content-Type", "text/event-stream")
				fl := w.(http.Flusher)
				for _, c := range []string{"chat", "流式", "输出"} {
					fmt.Fprintf(w, "data: %s\n\n", mustJSON(map[string]any{
						"id": "cmpl-1", "object": "chat.completion.chunk", "created": 1, "model": "model-a",
						"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"content": c}}},
					}))
					fl.Flush()
				}
				fmt.Fprint(w, "data: [DONE]\n\n")
				fl.Flush()
				return
			}
			w.Header().Set("Content-Type", "application/json")
			tools, _ := data["tools"].([]any)
			messages, _ := data["messages"].([]any)
			hasToolMsg := false
			var toolContent string
			for _, m := range messages {
				mm, _ := m.(map[string]any)
				if role, _ := mm["role"].(string); role == "tool" {
					hasToolMsg = true
					toolContent, _ = mm["content"].(string)
				}
			}
			if len(tools) > 0 && !hasToolMsg {
				toolName := "unknown"
				if t0, ok := tools[0].(map[string]any); ok {
					if fn, ok := t0["function"].(map[string]any); ok {
						toolName, _ = fn["name"].(string)
					}
				}
				fmt.Fprint(w, mustJSON(map[string]any{
					"id": "cmpl-1", "object": "chat.completion", "created": 1, "model": "model-a",
					"choices": []any{map[string]any{
						"index": 0,
						"message": map[string]any{
							"role": "assistant", "content": nil,
							"tool_calls": []any{map[string]any{
								"id": "call_1", "type": "function",
								"function": map[string]any{"name": toolName, "arguments": "{}"},
							}},
						},
						"finish_reason": "tool_calls",
					}},
				}))
				return
			}
			content := "chat 模式回复成功"
			if hasToolMsg {
				content = "chat tool-result=" + toolContent
			}
			fmt.Fprint(w, mustJSON(map[string]any{
				"id": "cmpl-1", "object": "chat.completion", "created": 1, "model": "model-a",
				"choices": []any{map[string]any{
					"index": 0, "message": map[string]any{"role": "assistant", "content": content}, "finish_reason": "stop",
				}},
			}))
			return
		}

		if r.URL.Path == "/v1/responses" {
			if stream, _ := data["stream"].(bool); stream {
				w.Header().Set("Content-Type", "text/event-stream")
				fl := w.(http.Flusher)
				for i, c := range []string{"responses", "流式", "输出"} {
					fmt.Fprintf(w, "data: %s\n\n", mustJSON(map[string]any{
						"type": "response.output_text.delta", "sequence_number": i + 1,
						"item_id": "msg_1", "output_index": 0, "content_index": 0, "delta": c, "logprobs": []any{},
					}))
					fl.Flush()
				}
				fmt.Fprint(w, "data: [DONE]\n\n")
				fl.Flush()
				return
			}
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, mustJSON(map[string]any{
				"id": "resp_1", "object": "response", "created_at": 1, "status": "completed", "model": "model-a",
				"output": []any{map[string]any{
					"id": "msg_1", "type": "message", "role": "assistant", "status": "completed",
					"content": []any{map[string]any{"type": "output_text", "text": "responses 模式回复成功", "annotations": []any{}}},
				}},
				"output_text": "responses 模式回复成功",
			}))
			return
		}

		http.NotFound(w, r)
	}))
}

func mustJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func seedProviderAgentSession(t *testing.T, apiMode string, toolIDs, mcpIDs []uint) (*model.AiSession, *model.AiProvider) {
	t.Helper()
	gw := newMockGateway()
	t.Cleanup(gw.Close)
	base := gw.URL + "/v1"
	if err := CreateAiProvider("Mock", base, "mock-key", apiMode, "model-a", 0.7, true, ""); err != nil {
		t.Fatalf("create provider: %v", err)
	}
	providers, _ := ListAiProviders("")
	provider := providers[0]
	if err := CreateAiAgent("测试助手", "你是助手", provider.ID, 0.5, true, "", nil, toolIDs, mcpIDs); err != nil {
		t.Fatalf("create agent: %v", err)
	}
	agents, _ := ListAiAgents("")
	ag := agents[0]
	session, err := CreateAiSession("", ag.ID, 1)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	return session, &provider
}

func TestChatAiResponses(t *testing.T) {
	setupTestDB(t)
	session, _ := seedProviderAgentSession(t, "responses", nil, nil)
	msg, err := ChatAi(session.ID, "你好")
	if err != nil {
		t.Fatalf("ChatAi: %v", err)
	}
	if msg.Content != "responses 模式回复成功" {
		t.Fatalf("unexpected content: %s", msg.Content)
	}
}

func TestChatAiChatWithTool(t *testing.T) {
	setupTestDB(t)
	if err := SaveAiTool(0, "get_weather", "查询天气", "{}", true, ""); err != nil {
		t.Fatalf("save tool: %v", err)
	}
	tools, _ := ListAiTools("")
	session, _ := seedProviderAgentSession(t, "chat", []uint{tools[0].ID}, nil)
	msg, err := ChatAi(session.ID, "北京天气")
	if err != nil {
		t.Fatalf("ChatAi: %v", err)
	}
	if !strings.Contains(msg.Content, "北京") || !strings.Contains(msg.Content, "晴") {
		t.Fatalf("tool result missing in reply: %s", msg.Content)
	}
}

func TestStreamChatAiTo(t *testing.T) {
	setupTestDB(t)
	session, _ := seedProviderAgentSession(t, "chat", nil, nil)
	var got strings.Builder
	full, err := StreamChatAiTo(context.Background(), session.ID, "流式", func(d string) {
		got.WriteString(d)
	})
	if err != nil {
		t.Fatalf("StreamChatAiTo: %v", err)
	}
	if full != "chat流式输出" {
		t.Fatalf("unexpected stream full: %s", full)
	}
	if got.String() != full {
		t.Fatalf("callback mismatch: %s vs %s", got.String(), full)
	}
}

func TestAutoTitle(t *testing.T) {
	setupTestDB(t)
	session, _ := seedProviderAgentSession(t, "responses", nil, nil)
	if _, err := ChatAi(session.ID, "今天天气怎么样啊"); err != nil {
		t.Fatalf("ChatAi: %v", err)
	}
	var s model.AiSession
	if err := global.DB.First(&s, session.ID).Error; err != nil {
		t.Fatalf("reload session: %v", err)
	}
	if s.Title != "今天天气怎么样啊" {
		t.Fatalf("unexpected title: %s", s.Title)
	}
}

func TestSessionOps(t *testing.T) {
	setupTestDB(t)
	session, _ := seedProviderAgentSession(t, "chat", nil, nil)

	if _, err := ChatAi(session.ID, "你好"); err != nil {
		t.Fatalf("ChatAi: %v", err)
	}
	if err := RenameAiSession(session.ID, "改名后的会话"); err != nil {
		t.Fatalf("rename: %v", err)
	}
	var s model.AiSession
	global.DB.First(&s, session.ID)
	if s.Title != "改名后的会话" {
		t.Fatalf("title not updated: %s", s.Title)
	}

	// 重新生成: 消息数不变, 最后一条仍是 assistant
	if _, err := RegenerateAiSession(session.ID); err != nil {
		t.Fatalf("regenerate: %v", err)
	}
	msgs, _ := ListAiMessages(session.ID)
	if len(msgs) != 2 || msgs[1].Role != "assistant" {
		t.Fatalf("regenerate messages: len=%d lastRole=%s", len(msgs), msgs[len(msgs)-1].Role)
	}

	// 编辑重发: 更新用户消息并丢弃其后消息
	userMsg := msgs[0]
	if _, err := ResendAiMessage(session.ID, userMsg.ID, "编辑后的内容"); err != nil {
		t.Fatalf("resend: %v", err)
	}
	msgs, _ = ListAiMessages(session.ID)
	if len(msgs) != 2 || msgs[0].Content != "编辑后的内容" {
		t.Fatalf("resend messages: len=%d first=%s", len(msgs), msgs[0].Content)
	}

	// 清空
	if err := ClearAiSession(session.ID); err != nil {
		t.Fatalf("clear: %v", err)
	}
	msgs, _ = ListAiMessages(session.ID)
	if len(msgs) != 0 {
		t.Fatalf("clear messages: len=%d", len(msgs))
	}
}

func TestListAiSessionsPagination(t *testing.T) {
	setupTestDB(t)
	if err := CreateAiProvider("Mock", "http://127.0.0.1:9/v1", "k", "chat", "m", 0.7, true, ""); err != nil {
		t.Fatalf("create provider: %v", err)
	}
	providers, _ := ListAiProviders("")
	if err := CreateAiAgent("A", "", providers[0].ID, 0.5, true, "", nil, nil, nil); err != nil {
		t.Fatalf("create agent: %v", err)
	}
	agents, _ := ListAiAgents("")
	for i := 0; i < 25; i++ {
		if _, err := CreateAiSession(fmt.Sprintf("批量会话%d", i), agents[0].ID, 1); err != nil {
			t.Fatalf("create session %d: %v", i, err)
		}
	}
	if _, err := CreateAiSession("特殊标题xyz", agents[0].ID, 1); err != nil {
		t.Fatalf("create special session: %v", err)
	}
	list, total, err := ListAiSessions("", 2, 10)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if total != 26 || len(list) != 10 {
		t.Fatalf("pagination: total=%d len=%d", total, len(list))
	}
	_, total, err = ListAiSessions("特殊", 1, 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if total != 1 {
		t.Fatalf("search total: %d", total)
	}
}

func TestMcpStreamableTool(t *testing.T) {
	setupTestDB(t)
	// 真实 MCP server(streamable HTTP)
	srv := mcp.NewServer(&mcp.Implementation{Name: "test-mcp", Version: "1.0"}, nil)
	srv.AddTool(&mcp.Tool{
		Name:        "mcp_echo",
		Description: "回声工具",
		InputSchema: map[string]any{"type": "object"},
	}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "echo:" + string(req.Params.Arguments)}}}, nil
	})
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return srv }, nil)
	ts := httptest.NewServer(handler)
	t.Cleanup(ts.Close)

	if err := SaveAiMcpServer(0, "TestMCP", "streamable", ts.URL, "", "", "", true, ""); err != nil {
		t.Fatalf("save mcp: %v", err)
	}
	mcps, _ := ListAiMcpServers("")
	session, _ := seedProviderAgentSession(t, "chat", nil, []uint{mcps[0].ID})
	msg, err := ChatAi(session.ID, "回声")
	if err != nil {
		t.Fatalf("ChatAi: %v", err)
	}
	if !strings.Contains(msg.Content, "echo:") {
		t.Fatalf("mcp tool result missing: %s", msg.Content)
	}
}
