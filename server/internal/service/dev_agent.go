package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/microsoft/agent-framework-go/agent"
	"github.com/microsoft/agent-framework-go/agent/harness/toolautocall"
	"github.com/microsoft/agent-framework-go/message"
	"github.com/microsoft/agent-framework-go/provider/openaiprovider"
	"github.com/microsoft/agent-framework-go/tool"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/responses"

	"fde-admin/internal/global"
	"fde-admin/internal/model"
)

// DevelopmentRunScope is created by the host after a durable queue claim. Model
// JSON never chooses an actor, run, project or filesystem root.
type DevelopmentRunScope struct {
	RunID         uint
	RunKey        string
	ActorID       uint
	ProjectID     uint
	WorkspacePath string
}
type developmentRunScopeKey struct{}

func WithDevelopmentRunScope(ctx context.Context, scope DevelopmentRunScope) context.Context {
	return context.WithValue(ctx, developmentRunScopeKey{}, scope)
}
func RequireDevelopmentRunScope(ctx context.Context) (DevelopmentRunScope, error) {
	scope, ok := ctx.Value(developmentRunScopeKey{}).(DevelopmentRunScope)
	if !ok || scope.RunID == 0 || scope.RunKey == "" || scope.ActorID == 0 || scope.ProjectID == 0 || scope.WorkspacePath == "" {
		return DevelopmentRunScope{}, errors.New("开发工具需要宿主绑定的任务身份与工作区")
	}
	return scope, nil
}

const developmentAgentInstructions = `你正在实现一个已授权的客户开发任务。
只能使用本次固定提供的工程工具，在当前任务隔离工作区内读取、生成或修改源码。先读取项目上下文和现有代码，再实施需求。
不得请求宿主shell、修改宿主源码、生产数据、发布环境、凭证、.git、依赖锁文件或检查配置。
不得删除或弱化测试、跳过检查或伪造检查结果。保持用户已有业务规则；遇到需求冲突应明确说明。
当前工作区内的源码编辑已授权，无需对每次patch再次要求批准。部署和发布尚未授权。
最后说明实际变更与待处理事项。最终文字不能宣告交付成功，宿主会独立运行固定检查并裁决。`

func runDevelopmentAgent(ctx context.Context, run model.DevRun, feedback string, tools []tool.Tool) (string, error) {
	var selected model.AiAgent
	if err := global.DB.Preload("Skills").First(&selected, run.AgentID).Error; err != nil || !selected.Enabled {
		return "", errors.New("执行智能体不存在或已禁用")
	}
	provider, err := loadProvider(&selected)
	if err != nil {
		return "", err
	}
	wrapped, err := developmentObservedTools(run, tools)
	if err != nil {
		return "", err
	}
	opts := []option.RequestOption{option.WithAPIKey(provider.APIKey)}
	if provider.BaseURL != "" {
		opts = append(opts, option.WithBaseURL(provider.BaseURL))
	}
	client := openai.NewClient(opts...)
	maximumRounds, maximumErrors := 24, 2
	cfg := openaiprovider.AgentConfig{
		Config:             agent.Config{Tools: wrapped},
		Instructions:       buildSystemPrompt(&selected) + "\n\n" + developmentAgentInstructions,
		Model:              provider.Model,
		ToolAutoCall:       &toolautocall.Config{MaximumIterationsPerRequest: &maximumRounds, MaximumConsecutiveErrorsPerRequest: &maximumErrors, IncludeDetailedErrors: true, AllowConcurrentInvocations: false},
		DisableStoreOutput: true,
	}
	var instance *agent.Agent
	if provider.APIMode == "chat" {
		instance = openaiprovider.NewChatCompletionsAgent(client, cfg)
	} else {
		cfg.RunOptions = append(cfg.RunOptions, openaiprovider.ResponsesNewParams(responses.ResponseNewParams{Store: openai.Bool(false)}))
		instance = openaiprovider.NewResponsesAgent(client, cfg)
	}
	prompt := fmt.Sprintf("任务: %s\n项目ID: %d\n源码基线: %s\n需求:\n%s", run.Key, run.ProjectID, run.BaseCommit, run.Requirement)
	if feedback != "" {
		prompt += "\n\n宿主检查失败，请修复本次源码变更（不能更改检查规则）：\n" + limitDevelopmentText(feedback, 16000)
	}
	if err := appendDevelopmentEvent(run.ID, "agent_started", developmentJSON(map[string]any{"agentId": selected.ID, "providerId": provider.ID, "model": provider.Model, "attempt": run.Attempts})); err != nil {
		return "", err
	}
	started := time.Now()
	response, err := instance.Run(ctx, []*message.Message{message.NewText(prompt)}).Collect()
	if err != nil {
		return "", err
	}
	final := response.String()
	recordAiUsage(0, selected.ID, provider.ID, provider.Model, response.Usage(), time.Since(started))
	if err := appendDevelopmentEvent(run.ID, "agent_result", developmentSummary(map[string]any{"attempt": run.Attempts, "text": final})); err != nil {
		return "", err
	}
	return final, nil
}

type observedDevelopmentTool struct {
	tool.FuncTool
	run   model.DevRun
	calls *atomic.Int32
}

func developmentObservedTools(run model.DevRun, tools []tool.Tool) ([]tool.Tool, error) {
	observed := make([]tool.Tool, 0, len(tools))
	counter := &atomic.Int32{}
	for _, candidate := range tools {
		executable, ok := candidate.(tool.FuncTool)
		if !ok {
			return nil, fmt.Errorf("工程工具 %s 不可执行", candidate.Name())
		}
		observed = append(observed, &observedDevelopmentTool{FuncTool: executable, run: run, calls: counter})
	}
	if len(observed) == 0 {
		return nil, errors.New("工程工具尚未配置")
	}
	return observed, nil
}
func (t *observedDevelopmentTool) Call(ctx context.Context, args string) (any, error) {
	scope, err := RequireDevelopmentRunScope(ctx)
	if err != nil {
		return nil, err
	}
	if scope.RunID != t.run.ID || scope.RunKey != t.run.Key || scope.ActorID != t.run.ActorID || scope.ProjectID != t.run.ProjectID || scope.WorkspacePath != t.run.WorkspacePath {
		return nil, errors.New("工程工具任务作用域不匹配")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if t.calls.Add(1) > 32 {
		return nil, errors.New("本轮工程工具调用超过宿主限制")
	}
	var state model.DevRun
	if err := global.DB.Select("state", "cancel_requested").First(&state, t.run.ID).Error; err != nil {
		return nil, err
	}
	if state.State != "running" || state.CancelRequested {
		return nil, context.Canceled
	}
	var arguments any
	if json.Unmarshal([]byte(args), &arguments) != nil {
		arguments = "invalid JSON"
	}
	if err := appendDevelopmentEvent(t.run.ID, "tool_started", developmentSummary(map[string]any{"tool": t.Name(), "arguments": arguments})); err != nil {
		return nil, err
	}
	started := time.Now()
	result, callErr := t.FuncTool.Call(ctx, args)
	event := map[string]any{"tool": t.Name(), "durationMs": time.Since(started).Milliseconds(), "result": result, "ok": callErr == nil}
	if callErr != nil {
		event["error"] = callErr.Error()
	}
	if err := appendDevelopmentEvent(t.run.ID, "tool_result", developmentSummary(event)); err != nil {
		return nil, err
	}
	return result, callErr
}

func appendDevelopmentEvent(runID uint, kind, message string) error {
	return global.DB.Create(&model.DevRunEvent{RunID: runID, Kind: kind, Message: message}).Error
}
func developmentJSON(value any) string {
	raw, err := json.Marshal(value)
	if err != nil {
		return `{"error":"result could not be encoded"}`
	}
	return string(raw)
}
func limitDevelopmentText(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	for limit > 0 && !utf8.ValidString(value[:limit]) {
		limit--
	}
	return value[:limit] + "…(truncated)"
}

var developmentTokenPattern = regexp.MustCompile(`(?i)(?:sk-[a-z0-9_-]{12,}|gh[pousr]_[a-z0-9]{12,}|github_pat_[a-z0-9_]{16,}|Bearer\s+[a-z0-9._-]{8,})`)

func developmentSummary(value any) string {
	raw, err := json.Marshal(value)
	if err != nil {
		return `{"error":"result could not be encoded"}`
	}
	var parsed any
	if json.Unmarshal(raw, &parsed) != nil {
		return "result unavailable"
	}
	var sanitize func(any) any
	sanitize = func(item any) any {
		switch current := item.(type) {
		case map[string]any:
			out := map[string]any{}
			for key, value := range current {
				normalized := strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(key, "_", ""), "-", ""))
				switch normalized {
				case "apikey", "password", "secret", "token", "authorization", "credential":
					out[key] = "<redacted>"
					continue
				}
				if text, ok := value.(string); ok && (key == "content" || key == "patch") {
					digest := sha256.Sum256([]byte(text))
					out[key] = map[string]any{"bytes": len(text), "sha256": hex.EncodeToString(digest[:])}
					continue
				}
				out[key] = sanitize(value)
			}
			return out
		case []any:
			if len(current) > 24 {
				current = current[:24]
			}
			out := make([]any, 0, len(current))
			for _, value := range current {
				out = append(out, sanitize(value))
			}
			return out
		case string:
			return developmentTokenPattern.ReplaceAllString(limitDevelopmentText(current, 3000), "<redacted>")
		default:
			return item
		}
	}
	return limitDevelopmentText(developmentJSON(sanitize(parsed)), 12000)
}
