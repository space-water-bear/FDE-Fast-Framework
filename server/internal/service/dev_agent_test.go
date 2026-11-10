package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"fde-admin/internal/config"
	"fde-admin/internal/global"
	"fde-admin/internal/model"
)

// This gateway scripts provider responses only. MAF still performs real tool
// dispatch, receives real tool results and completes its function-calling loop.
func developmentMockGateway(t *testing.T, agentID uint) (*atomic.Int32, *atomic.Int32) {
	t.Helper()
	requests, toolResults := &atomic.Int32{}, &atomic.Int32{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		requests.Add(1)
		raw, _ := io.ReadAll(r.Body)
		var request map[string]any
		if err := json.Unmarshal(raw, &request); err != nil {
			t.Error(err)
			http.Error(w, "invalid request", 400)
			return
		}
		tools, _ := request["tools"].([]any)
		for _, rawTool := range tools {
			item := rawTool.(map[string]any)
			fn := item["function"].(map[string]any)
			if fn["name"] == "get_weather" || fn["name"] == "apply_files" {
				t.Error("database-bound/host-writing tool leaked into development agent")
			}
		}
		messages, _ := request["messages"].([]any)
		hasTool, repair := false, false
		for _, rawMessage := range messages {
			msg := rawMessage.(map[string]any)
			if msg["role"] == "tool" {
				hasTool = true
				toolResults.Add(1)
				if content, _ := msg["content"].(string); !strings.Contains(content, "repair_ticket") {
					t.Errorf("provider did not receive real generated file result: %s", content)
				}
			}
			if text, _ := msg["content"].(string); strings.Contains(text, "宿主检查失败") {
				repair = true
			}
		}
		responseMessage := map[string]any{"role": "assistant", "content": "所有测试通过，可以交付。"}
		finish := "stop"
		if !hasTool && !repair {
			args := map[string]any{"tableName": "biz_repair_ticket", "business": "维修登记", "fields": []map[string]any{{"name": "description", "title": "问题描述", "type": "string", "inForm": true, "inList": true, "required": true}}, "actorId": 99999, "projectId": 99999, "workspacePath": "/should-not-be-used"}
			arguments, _ := json.Marshal(args)
			responseMessage = map[string]any{"role": "assistant", "content": nil, "tool_calls": []any{map[string]any{"id": "call_codegen", "type": "function", "function": map[string]any{"name": "generate_business_code", "arguments": string(arguments)}}}}
			finish = "tool_calls"
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "dev-test", "object": "chat.completion", "created": 1, "model": "test-model", "choices": []any{map[string]any{"index": 0, "message": responseMessage, "finish_reason": finish}}, "usage": map[string]any{"prompt_tokens": 10, "completion_tokens": 10, "total_tokens": 20}})
	}))
	t.Cleanup(server.Close)
	var selected model.AiAgent
	if err := global.DB.First(&selected, agentID).Error; err != nil {
		t.Fatal(err)
	}
	if err := global.DB.Model(&model.AiProvider{}).Where("id = ?", selected.ProviderID).Update("base_url", server.URL+"/v1").Error; err != nil {
		t.Fatal(err)
	}
	forbidden := model.AiTool{Name: "get_weather", Schema: `{"type":"object"}`, Enabled: true}
	if err := global.DB.Create(&forbidden).Error; err != nil {
		t.Fatal(err)
	}
	if err := global.DB.Model(&selected).Association("Tools").Append(&forbidden); err != nil {
		t.Fatal(err)
	}
	return requests, toolResults
}

func developmentTestServices(checks func(context.Context, model.DevRun) (any, error)) developmentRunServices {
	return developmentRunServices{tools: DevelopmentAgentTools, snapshot: DevelopmentRunSnapshot, checks: checks}
}
func configureDevelopmentTestRoot(t *testing.T) {
	t.Helper()
	previous := global.Config
	global.Config = &config.Config{Development: config.Development{WorkspaceDir: t.TempDir()}}
	t.Cleanup(func() { global.Config = previous })
	if err := global.DB.AutoMigrate(&model.AiUsage{}); err != nil {
		t.Fatal(err)
	}
}

func TestDevelopmentWorkerUsesActualMAFToolsAndHostChecks(t *testing.T) {
	scope, selected, projectRepo := setupDevelopmentRunTest(t)
	configureDevelopmentTestRoot(t)
	_, toolResults := developmentMockGateway(t, selected.ID)
	requestCtx, disconnect := context.WithCancel(context.Background())
	run, err := CreateDevRun(requestCtx, scope, "新增维修登记，必须包含问题描述字段", selected.ID)
	if err != nil {
		t.Fatal(err)
	}
	disconnect()
	checks := 0
	services := developmentTestServices(func(_ context.Context, current model.DevRun) (any, error) {
		checks++
		if checks == 1 {
			return map[string]any{"success": false, "diagnostic": "generated model requires repair"}, errors.New("compile failed")
		}
		return map[string]any{"success": true, "snapshotHash": current.SourceHash}, nil
	})
	claimed, err := processNextDevelopmentRun(context.Background(), services)
	if err != nil || !claimed {
		t.Fatalf("process task: %v %v", claimed, err)
	}
	got, events, err := GetDevRun(scope, run.Key)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != "succeeded" || got.Attempts != 2 || checks != 2 || got.SourceHash == "" || got.Result == "" || !strings.Contains(got.Diff, "RepairTicket") {
		t.Fatalf("host verified result missing: %+v checks=%d", got, checks)
	}
	if got.WorkspacePath == projectRepo || got.WorkspaceBaseCommit == "" {
		t.Fatal("run did not receive an independent committed workspace")
	}
	if _, err := os.Stat(filepath.Join(got.WorkspacePath, "server/internal/model/repair_ticket.go")); err != nil {
		t.Fatalf("actual tool did not generate module: %v", err)
	}
	if _, err := os.Stat(filepath.Join(projectRepo, "server/internal/model/repair_ticket.go")); !os.IsNotExist(err) {
		t.Fatal("task modified project baseline")
	}
	sawResult, sawRepair := false, false
	for _, event := range events {
		if event.Kind == "tool_result" && strings.Contains(event.Message, "repair_ticket") {
			sawResult = true
		}
		if event.Kind == "repair_requested" {
			sawRepair = true
		}
	}
	if !sawResult || !sawRepair || toolResults.Load() == 0 {
		t.Fatal("real tool/check evidence not persisted")
	}
}

func TestDevelopmentWorkerNeverTrustsModelSuccessAndBoundsRepairs(t *testing.T) {
	scope, selected, _ := setupDevelopmentRunTest(t)
	configureDevelopmentTestRoot(t)
	developmentMockGateway(t, selected.ID)
	run, err := CreateDevRun(context.Background(), scope, "新增维修登记", selected.ID)
	if err != nil {
		t.Fatal(err)
	}
	checks := 0
	claimed, err := processNextDevelopmentRun(context.Background(), developmentTestServices(func(_ context.Context, _ model.DevRun) (any, error) {
		checks++
		return map[string]any{"success": false, "diagnostic": "acceptance failure"}, nil
	}))
	if err != nil || !claimed {
		t.Fatalf("worker: %v %v", claimed, err)
	}
	got, _, err := GetDevRun(scope, run.Key)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != "failed" || got.Attempts != 4 || checks != 4 || !strings.Contains(got.Error, "3 轮修复") {
		t.Fatalf("model claimed success bypassed host limit: %+v checks=%d", got, checks)
	}
}

func TestDevelopmentWorkerCancelsRunningChecks(t *testing.T) {
	scope, selected, _ := setupDevelopmentRunTest(t)
	configureDevelopmentTestRoot(t)
	developmentMockGateway(t, selected.ID)
	run, err := CreateDevRun(context.Background(), scope, "新增维修登记", selected.ID)
	if err != nil {
		t.Fatal(err)
	}
	entered := make(chan struct{})
	done := make(chan error, 1)
	services := developmentTestServices(func(ctx context.Context, _ model.DevRun) (any, error) {
		close(entered)
		<-ctx.Done()
		return nil, ctx.Err()
	})
	go func() { _, err := processNextDevelopmentRun(context.Background(), services); done <- err }()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("host checks did not start")
	}
	if claimed, err := processNextDevelopmentRun(context.Background(), services); err != nil || claimed {
		t.Fatalf("second consumer entered while single worker was active: %v %v", claimed, err)
	}
	if err := CancelDevRun(scope, run.Key); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("running check ignored explicit cancellation")
	}
	got, _, err := GetDevRun(scope, run.Key)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != "canceled" || !got.CancelRequested || got.WorkspacePath == "" {
		t.Fatalf("running cancellation not durable: %+v", got)
	}
}

func TestDevelopmentWorkerRejectsSourceChangedDuringChecks(t *testing.T) {
	scope, selected, _ := setupDevelopmentRunTest(t)
	configureDevelopmentTestRoot(t)
	developmentMockGateway(t, selected.ID)
	run, err := CreateDevRun(context.Background(), scope, "新增维修登记", selected.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = processNextDevelopmentRun(context.Background(), developmentTestServices(func(_ context.Context, current model.DevRun) (any, error) {
		if err := os.WriteFile(filepath.Join(current.WorkspacePath, "server/internal/model/concurrent.go"), []byte("package model\n"), 0o644); err != nil {
			return nil, err
		}
		return map[string]any{"success": true, "snapshotHash": current.SourceHash}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	got, _, err := GetDevRun(scope, run.Key)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != "failed" || !strings.Contains(got.Error, "源码发生变化") {
		t.Fatalf("accepted stale checks: %+v", got)
	}
}

func TestDevelopmentRunScopeCannotComeFromModelArguments(t *testing.T) {
	if _, err := RequireDevelopmentRunScope(context.Background()); err == nil {
		t.Fatal("accepted no run scope")
	}
	expected := DevelopmentRunScope{RunID: 7, RunKey: "run-key", ActorID: 9, ProjectID: 11, WorkspacePath: t.TempDir()}
	actual, err := RequireDevelopmentRunScope(WithDevelopmentRunScope(context.Background(), expected))
	if err != nil || actual != expected {
		t.Fatalf("scope lost: %v %+v", err, actual)
	}
	value := developmentSummary(map[string]any{"password": "do-not-log", "content": "source code", "result": map[string]any{"written": []string{"server/internal/model/example.go"}}})
	if strings.Contains(value, "do-not-log") || strings.Contains(value, "source code") || !strings.Contains(value, "example.go") {
		t.Fatalf("unsafe or unhelpful tool summary: %s", value)
	}
}
