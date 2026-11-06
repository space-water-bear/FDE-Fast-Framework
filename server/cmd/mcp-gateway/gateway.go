package main

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

const gatewayMCPName = "fde-admin-mcp-gateway"

// toolRoutes 工具名到主应用 /api/mcp/* 端点的映射。
var toolRoutes = map[string]string{
	"generate_form_schema":   "/api/mcp/formgen/schema/preview",
	"generate_business_code": "/api/mcp/codegen/preview",
	"apply_files":            "/api/mcp/formgen/apply",
}

// Gateway 把 MCP tools/call 翻译成对主应用 /api/mcp/* 的 HTTP 调用。
type Gateway struct {
	cfg    Config
	client *http.Client
}

// NewGateway 构建网关, HTTP 客户端超时 60s。
func NewGateway(cfg Config) *Gateway {
	return &Gateway{
		cfg:    cfg,
		client: &http.Client{Timeout: 60 * time.Second},
	}
}

// Handler 返回带鉴权的 streamable HTTP 处理器。
// 外部客户端须携带 Authorization: Bearer <GW_AUTH_TOKEN> 或 x-api-key: <GW_AUTH_TOKEN>。
func (g *Gateway) Handler() http.Handler {
	mcp := mcpsdk.NewStreamableHTTPHandler(func(*http.Request) *mcpsdk.Server {
		return g.newServer()
	}, nil)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !g.authorized(r) {
			log.Printf("mcp-gateway 鉴权拒绝: path=%s remote=%s", r.URL.Path, r.RemoteAddr)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
			return
		}
		mcp.ServeHTTP(w, r)
	})
}

// authorized 校验 Authorization: Bearer 或 x-api-key 是否匹配 AuthToken(常数时间比较)。
func (g *Gateway) authorized(r *http.Request) bool {
	token := g.cfg.AuthToken
	if token == "" {
		return false
	}
	if v := r.Header.Get("Authorization"); strings.HasPrefix(v, "Bearer ") {
		return subtle.ConstantTimeCompare([]byte(strings.TrimSpace(strings.TrimPrefix(v, "Bearer "))), []byte(token)) == 1
	}
	if v := r.Header.Get("x-api-key"); v != "" {
		return subtle.ConstantTimeCompare([]byte(v), []byte(token)) == 1
	}
	return false
}

func (g *Gateway) newServer() *mcpsdk.Server {
	srv := mcpsdk.NewServer(&mcpsdk.Implementation{Name: gatewayMCPName, Version: "1.0"}, nil)
	for _, t := range toolDefs {
		srv.AddTool(t, g.callTool)
	}
	return srv
}

func (g *Gateway) callTool(ctx context.Context, req *mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	name := req.Params.Name
	path, ok := toolRoutes[name]
	if !ok {
		return errorToolResult(fmt.Sprintf("工具未找到: %s", name)), nil
	}
	args := req.Params.Arguments
	if len(args) == 0 {
		args = []byte("{}")
	}
	data, err := g.forward(ctx, path, args)
	if err != nil {
		return errorToolResult(fmt.Sprintf("上游调用失败: %v", err)), nil
	}
	return &mcpsdk.CallToolResult{
		Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: string(data)}},
	}, nil
}

// envelope 是主应用统一响应信封 {code,msg,data}; encoding/json 按大小写不敏感匹配字段。
type envelope struct {
	Code int
	Msg  string
	Data json.RawMessage
}

// forward POST 到主应用并解析统一信封: code==0 返回 data, 否则返回业务错误。
func (g *Gateway) forward(ctx context.Context, path string, body []byte) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.cfg.Upstream+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", g.cfg.APIKey)

	resp, err := g.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var env envelope
	if err := json.Unmarshal(respBody, &env); err != nil {
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return nil, fmt.Errorf("上游 HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
		}
		return nil, fmt.Errorf("解析上游响应失败: %w", err)
	}
	if env.Code != 0 {
		return nil, fmt.Errorf("业务错误(%d): %s", env.Code, env.Msg)
	}
	if len(env.Data) == 0 {
		env.Data = []byte("null")
	}
	return env.Data, nil
}

func errorToolResult(text string) *mcpsdk.CallToolResult {
	return &mcpsdk.CallToolResult{
		IsError: true,
		Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: text}},
	}
}
